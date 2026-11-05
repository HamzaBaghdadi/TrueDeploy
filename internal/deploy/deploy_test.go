package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findBash(t *testing.T) string {
	t.Helper()
	candidates := []string{`C:\Program Files\Git\bin\bash.exe`}
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		candidates = append([]string{p}, candidates...)
	}
	for _, p := range candidates {
		if exec.Command(p, "-c", "command -v git && echo aGk= | base64 -d").Run() == nil {
			return p
		}
	}
	t.Skip("no bash with git and base64")
	return ""
}

// bashPath converts a Windows path for Git Bash (C:\x -> /c/x).
func bashPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 1 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

// env sets up a fake HOME, a fake docker that logs its arguments, and a
// local Git repo with one commit. It returns a runner for scripts.
func env(t *testing.T, files map[string]string) (run func(script string) (string, error), repo, home, dockerLog string) {
	t.Helper()
	bash := findBash(t)
	dir := t.TempDir()
	home = filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	repo = filepath.Join(dir, "repo")
	dockerLog = filepath.Join(dir, "docker.log")
	for _, d := range []string{home, bin, repo} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho \"$*\" >> '"+bashPath(dockerLog)+"'\n"), 0o755)

	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home), "PATH="+bashPath(bin)+":/usr/bin:/mingw64/bin:/bin")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	for name, body := range files {
		os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644)
	}
	git("add", ".")
	git("commit", "-q", "-m", "first commit")
	return run, repo, home, dockerLog
}

func markers(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := ParseMarker(line); ok {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

func TestComposeDeployClonesThenUpdates(t *testing.T) {
	run, repo, home, dockerLog := env(t, map[string]string{"compose.yaml": "services: {}\n"})
	spec := Spec{Slug: "my-app", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose,
		Env: map[string]string{"DB_URL": "postgres://u:p@db/x?a=1&b='q'", "PORT": "3000"}}

	out, err := run(Script(spec))
	if err != nil {
		t.Fatalf("first deploy: %v\n%s", err, out)
	}
	m := markers(out)
	if m["message"] != "first commit" || len(m["commit"]) < 7 {
		t.Fatalf("markers = %v\n%s", m, out)
	}
	envBody, _ := os.ReadFile(filepath.Join(home, "apps", "my-app", ".env"))
	if string(envBody) != "DB_URL=postgres://u:p@db/x?a=1&b='q'\nPORT=3000\n" {
		t.Fatalf(".env = %q", envBody)
	}
	log, _ := os.ReadFile(dockerLog)
	if !strings.Contains(string(log), "compose -p my-app -f compose.yaml up -d --build --remove-orphans") {
		t.Fatalf("docker calls:\n%s", log)
	}

	// Second deploy picks up a new commit through the update path.
	c := exec.Command("git", "-C", repo, "commit", "-q", "--allow-empty", "-m", "second commit")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	out, err = run(Script(spec))
	if err != nil {
		t.Fatalf("second deploy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "==> Updating") || markers(out)["message"] != "second commit" {
		t.Fatalf("expected update to second commit:\n%s", out)
	}
}

func TestAccessCheck(t *testing.T) {
	run, repo, _, _ := env(t, map[string]string{"README.md": "hi"})
	ok := Spec{RepoURL: "file://" + bashPath(repo), Branch: "main"}
	if out, err := run(AccessScript(ok)); err != nil || !strings.Contains(out, "Access OK") {
		t.Fatalf("expected access OK: %v\n%s", err, out)
	}
	out, err := run(AccessScript(Spec{RepoURL: ok.RepoURL, Branch: "nope"}))
	if err == nil || !strings.Contains(out, "Branch 'nope' doesn't exist") {
		t.Fatalf("want missing-branch error, got %v\n%s", err, out)
	}
	out, err = run(Script(Spec{Slug: "x", RepoURL: ok.RepoURL, Branch: "nope", Runtime: RuntimeCompose}))
	if err == nil || !strings.Contains(out, "doesn't exist") || strings.Contains(out, "Cloning") {
		t.Fatalf("deploy should stop at the access check: %v\n%s", err, out)
	}
}

func TestComposeDeployFailsClearlyWithoutComposeFile(t *testing.T) {
	run, repo, _, _ := env(t, map[string]string{"README.md": "hi"})
	out, err := run(Script(Spec{Slug: "x", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose}))
	if err == nil || !strings.Contains(out, "No compose.yaml") {
		t.Fatalf("want a clear missing-compose error, got err=%v\n%s", err, out)
	}
}

func TestDockerfileDeploy(t *testing.T) {
	run, repo, _, dockerLog := env(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	spec := Spec{Slug: "web", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeDockerfile, HostPort: 8080, ContainerPort: 3000}
	if out, err := run(Script(spec)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	log, _ := os.ReadFile(dockerLog)
	for _, want := range []string{"build -t truedeploy/web:", "rm -f td-web", "run -d --name td-web --label truedeploy.project=web --restart unless-stopped --env-file .env -p 8080:3000"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("missing docker call %q in:\n%s", want, log)
		}
	}
}

func TestScriptsParse(t *testing.T) {
	bash := findBash(t)
	specs := []Spec{
		{Slug: "a", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeCompose},
		{Slug: "a", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeCompose, ComposeFile: "deploy/prod.yml"},
		{Slug: "a", RepoURL: "https://x.dev/o/r", Branch: "release/1.0", Runtime: RuntimeDockerfile, HostPort: 80, ContainerPort: 8080},
	}
	for _, s := range specs {
		for name, script := range map[string]string{"deploy": Script(s), "status": StatusScript(s.Slug), "stop": StopScript(s), "access": AccessScript(s)} {
			if out, err := exec.Command(bash, "-n", "-c", script).CombinedOutput(); err != nil {
				t.Errorf("%s script (%s): %v\n%s", name, s.Runtime, err, out)
			}
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Spec{Slug: "api", RepoURL: "git@github.com:me/api.git", Branch: "main", Runtime: RuntimeCompose}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Spec{
		{Slug: "api", RepoURL: "not a url", Branch: "main", Runtime: RuntimeCompose},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "main; rm -rf /", Runtime: RuntimeCompose},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "--upload-pack=x", Runtime: RuntimeCompose},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, ComposeFile: "../../etc/passwd"},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeDockerfile},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, Env: map[string]string{"BAD KEY": "x"}},
		{Slug: "api", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose, Env: map[string]string{"K": "a\nb"}},
		{Slug: "API!", RepoURL: ok.RepoURL, Branch: "main", Runtime: RuntimeCompose},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("expected %+v to be rejected", s)
		}
	}
}

func TestSlugAndDeployKeyURL(t *testing.T) {
	if got := Slug("  My Cool App!! "); got != "my-cool-app" {
		t.Errorf("Slug = %q", got)
	}
	cases := map[string]string{
		"git@github.com:acme/api.git":     "https://github.com/acme/api/settings/keys/new",
		"https://github.com/acme/api":     "https://github.com/acme/api/settings/keys/new",
		"git@gitlab.com:grp/sub/proj.git": "https://gitlab.com/grp/sub/proj/-/settings/repository#js-deploy-keys-settings",
		"git@example.com:acme/api.git":    "",
	}
	for in, want := range cases {
		if got := DeployKeyURL(in); got != want {
			t.Errorf("DeployKeyURL(%q) = %q, want %q", in, got, want)
		}
	}
	if !UsesSSH("git@github.com:a/b.git") || UsesSSH("https://github.com/a/b") {
		t.Error("UsesSSH")
	}
}
