package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"truedeploy/internal/deploy"
	"truedeploy/internal/sshx"
)

// keyComment marks login keys TrueDeploy created, so a later rotation can
// remove them without touching keys you added yourself.
func keyComment(serverName string) string {
	return "truedeploy-login-" + deploy.Slug(serverName) + "-" + time.Now().UTC().Format("20060102")
}

const keyCommentPrefix = "truedeploy-login-"

// installKeyScript adds an authorized_keys line (idempotent).
func installKeyScript(line string) string {
	return fmt.Sprintf(`mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh"
touch "$HOME/.ssh/authorized_keys" && chmod 600 "$HOME/.ssh/authorized_keys"
L=$(echo %s | base64 -d)
grep -qxF "$L" "$HOME/.ssh/authorized_keys" || printf '%%s\n' "$L" >> "$HOME/.ssh/authorized_keys"
echo "Key installed."`, base64.StdEncoding.EncodeToString([]byte(line)))
}

// removeKeysScript drops authorized_keys lines with TrueDeploy's comment
// prefix, except keep. Other keys are never touched.
func removeKeysScript(keep string) string {
	return fmt.Sprintf(`F="$HOME/.ssh/authorized_keys"
[ -f "$F" ] || exit 0
K=$(echo %s | base64 -d)
awk -v keep="$K" -v p=%q '$0 == keep || index($0, " " p) == 0 { print }' "$F" > "$F.td" && cat "$F.td" > "$F" && rm -f "$F.td"
echo "Old TrueDeploy keys removed."`, base64.StdEncoding.EncodeToString([]byte(keep)), keyCommentPrefix)
}

// UseNewKey creates a new SSH key for this server, installs it with the
// current login, checks that logging in with it works, and only then saves
// it in place of the password (or the previous key). If anything fails,
// the saved login is unchanged.
func (s *ServerService) UseNewKey(serverID string) (string, error) {
	t, srv, err := savedTarget(s.store, serverID)
	if err != nil {
		return "", err
	}
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return "", friendly(err)
	}
	defer c.Close()

	priv, line, err := sshx.NewKeyPair(keyComment(srv.Name))
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := collect(ctx, c, installKeyScript(line)); err != nil {
		return "", fmt.Errorf("couldn't add the key to ~/.ssh/authorized_keys: %w", err)
	}

	// Log in with the new key on a separate connection before relying on it.
	nt := t
	nt.Auth, nt.PrivateKey, nt.KeyPath, nt.Passphrase, nt.Password = sshx.AuthKey, priv, "", "", ""
	c2, err := sshx.Dial(nt, dialTimeout)
	if err != nil {
		_, _ = collect(ctx, c, removeKeysScript("")) // take the unusable key back out
		return "", fmt.Errorf("the server didn't accept the new key, so nothing changed (its SSH server may not allow key logins: PubkeyAuthentication no). %v", friendly(err))
	}
	defer c2.Close()
	if out, err := c2.Run("echo ok"); err != nil || !strings.Contains(out, "ok") {
		return "", errors.New("logging in with the new key didn't work, so nothing changed")
	}

	_, sec, err := s.store.Get(serverID)
	if err != nil {
		return "", err
	}
	wasPassword := srv.Auth != string(sshx.AuthKey)
	srv.Auth, srv.KeyPath = string(sshx.AuthKey), ""
	sec.Password, sec.PrivateKey, sec.Passphrase = "", priv, ""
	if err := s.store.Put(srv, sec); err != nil {
		return "", err
	}
	// The new key works and is saved: older TrueDeploy keys can go.
	_, _ = collect(ctx, c2, removeKeysScript(line))
	if wasPassword {
		return "TrueDeploy now logs in with its own SSH key; the saved password was deleted. Your password still works for you, and you can turn off password logins below.", nil
	}
	return "The login key was replaced and the old TrueDeploy key removed from the server. Keys you added yourself weren't touched.", nil
}

// sshdDropIn turns password logins off; Ubuntu and Debian read sshd_config.d.
const sshdDropIn = "/etc/ssh/sshd_config.d/10-truedeploy.conf"

// AdminPasswordOffCommands are what an admin runs when the SSH user has no sudo.
func AdminPasswordOffCommands() string {
	return fmt.Sprintf(`printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\n' | sudo tee %s
sudo sshd -t && sudo systemctl reload ssh`, sshdDropIn)
}

// DisablePasswordLogin turns off SSH password logins for the whole server
// (it needs passwordless sudo). TrueDeploy must already log in with a key.
// The second return value is set when the user has no sudo: commands for
// an admin to run.
func (s *ServerService) DisablePasswordLogin(serverID string) (string, string, error) {
	t, srv, err := savedTarget(s.store, serverID)
	if err != nil {
		return "", "", err
	}
	if srv.Auth != string(sshx.AuthKey) {
		return "", "", errors.New("switch TrueDeploy to an SSH key first, or you'd lock it out")
	}
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return "", "", friendly(err)
	}
	defer c.Close()
	script := fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
if ! command -v sudo >/dev/null || ! sudo -n true 2>/dev/null; then echo "::td::needsadmin"; exit 0; fi
grep -qiE '^\s*Include\s+/etc/ssh/sshd_config.d' /etc/ssh/sshd_config || fail "This server's SSH config doesn't read sshd_config.d. Set PasswordAuthentication no in /etc/ssh/sshd_config yourself."
printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\n' | sudo -n tee %[1]s >/dev/null
if ! sudo -n sshd -t 2>/tmp/td-sshd.err; then sudo -n rm -f %[1]s; fail "The SSH config check failed, so nothing changed: $(cat /tmp/td-sshd.err)"; fi
sudo -n systemctl reload ssh 2>/dev/null || sudo -n systemctl reload sshd 2>/dev/null || sudo -n service ssh reload || fail "Couldn't reload the SSH server."
echo "Password logins are off."`, sshdDropIn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	lines, err := collect(ctx, c, script)
	if err != nil {
		return "", "", err
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "::td::needsadmin" {
			return "", AdminPasswordOffCommands(), nil
		}
	}
	// Make sure TrueDeploy can still get in after the reload.
	c2, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return "", "", fmt.Errorf("password logins are off, but reconnecting failed: %v. Use your hosting provider's console if you're locked out", friendly(err))
	}
	c2.Close()
	return "Password logins are off for every account on " + srv.Name + ". Logging in now needs an SSH key.", "", nil
}
