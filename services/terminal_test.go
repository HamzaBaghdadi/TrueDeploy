package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContainerShellCommand(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	dir := t.TempDir()
	logf := filepath.ToSlash(filepath.Join(dir, "docker.log"))
	os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\necho \"$*\" >> '"+logf+"'\n"), 0o755)
	path := filepath.ToSlash(dir)
	if len(path) > 1 && path[1] == ':' {
		path = "/" + strings.ToLower(path[:1]) + path[2:]
	}
	cmd := exec.Command(bash, "-c", "export PATH='"+path+"':\"$PATH\"\n"+ContainerShellCommand("shop-web-1"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "docker.log"))
	if !strings.Contains(string(b), "exec -it shop-web-1 sh -c command -v bash >/dev/null && exec bash || exec sh") {
		t.Errorf("docker calls:\n%s", b)
	}
	svc := NewTerminalService(newTestStore(t))
	for _, bad := range []string{"x'; rm -rf ~", "../x", "", "a b"} {
		if _, err := svc.OpenContainer("s1", bad, 80, 24); err == nil || !strings.Contains(err.Error(), "isn't a container name") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
