package autodeploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func findBash(t *testing.T) string {
	t.Helper()
	candidates := []string{`C:\Program Files\Git\bin\bash.exe`}
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		candidates = append([]string{p}, candidates...)
	}
	for _, p := range candidates {
		if exec.Command(p, "-c", "echo aGk= | base64 -d").Run() == nil {
			return p
		}
	}
	t.Skip("no bash")
	return ""
}

func bashPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 1 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

func runner(t *testing.T) (func(string) (string, error), string) {
	bash := findBash(t)
	home := t.TempDir()
	return func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}, home
}

func TestKeyPair(t *testing.T) {
	kp, err := NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(kp.PrivatePEM))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))); got != kp.PublicKey {
		t.Fatalf("public key mismatch: %q vs %q", got, kp.PublicKey)
	}
}

func TestInstallLocksKeyToHookAndUninstallCleansUp(t *testing.T) {
	run, home := runner(t)
	ak := filepath.Join(home, ".ssh", "authorized_keys")
	os.MkdirAll(filepath.Dir(ak), 0o700)
	os.WriteFile(ak, []byte("ssh-ed25519 AAAAmine me@laptop\n"), 0o600)

	hook := HookScript("shop", `echo "::td::commit=abc1234"; echo "::td::message=Fix cart"; echo "deploying"`)
	out, err := run(InstallScript("shop", hook, "ssh-ed25519 AAAAci1"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "::td::home=") {
		t.Fatalf("missing home marker:\n%s", out)
	}
	// Reinstalling replaces the old CI key instead of adding a second one.
	if out, err := run(InstallScript("shop", hook, "ssh-ed25519 AAAAci2")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ := os.ReadFile(ak)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[0] != "ssh-ed25519 AAAAmine me@laptop" {
		t.Fatalf("authorized_keys = %q", b)
	}
	if !strings.HasPrefix(lines[1], `command="`) || !strings.Contains(lines[1], `/truedeploy/hooks/shop.sh",restrict ssh-ed25519 AAAAci2 truedeploy-ci-shop`) {
		t.Fatalf("CI key must be locked to the hook with restrict: %q", lines[1])
	}

	// Run the installed hook the way sshd would, and check it records history.
	hookPath := bashPath(filepath.Join(home, "truedeploy", "hooks", "shop.sh"))
	if out, err := run(hookPath); err != nil || !strings.Contains(out, "deploying") {
		t.Fatalf("hook run: %v\n%s", err, out)
	}
	hist, _ := run(HistoryScript("shop"))
	runs, log := ParseHistory(strings.Split(strings.TrimRight(hist, "\n"), "\n"))
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].Commit != "abc1234" || runs[0].Message != "Fix cart" {
		t.Fatalf("runs = %+v\n%s", runs, hist)
	}
	if len(log) == 0 || log[len(log)-1] != "deploying" {
		t.Fatalf("last log = %q", log)
	}

	if out, err := run(UninstallScript("shop")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ = os.ReadFile(ak)
	if strings.TrimSpace(string(b)) != "ssh-ed25519 AAAAmine me@laptop" {
		t.Fatalf("uninstall must only remove the CI key: %q", b)
	}
	if _, err := os.Stat(filepath.Join(home, "truedeploy", "hooks", "shop.sh")); !os.IsNotExist(err) {
		t.Fatal("hook script should be removed")
	}
}

func TestFailedHookRecordsError(t *testing.T) {
	run, home := runner(t)
	hook := HookScript("api", `echo "::td::commit=def5678"; echo "Branch 'x' doesn't exist." >&2; exit 1`)
	if out, err := run(InstallScript("api", hook, "ssh-ed25519 AAAAci")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := run(bashPath(filepath.Join(home, "truedeploy", "hooks", "api.sh"))); err == nil {
		t.Fatal("hook must exit non-zero when the deploy fails")
	}
	hist, _ := run(HistoryScript("api"))
	runs, _ := ParseHistory(strings.Split(hist, "\n"))
	if len(runs) != 1 || runs[0].Status != "failed" || runs[0].Message != "Branch 'x' doesn't exist." {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestWorkflowAndSecrets(t *testing.T) {
	wf := Workflow("my-shop", "My Shop", "release/v1")
	for _, want := range []string{
		"branches: ['release/v1']",
		"secrets.TRUEDEPLOY_MY_SHOP_SSH_KEY",
		"StrictHostKeyChecking=yes",
		"group: truedeploy-my-shop",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q:\n%s", want, wf)
		}
	}
	sec := Secrets("my-shop", "deployer", "vps.example.com", 2222, "PEM", []string{"ssh-ed25519 AAAAhost"})
	if sec["TRUEDEPLOY_MY_SHOP_TARGET"] != "deployer@vps.example.com" || sec["TRUEDEPLOY_MY_SHOP_PORT"] != "2222" {
		t.Fatalf("secrets = %v", sec)
	}
	if sec["TRUEDEPLOY_MY_SHOP_KNOWN_HOSTS"] != "[vps.example.com]:2222 ssh-ed25519 AAAAhost\n" {
		t.Fatalf("known_hosts = %q", sec["TRUEDEPLOY_MY_SHOP_KNOWN_HOSTS"])
	}
	if KnownHosts("1.2.3.4", 22, []string{"k1", "k2"}) != "1.2.3.4 k1\n1.2.3.4 k2\n" {
		t.Fatal("port 22 hosts are written without brackets")
	}
}
