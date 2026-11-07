package importer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func bashPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 1 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

func findBash(t *testing.T) string {
	t.Helper()
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		bash = p
	}
	if exec.Command(bash, "-c", "command -v git && command -v base64").Run() != nil {
		t.Skip("no bash with git/base64")
	}
	return bash
}

const fakeDocker = `#!/bin/sh
echo "$*" >> 'DIR/docker.log'
case "$*" in
  info*) exit 0 ;;
  *"com.docker.compose.project.working_dir"*) printf 'shop\tWD\tWD/docker-compose.yml\nshop\tWD\tWD/docker-compose.yml\n' ;;
  "ps -aq --filter label=com.docker.compose.project=shop") echo abc123 ;;
  "ps -a --filter label=com.docker.compose.project=shop --format"*)
    printf 'P\tshop\tshop-web-1\trunning\t0.0.0.0:8080->80/tcp, [::]:8080->80/tcp\nP\tshop\tshop-db-1\trunning\t5432/tcp\n' ;;
  "inspect -f {{range .Mounts}}"*) printf '/srv/uploads\n' ;;
  "ps -a --format {{.Names}}"*) printf 'pg\tpostgres:16-alpine\trunning\t0.0.0.0:5432->5432/tcp\t\nworker\tme/worker:1\texited\t\t\ntd-db-main\tpostgres:17\trunning\t\t\n' ;;
  "inspect -f {{range .Config.Env}}"*) printf 'POSTGRES_USER=shop\nPOSTGRES_PASSWORD=s3cret\nPOSTGRES_DB=shopdb\nPATH=/usr/bin\n' ;;
  exec*) echo "dump" ;;
esac
`

func setup(t *testing.T) (run func(string) (string, error), dir, wd string) {
	t.Helper()
	bash := findBash(t)
	dir = t.TempDir()
	bin := filepath.Join(dir, "bin")
	home := filepath.Join(dir, "home")
	wd = filepath.Join(dir, "srv", "shop")
	for _, d := range []string{bin, home, wd} {
		os.MkdirAll(d, 0o755)
	}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", wd}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(wd, "docker-compose.yml"), []byte("services: {}\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("remote", "add", "origin", "git@github.com:me/shop.git")
	os.WriteFile(filepath.Join(wd, ".env"), []byte("# app\nAPI_KEY=\"abc def\"\nexport DEBUG=0\n"), 0o644)
	os.WriteFile(filepath.Join(wd, "local.txt"), []byte("untracked: deploys never touch it"), 0o644)
	os.WriteFile(filepath.Join(wd, "docker-compose.yml"), []byte("services: {} # edited on the server"), 0o644)

	fake := strings.ReplaceAll(strings.ReplaceAll(fakeDocker, "DIR", bashPath(dir)), "WD", bashPath(wd))
	os.WriteFile(filepath.Join(bin, "docker"), []byte(fake), 0o755)
	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home), "PATH="+bashPath(bin)+":/usr/bin:/mingw64/bin:/bin")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return run, dir, wd
}

func TestScanFindsProjectsAndDatabases(t *testing.T) {
	run, _, wd := setup(t)
	out, err := run(ScanScript)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// nginx isn't on the test machine: add a site the way the script prints it.
	lines := append(strings.Split(out, "\n"),
		"N\t/etc/nginx/sites-enabled/shop\tshop.io www.shop.io _\thttp://127.0.0.1:8080 http://upstream_name",
		"L\t80\tnginx", "S\tnginx")
	sc := Parse(lines)

	if !sc.Docker || len(sc.Compose) != 1 {
		t.Fatalf("scan: %+v", sc)
	}
	c := sc.Compose[0]
	if c.Name != "shop" || c.WorkDir != bashPath(wd) || c.RepoURL != "git@github.com:me/shop.git" || c.Branch != "main" || !c.Writable {
		t.Errorf("compose project: %+v", c)
	}
	if c.Dirty != 1 {
		t.Errorf("only the edited tracked file counts, got %d", c.Dirty)
	}
	if c.Env["API_KEY"] != "abc def" || c.Env["DEBUG"] != "0" || len(c.Env) != 2 {
		t.Errorf("env: %v", c.Env)
	}
	if len(c.Containers) != 2 || len(c.Ports) != 1 || c.Ports[0] != 8080 || len(c.BindMounts) != 1 || c.ComposeFile() != "" {
		t.Errorf("containers/ports/mounts: %+v", c)
	}
	if c.SuggestedPort != 8080 || strings.Join(c.SuggestedDomains, ",") != "shop.io,www.shop.io" {
		t.Errorf("suggestions: %d %v", c.SuggestedPort, c.SuggestedDomains)
	}
	if len(c.Problems(func(string) bool { return true })) != 0 {
		t.Errorf("problems: %v", c.Problems(func(string) bool { return true }))
	}

	if len(sc.Databases) != 1 {
		t.Fatalf("databases: %+v", sc.Databases)
	}
	db := sc.Databases[0]
	if db.Container != "pg" || db.Engine != "postgres" || db.Version != "16" || db.User != "shop" || db.Database != "shopdb" || !db.HasPassword() {
		t.Errorf("database: %+v", db)
	}
	if len(sc.Containers) != 1 || sc.Containers[0].Name != "worker" {
		t.Errorf("other containers (td-* must be skipped): %+v", sc.Containers)
	}
	if len(sc.Listeners) != 1 || sc.Listeners[0].Process != "nginx" || len(sc.Services) != 1 {
		t.Errorf("listeners/services: %+v %v", sc.Listeners, sc.Services)
	}
}

