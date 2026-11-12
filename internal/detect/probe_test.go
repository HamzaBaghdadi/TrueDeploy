package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"truedeploy/internal/deploy"
)

func TestProbeScriptEndToEnd(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	if exec.Command(bash, "-c", "command -v git && command -v base64").Run() != nil {
		t.Skip("no bash with git")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	files := map[string]string{
		"frontend/package.json":   `{"dependencies":{"next":"15"}}`,
		"frontend/next.config.js": "module.exports = { output: 'export' }",
		"frontend/app/page.tsx":   "export default function Page() {}",
		"backend/package.json":    `{"dependencies":{"@nestjs/core":"10"}}`,
	}
	for f, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(repoDir, f)), 0o755)
		os.WriteFile(filepath.Join(repoDir, f), []byte(c), 0o644)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "x"}} {
		c := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	home := filepath.Join(dir, "home")
	os.MkdirAll(home, 0o755)
	url := "file:///" + strings.TrimPrefix(filepath.ToSlash(repoDir), "/")
	cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+deploy.ProbeScript("shop", url, "main", KeyNames()))
	cmd.Env = append(os.Environ(), "HOME="+filepath.ToSlash(home))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	r := ParseProbe(strings.Split(string(out), "\n"))
	if len(r.Files) != 4 || r.Contents["frontend/next.config.js"] == "" || r.Contents["frontend/app/page.tsx"] != "" {
		t.Fatalf("probe: files=%v contents=%v", r.Files, r.Contents)
	}
	got := Detect(r)
	if len(got) != 2 || got[0].Folder != "backend" || got[0].Preset != "nestjs" || got[1].Preset != "nextjs-export" {
		t.Errorf("detect: %+v", got)
	}

	bad := exec.Command(bash, "-c", "set -euo pipefail\n"+deploy.ProbeScript("shop", url, "nope", KeyNames()))
	bad.Env = cmd.Env
	if out, err := bad.CombinedOutput(); err == nil {
		t.Errorf("missing branch should fail: %s", out)
	}
}
