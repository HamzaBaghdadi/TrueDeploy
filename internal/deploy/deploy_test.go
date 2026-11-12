package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func findBash(t *testing.T) string {
	t.Helper()
	candidates := []string{`C:\Program Files\Git\bin\bash.exe`}
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
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

func TestRollbackToEarlierCommit(t *testing.T) {
	run, repo, home, _ := env(t, map[string]string{"compose.yaml": "services: {}\n", "VERSION": "1\n"})
	spec := Spec{Slug: "app", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose}
	out, err := run(Script(spec))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	firstSHA := markers(out)["sha"]
	if len(firstSHA) != 40 {
		t.Fatalf("full sha marker missing: %v", markers(out))
	}

	os.WriteFile(filepath.Join(repo, "VERSION"), []byte("2\n"), 0o644)
	c := exec.Command("git", "-C", repo, "commit", "-qam", "v2")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	if out, err := run(Script(spec)); err != nil || markers(out)["message"] != "v2" {
		t.Fatalf("deploy v2: %v\n%s", err, out)
	}

	spec.Commit = firstSHA
	out, err = run(Script(spec))
	if err != nil {
		t.Fatalf("rollback: %v\n%s", err, out)
	}
	if m := markers(out); m["sha"] != firstSHA || m["message"] != "first commit" || !strings.Contains(out, "Rolling back") {
		t.Fatalf("rollback landed on %v\n%s", m, out)
	}
	if v, _ := os.ReadFile(filepath.Join(home, "apps", "app", "VERSION")); strings.TrimSpace(string(v)) != "1" {
		t.Fatalf("working tree not rolled back: VERSION=%q", v)
	}

	spec.Commit = "not-a-sha"
	if spec.Validate() == nil {
		t.Fatal("short or invalid SHAs must be rejected")
	}
}

func TestDeployJoinsDatabaseNetwork(t *testing.T) {
	run, repo, home, dockerLog := env(t, map[string]string{"compose.yaml": "services: {}\n"})
	// A fake docker that knows two compose services and has no network yet.
	bin := filepath.Join(filepath.Dir(dockerLog), "bin", "docker")
	os.WriteFile(bin, []byte(`#!/bin/sh
echo "$*" >> '`+bashPath(dockerLog)+`'
case "$*" in
  *"config --services"*) printf 'web\ndb\n' ;;
  *"compose -f compose.yaml config") echo "services: {}" ;;
  "network inspect"*) exit 1 ;;
esac
`), 0o755)

	spec := Spec{Slug: "shop", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose, Network: "truedeploy"}
	if out, err := run(Script(spec)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	log, _ := os.ReadFile(dockerLog)
	for _, want := range []string{"network create truedeploy", "compose -p shop -f compose.yaml -f .truedeploy.compose.yml up -d"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("missing %q in docker calls:\n%s", want, log)
		}
	}
	ovr, _ := os.ReadFile(filepath.Join(home, "apps", "shop", ".truedeploy.compose.yml"))
	for _, want := range []string{"  web:\n    networks:\n      default: {}\n      truedeploy: {}", "  db:\n", "truedeploy:\n    external: true"} {
		if !strings.Contains(string(ovr), want) {
			t.Errorf("override missing %q:\n%s", want, ovr)
		}
	}

	// Dockerfile projects join with --network.
	run2, repo2, _, log2 := env(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	d := Spec{Slug: "web", RepoURL: "file://" + bashPath(repo2), Branch: "main", Runtime: RuntimeDockerfile, HostPort: 8080, ContainerPort: 80, Network: "truedeploy"}
	if out, err := run2(Script(d)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if l, _ := os.ReadFile(log2); !strings.Contains(string(l), "-p 8080:80 --network truedeploy truedeploy/web:") {
		t.Errorf("dockerfile run should join the network:\n%s", l)
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

func TestPerProjectDeployKey(t *testing.T) {
	run, _, _, _ := env(t, map[string]string{"README.md": "hi"})
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		if out, err := run("command -v ssh-keygen"); err != nil {
			t.Skipf("ssh-keygen not available: %s", out)
		}
	}
	// Before a project key exists, git must not point at it.
	out, err := run(prelude("shop") + `echo "$GIT_SSH_COMMAND"`)
	if err != nil || strings.Contains(out, "truedeploy_shop") {
		t.Fatalf("project key used before it exists: %v\n%s", err, out)
	}

	first, err := run(KeyScript("shop"))
	if err != nil || !strings.HasPrefix(strings.TrimSpace(first), "ssh-ed25519 ") || !strings.Contains(first, "truedeploy-shop@") {
		t.Fatalf("KeyScript: %v\n%s", err, first)
	}
	second, _ := run(KeyScript("shop"))
	if first != second {
		t.Fatal("KeyScript must reuse the existing key")
	}
	other, _ := run(KeyScript("blog"))
	if other == first {
		t.Fatal("each project needs its own key")
	}

	out, _ = run(prelude("shop") + `echo "$GIT_SSH_COMMAND"`)
	if !strings.Contains(out, "truedeploy_shop") {
		t.Fatalf("deploy should use the project key once it exists: %s", out)
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
	for _, want := range []string{"build -f ./Dockerfile -t truedeploy/web:", "rm -f td-web", "run -d --name td-web --label truedeploy.project=web --restart unless-stopped --env-file .env -p 8080:3000"} {
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
		for name, script := range map[string]string{"deploy": Script(s), "status": StatusScript(s.Slug), "stop": StopScript(s), "access": AccessScript(s), "logs": LogsScript(s, 100, true)} {
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

func TestInlineComposeDeployNeedsNoGit(t *testing.T) {
	run, _, home, dockerLog := env(t, map[string]string{"README": "unused"})
	compose := "services:\n  web:\n    image: nginx:1\n    ports: [\"127.0.0.1:${PORT}:80\"]\n"
	spec := Spec{Slug: "site", Runtime: RuntimeCompose, Compose: compose, Env: map[string]string{"PORT": "8100"}}
	check := spec
	check.RepoURL = "git@github.com:o/r.git" // file:// URLs are only for tests
	if err := check.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := run(Script(spec))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	written, _ := os.ReadFile(filepath.Join(home, "apps", "site", "compose.yaml"))
	if string(written) != compose {
		t.Errorf("compose.yaml = %q", written)
	}
	envf, _ := os.ReadFile(filepath.Join(home, "apps", "site", ".env"))
	if string(envf) != "PORT=8100\n" {
		t.Errorf(".env = %q", envf)
	}
	m := markers(out)
	if len(m["commit"]) != 7 || m["sha"] != "" || !strings.HasPrefix(m["message"], "App configuration") {
		t.Errorf("markers = %v (no sha, so no rollback)", m)
	}
	log, _ := os.ReadFile(dockerLog)
	for _, want := range []string{"compose -p site -f compose.yaml pull", "compose -p site -f compose.yaml up -d"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("missing %q in docker calls:\n%s", want, log)
		}
	}
	if !strings.Contains(out, "Deployed "+m["commit"]) {
		t.Errorf("should finish with the revision:\n%s", out)
	}

	// Same file, same revision; a changed file, a new one.
	out2, _ := run(Script(spec))
	spec.Compose += "# changed\n"
	out3, _ := run(Script(spec))
	if markers(out2)["commit"] != m["commit"] || markers(out3)["commit"] == m["commit"] {
		t.Errorf("revisions: %s %s %s", m["commit"], markers(out2)["commit"], markers(out3)["commit"])
	}

	for _, bad := range []Spec{
		{Slug: "site", Runtime: RuntimeDockerfile, Compose: compose, HostPort: 1, ContainerPort: 1},
		{Slug: "site", Runtime: RuntimeCompose, Compose: compose, ComposeFile: "x.yml"},
		{Slug: "site", Runtime: RuntimeCompose, Compose: compose, Commit: strings.Repeat("a", 40)},
	} {
		if bad.Validate() == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
}

func TestPresetDeployWritesFilesIntoFolder(t *testing.T) {
	run, _, home, dockerLog := env(t, map[string]string{"README": "monorepo"})
	// Simulate a monorepo: the frontend lives in web/.
	repo := filepath.Join(filepath.Dir(dockerLog), "repo")
	os.MkdirAll(filepath.Join(repo, "web"), 0o755)
	os.WriteFile(filepath.Join(repo, "web", "package.json"), []byte("{}"), 0o644)
	gitCommit(t, repo)

	spec := Spec{Slug: "shop", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeDockerfile,
		HostPort: 8081, ContainerPort: 80, Folder: "web", LocalOnly: true, PresetName: "React (Vite)",
		Files: map[string]string{".truedeploy/Dockerfile": "FROM nginx\n", ".truedeploy/nginx.conf": "server {}\n"},
		Env:   map[string]string{"VITE_API": "/api"}}
	check := spec
	check.RepoURL = "git@github.com:o/r.git" // file:// URLs are only for tests
	if err := check.Validate(); err != nil {
		t.Fatal(err)
	}
	out, err := run(Script(spec))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	app := filepath.Join(home, "apps", "shop", "web")
	for name, want := range map[string]string{".truedeploy/Dockerfile": "FROM nginx\n", ".truedeploy/nginx.conf": "server {}\n", ".env": "VITE_API=/api\n"} {
		if got, _ := os.ReadFile(filepath.Join(app, name)); string(got) != want {
			t.Errorf("%s = %q", name, got)
		}
	}
	log, _ := os.ReadFile(dockerLog)
	if !strings.Contains(string(log), `build -f web/.truedeploy/Dockerfile -t truedeploy/shop:`) || !strings.Contains(string(log), " web\n") {
		t.Errorf("build call:\n%s", log)
	}
	if !strings.Contains(string(log), "-p 127.0.0.1:8081:80 ") {
		t.Errorf("local-only publish:\n%s", log)
	}
	if !strings.Contains(out, "Using the React (Vite) preset") {
		t.Errorf("output:\n%s", out)
	}

	for _, bad := range []Spec{
		{Slug: "x", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeDockerfile, HostPort: 1, ContainerPort: 1, Folder: "../etc"},
		{Slug: "x", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeDockerfile, HostPort: 1, ContainerPort: 1, Files: map[string]string{"Dockerfile": "x"}},
		{Slug: "x", RepoURL: "git@github.com:o/r.git", Branch: "main", Runtime: RuntimeDockerfile, HostPort: 1, ContainerPort: 1, Files: map[string]string{".truedeploy/../../x": "x"}},
	} {
		if bad.Validate() == nil {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
}

func gitCommit(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "more"}} {
		c := exec.Command("git", append([]string{"-C", repo}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestLogsSince(t *testing.T) {
	c := Spec{Slug: "shop", Runtime: RuntimeCompose}
	if s := LogsSinceScript(c, 500, "1h", true); !strings.Contains(s, "logs --no-color --tail 500 --timestamps --since 1h -f 2>&1") {
		t.Errorf("compose:\n%s", s)
	}
	d := Spec{Slug: "api", Runtime: RuntimeDockerfile}
	if s := LogsSinceScript(d, 50000, "168h", false); !strings.Contains(s, "docker logs --tail 50000 --timestamps --since 168h 'td-api'") {
		t.Errorf("dockerfile:\n%s", s)
	}
	if s := LogsSinceScript(d, 10, "1h; rm -rf /", false); strings.Contains(s, "rm -rf") || strings.Contains(s, "--since") {
		t.Errorf("unknown ranges must be ignored:\n%s", s)
	}
	if LogsScript(d, 200, true) != LogsSinceScript(d, 200, "", true) {
		t.Error("LogsScript should be LogsSinceScript without a range")
	}
}
