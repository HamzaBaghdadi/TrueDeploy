package setup

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// findBash returns a working bash (Git Bash on Windows; the WSL stub in
// System32 is skipped because it fails without a Linux distro installed).
func findBash(t *testing.T) string {
	t.Helper()
	candidates := []string{`C:\Program Files\Git\bin\bash.exe`}
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		candidates = append([]string{p}, candidates...)
	}
	for _, p := range candidates {
		if exec.Command(p, "-c", "echo aGk= | base64 -d").Run() == nil {
			return p
		}
	}
	t.Skip("no working bash with base64")
	return ""
}

func TestWrapRunsScriptVerbatim(t *testing.T) {
	bash := findBash(t)
	// Quotes, $vars, heredocs and backslashes must survive the trip intact.
	script := `X='single "double" $notexpanded'
echo "$X"
cat <<'EOF'
back\slash ` + "`tick`" + `
EOF`
	out, err := exec.Command(bash, "-c", Wrap(script, Direct)).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := "single \"double\" $notexpanded\nback\\slash `tick`\n"
	if strings.ReplaceAll(string(out), "\r", "") != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestWrapFailsOnFirstError(t *testing.T) {
	bash := findBash(t)
	out, err := exec.Command(bash, "-c", Wrap("false\necho should-not-run", Direct)).CombinedOutput()
	if err == nil || strings.Contains(string(out), "should-not-run") {
		t.Fatalf("expected set -e to stop the script; err=%v out=%q", err, out)
	}
}

func TestWrapStdinIsNotTheScript(t *testing.T) {
	bash := findBash(t)
	// A command that reads stdin must see EOF, not leftover input (e.g. a sudo password).
	cmd := exec.Command(bash, "-c", Wrap(`cat; echo end`, Direct))
	cmd.Stdin = strings.NewReader("hunter2\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.Contains(string(out), "hunter2") {
		t.Fatalf("script stdin leaked: %q", out)
	}
}

func TestWrapPrivilegePrefixes(t *testing.T) {
	if !strings.HasPrefix(Wrap("true", SudoNoPassword), "sudo -n ") {
		t.Error("SudoNoPassword should use sudo -n")
	}
	if !strings.HasPrefix(Wrap("true", SudoWithPassword), "sudo -S -p '' ") {
		t.Error("SudoWithPassword should use sudo -S with an empty prompt")
	}
	if PasswordInput(SudoWithPassword, "pw") != "pw\n" || PasswordInput(Direct, "pw") != "" {
		t.Error("PasswordInput only sends the password for sudo -S")
	}
}

func TestValidUser(t *testing.T) {
	for _, u := range []string{"deploy", "app_1", "_svc"} {
		if !ValidUser(u) {
			t.Errorf("%q should be valid", u)
		}
	}
	for _, u := range []string{"", "Root", "a b", "x;rm -rf /", "$(id)", "1abc"} {
		if ValidUser(u) {
			t.Errorf("%q should be invalid", u)
		}
	}
}
