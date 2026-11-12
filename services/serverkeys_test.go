package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"truedeploy/internal/sshx"
)

func TestAuthorizedKeysScripts(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	home := t.TempDir()
	run := func(script string) string {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+filepath.ToSlash(home))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return string(out)
	}
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	mine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMine me@laptop"
	os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(mine+"\n"), 0o600)

	_, old, _ := sshx.NewKeyPair(keyCommentPrefix + "vps-20260101")
	_, cur, _ := sshx.NewKeyPair(keyCommentPrefix + "vps-20261106")
	run(installKeyScript(old))
	run(installKeyScript(cur))
	run(installKeyScript(cur)) // idempotent
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Count(string(b), "\n") != 3 {
		t.Fatalf("after installs:\n%s", b)
	}
	run(removeKeysScript(cur))
	b, _ = os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	got := string(b)
	if !strings.Contains(got, mine) || !strings.Contains(got, cur) || strings.Contains(got, old) {
		t.Errorf("rotation must keep your keys and the new one:\n%s", got)
	}
	run(removeKeysScript(""))
	b, _ = os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.TrimSpace(string(b)) != mine {
		t.Errorf("cleanup should leave only your key:\n%s", b)
	}
}

func TestNewKeyPairParses(t *testing.T) {
	priv, line, err := sshx.NewKeyPair("truedeploy-login-x")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := sshx.PublicKeyOf(sshx.Target{Auth: sshx.AuthKey, PrivateKey: priv})
	if err != nil || !strings.HasPrefix(line, pub+" ") || !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Errorf("key pair: %v %q %q", err, pub, line)
	}
	if !strings.Contains(priv, "OPENSSH PRIVATE KEY") {
		t.Error("private key should be in OpenSSH format")
	}
}

func TestPasswordOffNeedsKeyFirst(t *testing.T) {
	svc := NewServerService(newTestStore(t)) // s1 logs in with a password
	if _, _, err := svc.DisablePasswordLogin("s1"); err == nil || !strings.Contains(err.Error(), "SSH key first") {
		t.Errorf("got %v", err)
	}
}
