// Package autodeploy wires "deploy on every push" without the desktop app:
// GitHub Actions connects over SSH with a dedicated key whose authorized_keys
// entry is locked to a single forced command, the project's deploy hook.
// The hook runs the normal deploy script (keeping the server's .env, so no
// secrets live in CI) and records each run for the app's history view.
package autodeploy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// HooksDir holds hook scripts, logs and history on the server.
const HooksDir = "$HOME/truedeploy/hooks"

func marker(slug string) string { return "truedeploy-ci-" + slug }

// KeyPair is the CI key. The private half goes to GitHub only.
type KeyPair struct {
	PrivatePEM string
	PublicKey  string // "ssh-ed25519 AAAA..." without comment
}

func NewKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "truedeploy-ci")
	if err != nil {
		return nil, err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PrivatePEM: string(pem.EncodeToMemory(block)),
		PublicKey:  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))),
	}, nil
}

// HookScript is the file installed at HooksDir/<slug>.sh. deployScript must
// be the project's deploy script built with KeepEnv.
func HookScript(slug, deployScript string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte("set -euo pipefail\n" + deployScript + "\n"))
	return fmt.Sprintf(`#!/usr/bin/env bash
# Managed by TrueDeploy: auto-deploy hook for %[1]s.
# Run by GitHub Actions through a locked SSH key (see ~/.ssh/authorized_keys).
set -uo pipefail
HOOKS="%[2]s"
mkdir -p "$HOOKS"
if command -v flock >/dev/null; then
  exec 9>"$HOOKS/%[1]s.lock"
  flock -w 900 9 || { echo "Another deploy of %[1]s is still running." >&2; exit 1; }
fi
START=$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)
LOG="$HOOKS/%[1]s.last.log"
bash -c "$(echo %[3]s | base64 -d)" </dev/null 2>&1 | tee "$LOG"
RC=${PIPESTATUS[0]}
END=$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)
COMMIT=$(grep -o '::td::commit=.*' "$LOG" | tail -n 1 | cut -d= -f2- | tr -d '\t\r')
MSG=$(grep -o '::td::message=.*' "$LOG" | tail -n 1 | cut -d= -f2- | tr -d '\t\r')
if [ "$RC" -eq 0 ]; then STATUS=success; else STATUS=failed; MSG=$(grep -v '^::td::' "$LOG" | grep -v '^\s*$' | tail -n 1 | tr -d '\t\r'); fi
HIST="$HOOKS/%[1]s.history"
printf '%%s\t%%s\t%%s\t%%s\t%%s\n' "$START" "$END" "$STATUS" "$COMMIT" "$MSG" >> "$HIST"
tail -n 100 "$HIST" > "$HIST.tmp" && cat "$HIST.tmp" > "$HIST"; rm -f "$HIST.tmp"
exit "$RC"
`, slug, HooksDir, b64)
}

// InstallScript installs the hook and authorizes the CI key for it only.
// It prints ::td::home= and ::td::hostkey= markers for the workflow secrets.
func InstallScript(slug, hook, ciPublicKey string) string {
	return fmt.Sprintf(`HOOKS="%[1]s"
mkdir -p "$HOOKS"
H="$HOOKS/%[2]s.sh"
echo %[3]s | base64 -d > "$H"
chmod 700 "$H"
mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh" 2>/dev/null || true
AK="$HOME/.ssh/authorized_keys"
touch "$AK" && chmod 600 "$AK" 2>/dev/null || true
grep -v ' %[4]s$' "$AK" > "$AK.td" || true
cat "$AK.td" > "$AK"; rm -f "$AK.td"
echo "command=\"$H\",restrict %[5]s %[4]s" >> "$AK"
echo "::td::home=$HOME"
for f in /etc/ssh/ssh_host_ed25519_key.pub /etc/ssh/ssh_host_ecdsa_key.pub /etc/ssh/ssh_host_rsa_key.pub; do
  [ -r "$f" ] && echo "::td::hostkey=$(cut -d' ' -f1,2 "$f")"
done
echo "Auto-deploy hook installed."`, HooksDir, slug, base64.StdEncoding.EncodeToString([]byte(hook)), marker(slug), ciPublicKey)
}

