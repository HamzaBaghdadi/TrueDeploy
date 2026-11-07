package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// healthEnv adds fake curl to the deploy fake: it answers 502 while the
// deployed checkout contains BROKEN, else 200. A broken app keeps 502ing.
func healthEnv(t *testing.T) (run func(string) (string, error), repo, home string, commit func(content, msg string)) {
	t.Helper()
	run, repo, home, dockerLog := env(t, map[string]string{"compose.yaml": "services: {}\n", "app.txt": "v1 ok\n"})
	bin := filepath.Join(filepath.Dir(dockerLog), "bin")
	os.WriteFile(filepath.Join(bin, "curl"), []byte(`#!/bin/sh
echo "$*" >> '`+bashPath(filepath.Join(filepath.Dir(dockerLog), "curl.log"))+`'
if grep -q BROKEN "$HOME/apps/shop/app.txt"; then printf 502; else printf 200; fi
`), 0o755)
	// "sleep" is instant so the health loop runs fast.
	os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	// Git for Windows' bash.exe launcher puts its own folders (with a real
	// curl) first in PATH; make sure the fakes win.
	inner := run
	run = func(script string) (string, error) {
		return inner("export PATH='" + bashPath(bin) + "':\"$PATH\"\n" + script)
	}
	commit = func(content, msg string) {
		os.WriteFile(filepath.Join(repo, "app.txt"), []byte(content), 0o644)
		for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", msg}} {
			c := exec.Command("git", append([]string{"-C", repo}, args...)...)
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
	}
	return run, repo, home, commit
}

func TestHealthCheckAndRollback(t *testing.T) {
	run, repo, home, commit := healthEnv(t)
	spec := Spec{Slug: "shop", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose,
		HealthCheck: true, HealthPort: 8080, HealthPath: "/health", HealthTimeout: 5, AutoRollback: true}

	out, err := run(Script(spec))
	if err != nil || !strings.Contains(out, "Healthy: the app answered 200") || !strings.Contains(out, "Deployed ") {
		t.Fatalf("healthy deploy: %v\n%s", err, out)
	}
	good, _ := os.ReadFile(filepath.Join(home, "apps", "shop", ".git", "truedeploy-last-good"))
	if len(strings.TrimSpace(string(good))) != 40 {
		t.Fatalf("last good commit not recorded: %q", good)
	}
	curlLog, _ := os.ReadFile(filepath.Join(filepath.Dir(repo), "curl.log"))
	if !strings.Contains(string(curlLog), "http://127.0.0.1:8080/health") {
		t.Errorf("curl called with: %s", curlLog)
	}

	// A broken commit fails its check and is rolled back automatically.
	commit("BROKEN\n", "break it")
	out, err = run(Script(spec))
	if err == nil {
		t.Fatalf("an unhealthy deploy must fail:\n%s", out)
	}
	m := markers(out)
	if !strings.Contains(m["unhealthy"], "answered 502") || m["rolledback"] == "" || !strings.Contains(out, "Rolled back to") {
		t.Errorf("rollback markers %v:\n%s", m, out)
	}
	// (Windows git may turn line endings into CRLF on checkout.)
	if b, _ := os.ReadFile(filepath.Join(home, "apps", "shop", "app.txt")); strings.TrimSpace(string(b)) != "v1 ok" {
		t.Errorf("the good version should be checked out again, got %q", b)
	}
	if again, _ := os.ReadFile(filepath.Join(home, "apps", "shop", ".git", "truedeploy-last-good")); string(again) != string(good) {
		t.Error("the last good commit must not change after a failed deploy")
	}

	// Without auto-rollback the broken version stays and the deploy fails.
	spec.AutoRollback = false
	out, err = run(Script(spec))
	if err == nil || strings.Contains(out, "Rolling back") || !strings.Contains(out, "isn't healthy: ") {
		t.Errorf("no rollback expected: %v\n%s", err, out)
	}

	// A fix is healthy again and becomes the new last good commit.
	commit("v3 ok\n", "fix")
	spec.AutoRollback = true
	if out, err := run(Script(spec)); err != nil {
		t.Fatalf("fixed deploy: %v\n%s", err, out)
	}
	if now, _ := os.ReadFile(filepath.Join(home, "apps", "shop", ".git", "truedeploy-last-good")); string(now) == string(good) {
		t.Error("the fix should be the new last good commit")
	}
}

func TestHealthValidation(t *testing.T) {
	ok := Spec{Slug: "a", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeCompose, HealthCheck: true, HealthPort: 3000, HealthPath: "/api/health?full=1", HealthTimeout: 120}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Spec{
		{Slug: "a", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, HealthCheck: true, HealthPath: "health"},
		{Slug: "a", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, HealthCheck: true, HealthPath: "/x'; rm -rf /"},
		{Slug: "a", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, HealthCheck: true, HealthTimeout: 2},
	} {
		if bad.Validate() == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
	// No health check: no curl, and the last good commit is still recorded.
	if s := Script(Spec{Slug: "a", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose}); strings.Contains(s, "td_health") || !strings.Contains(s, "truedeploy-last-good") {
		t.Error("health check must be opt-in")
	}
}