func TestAdoptLinksTheExistingFolder(t *testing.T) {
	run, dir, wd := setup(t)
	if out, err := run(AdoptScript("shop", bashPath(wd))); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// Git Bash on Windows copies instead of linking unless symlinks are
	// enabled; the script targets Linux servers, where ln -s always links.
	if out, _ := run(`[ -L "$HOME/apps/shop" ] && echo link`); !strings.Contains(out, "link") {
		t.Skip("this shell can't create symlinks")
	}
	if out, err := run(AdoptScript("shop", bashPath(wd))); err != nil || !strings.Contains(out, "Already linked") {
		t.Fatalf("second run should be a no-op: %v\n%s", err, out)
	}
	link := filepath.Join(dir, "home", "apps", "shop")
	if out, err := run(`[ -f "$HOME/apps/shop/docker-compose.yml" ] && [ -d "$HOME/apps/shop/.git" ] && echo ok`); err != nil || !strings.Contains(out, "ok") {
		t.Errorf("link %s doesn't lead to the project: %v %s", link, err, out)
	}
	// A different folder under the same name is refused.
	other := filepath.Join(dir, "srv", "other")
	os.MkdirAll(filepath.Join(other, ".git"), 0o755)
	if out, err := run(AdoptScript("shop", bashPath(other))); err == nil || !strings.Contains(out, "already exists") {
		t.Errorf("expected a conflict: %v %s", err, out)
	}
	if out, err := run(AdoptScript("x", bashPath(filepath.Join(dir, "missing")))); err == nil || !strings.Contains(out, "no longer exists") {
		t.Errorf("missing folder: %s", out)
	}
}

func TestCopyScripts(t *testing.T) {
	run, dir, _ := setup(t)
	src := Database{Container: "pg", Engine: "postgres", User: "shop", Database: "shopdb", password: "s3cret"}
	s, err := CopyScript(src, CopyTarget{Container: "td-db-shop", User: "app", Database: "shop", Password: "n3w"})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(s); err != nil || !strings.Contains(out, "Copied.") {
		t.Fatalf("%v\n%s", err, out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "docker.log"))
	if !strings.Contains(string(log), "exec -e PGPASSWORD=s3cret pg pg_dump -U shop -d shopdb --no-owner") ||
		!strings.Contains(string(log), "exec -i -e PGPASSWORD=n3w td-db-shop psql -q -U app -d shop") {
		t.Errorf("docker calls:\n%s", log)
	}
	if strings.Contains(s, "s3cret") || strings.Contains(s, "n3w") {
		t.Error("passwords must travel base64-encoded, not in the script text")
	}

	my := Database{Container: "maria", Engine: "mysql", MariaDB: true, User: "root", Database: "wp", password: "x"}
	if s, err := CopyScript(my, CopyTarget{Container: "td-db-wp", User: "app", Database: "wp", Password: "y"}); err != nil || !strings.Contains(s, "mariadb-dump || command -v mysqldump") {
		t.Errorf("mysql: %v", err)
	}
	if _, err := CopyScript(Database{Container: "m", Engine: "mysql"}, CopyTarget{}); err == nil {
		t.Error("mysql without a database name should be refused")
	}
}

func TestComposeFileAndProblems(t *testing.T) {
	c := ComposeProject{Name: "My App", WorkDir: "/srv/app", ConfigFiles: []string{"/srv/app/deploy/prod.yml", "/srv/app/override.yml"}}
	if c.ComposeFile() != "deploy/prod.yml" {
		t.Errorf("compose file: %s", c.ComposeFile())
	}
	p := c.Problems(func(n string) bool { return n == strings.ToLower(n) })
	if len(p) != 4 { // no repo, not writable, bad name, several files
		t.Errorf("problems: %v", p)
	}
}