// UninstallScript removes the CI key and the hook files.
func UninstallScript(slug string) string {
	return fmt.Sprintf(`AK="$HOME/.ssh/authorized_keys"
if [ -f "$AK" ]; then
  grep -v ' %[1]s$' "$AK" > "$AK.td" || true
  cat "$AK.td" > "$AK"; rm -f "$AK.td"
fi
rm -f "%[2]s/%[3]s.sh" "%[2]s/%[3]s.lock"
echo "Auto-deploy removed from the server."`, marker(slug), HooksDir, slug)
}

// HistoryScript prints recent hook runs (tab-separated) and the last log.
func HistoryScript(slug string) string {
	return fmt.Sprintf(`tail -n 30 "%[1]s/%[2]s.history" 2>/dev/null || true
echo "::td::log"
tail -n 300 "%[1]s/%[2]s.last.log" 2>/dev/null || true`, HooksDir, slug)
}

// Run is one auto-deploy recorded by the hook.
type Run struct {
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Status     string    `json:"status"`
	Commit     string    `json:"commit,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// ParseHistory reads HistoryScript output: runs newest first, and the last log.
func ParseHistory(lines []string) (runs []Run, lastLog []string) {
	inLog := false
	for _, l := range lines {
		if l == "::td::log" {
			inLog = true
			continue
		}
		if inLog {
			if !strings.HasPrefix(l, "::td::") {
				lastLog = append(lastLog, l)
			}
			continue
		}
		f := strings.Split(l, "\t")
		if len(f) < 3 {
			continue
		}
		r := Run{Status: f[2]}
		r.StartedAt, _ = time.Parse(time.RFC3339, f[0])
		r.FinishedAt, _ = time.Parse(time.RFC3339, f[1])
		if len(f) > 3 {
			r.Commit = f[3]
		}
		if len(f) > 4 {
			r.Message = f[4]
		}
		runs = append([]Run{r}, runs...)
	}
	return runs, lastLog
}

// SecretPrefix namespaces a project's secrets so several projects can share a repo.
func SecretPrefix(slug string) string {
	return "TRUEDEPLOY_" + strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))
}

// Secrets returns the repository secrets the workflow needs.
func Secrets(slug, user, host string, port int, privatePEM string, hostKeys []string) map[string]string {
	p := SecretPrefix(slug)
	return map[string]string{
		p + "_SSH_KEY":     privatePEM,
		p + "_KNOWN_HOSTS": KnownHosts(host, port, hostKeys),
		p + "_TARGET":      user + "@" + host,
		p + "_PORT":        strconv.Itoa(port),
	}
}

// KnownHosts formats host key lines the way ssh expects for host:port.
func KnownHosts(host string, port int, keys []string) string {
	name := host
	if port != 22 {
		name = "[" + host + "]:" + strconv.Itoa(port)
	} else if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		name = host // IPv6 on port 22 is written bare
	}
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %s\n", name, strings.TrimSpace(k))
	}
	return b.String()
}

// WorkflowPath is where the workflow lives in the repository.
func WorkflowPath(slug string) string { return ".github/workflows/truedeploy-" + slug + ".yml" }

// Workflow renders the GitHub Actions workflow for a project.
func Workflow(slug, projectName, branch string) string {
	p := SecretPrefix(slug)
	name := strings.NewReplacer(`'`, "", "\n", " ").Replace(projectName)
	return fmt.Sprintf(`# Managed by TrueDeploy. Deploys %[1]s to your server on every push to %[2]s.
# Turn it off from the project page in TrueDeploy (that also removes the server key).
name: 'Deploy %[1]s (TrueDeploy)'

on:
  push:
    branches: ['%[2]s']
  workflow_dispatch:

concurrency:
  group: truedeploy-%[3]s
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - name: Deploy over SSH
        env:
          SSH_KEY: ${{ secrets.%[4]s_SSH_KEY }}
          KNOWN_HOSTS: ${{ secrets.%[4]s_KNOWN_HOSTS }}
          TARGET: ${{ secrets.%[4]s_TARGET }}
          PORT: ${{ secrets.%[4]s_PORT }}
        run: |
          install -m 700 -d ~/.ssh
          printf '%%s\n' "$SSH_KEY" > ~/.ssh/truedeploy && chmod 600 ~/.ssh/truedeploy
          printf '%%s\n' "$KNOWN_HOSTS" > ~/.ssh/known_hosts
          # The server only lets this key run the project's deploy hook.
          ssh -i ~/.ssh/truedeploy -p "$PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes "$TARGET" deploy
`, name, branch, slug, p)
}
