package jobs

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

const fakeDocker = `#!/bin/sh
echo "$*" >> 'DIR/docker.log'
case "$*" in
  info*) exit 0 ;;
  *"exec -T web sh -c"*) shift $(($# - 1)); echo "ran: $1"; case "$1" in *fail*) exit 3 ;; esac ;;
  *"exec td-api sh -c"*) shift $(($# - 1)); echo "ran in container: $1" ;;
esac
`

const fakeCrontab = `#!/bin/sh
f='DIR/crontab.txt'
if [ "$1" = "-l" ]; then [ -f "$f" ] && cat "$f" || exit 1; else cat > "$f"; fi
`

func setup(t *testing.T) (run func(string) (string, error), dir, home string) {
	t.Helper()
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		bash = p
	}
	if exec.Command(bash, "-c", "command -v base64 && command -v tail").Run() != nil {
		t.Skip("no bash")
	}
	dir = t.TempDir()
	bin := filepath.Join(dir, "bin")
	home = filepath.Join(dir, "home")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(filepath.Join(home, "apps", "shop"), 0o755)
	os.MkdirAll(filepath.Join(home, "apps", "api"), 0o755)
	os.WriteFile(filepath.Join(home, "apps", "shop", "compose.yaml"), []byte("services: {}\n"), 0o644)
	os.WriteFile(filepath.Join(bin, "docker"), []byte(strings.ReplaceAll(fakeDocker, "DIR", bashPath(dir))), 0o755)
	os.WriteFile(filepath.Join(bin, "crontab"), []byte(strings.ReplaceAll(fakeCrontab, "DIR", bashPath(dir))), 0o755)
	os.WriteFile(filepath.Join(dir, "crontab.txt"), []byte("0 1 * * * certbot renew\n0 3 * * * $HOME/b.sh # truedeploy-backup-shop\n"), 0o644)
	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "export PATH='"+bashPath(bin)+"':\"$PATH\"\nset -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return run, dir, home
}

func TestJobsLifecycle(t *testing.T) {
	run, dir, home := setup(t)
	target := Target{Slug: "shop"}
	list := []Job{
		{ID: "job1aaaa", Name: "Laravel scheduler", Schedule: "* * * * *", Service: "web", Command: "php artisan schedule:run", Enabled: true},
		{ID: "job2bbbb", Name: "Nightly cleanup", Schedule: "@daily", Service: "web", Command: "fail-on-purpose", Enabled: true},
		{ID: "job3cccc", Name: "Paused", Schedule: "0 * * * *", Service: "web", Command: "echo hi", Enabled: false},
	}
	s, err := ApplyScript(target, list)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if out, err := run(s); err != nil || !strings.Contains(out, "2 scheduled job(s) active.") {
			t.Fatalf("apply: %v\n%s", err, out)
		}
	}
	cron, _ := os.ReadFile(filepath.Join(dir, "crontab.txt"))
	c := string(cron)
	if strings.Count(c, "truedeploy-job-shop-") != 2 || !strings.Contains(c, "certbot") || !strings.Contains(c, "truedeploy-backup-shop") ||
		!strings.Contains(c, "* * * * * "+bashPath(home)+"/truedeploy/jobs/shop/job1aaaa.sh") || !strings.Contains(c, "@daily ") {
		t.Errorf("crontab:\n%s", c)
	}

	// Run the jobs as cron would.
	jd := filepath.Join(home, "truedeploy", "jobs", "shop")
	if out, err := run(`bash "$HOME/truedeploy/jobs/shop/job1aaaa.sh"`); err != nil {
		t.Fatalf("job1: %v\n%s", err, out)
	}
	if out, err := run(`bash "$HOME/truedeploy/jobs/shop/job2bbbb.sh"`); err == nil {
		t.Errorf("job2 should exit non-zero: %s", out)
	}
	log1, _ := os.ReadFile(filepath.Join(jd, "job1aaaa.log"))
	if !strings.Contains(string(log1), "ran: php artisan schedule:run") || !strings.Contains(string(log1), "==> exit 0") {
		t.Errorf("job1 log:\n%s", log1)
	}
	dl, _ := os.ReadFile(filepath.Join(dir, "docker.log"))
	if !strings.Contains(string(dl), "compose -p shop -f compose.yaml exec -T web sh -c php artisan schedule:run") {
		t.Errorf("docker calls:\n%s", dl)
	}

	st, _ := run(StatusScript("shop", []string{"job1aaaa", "job2bbbb", "job3cccc"}))
	got := ParseStatus(strings.Split(st, "\n"))
	if got["job1aaaa"].Start == 0 || got["job1aaaa"].Exit != 0 || got["job2bbbb"].Exit != 3 || got["job3cccc"].Start != 0 {
		t.Errorf("status: %+v\n%s", got, st)
	}

	// Run now streams the output too.
	rs, _ := RunScript("shop", "job1aaaa")
	if out, err := run(rs); err != nil || !strings.Contains(out, "ran: php artisan schedule:run") {
		t.Errorf("run now: %v\n%s", err, out)
	}
	ls, _ := LogScript("shop", "job1aaaa", 50)
	if out, _ := run(ls); strings.Count(out, "==> exit 0") != 2 {
		t.Errorf("log after two runs:\n%s", out)
	}

	// Removing jobs drops their cron lines and scripts, nothing else.
	s, _ = ApplyScript(target, list[:1])
	if out, err := run(s); err != nil {
		t.Fatal(out)
	}
	cron, _ = os.ReadFile(filepath.Join(dir, "crontab.txt"))
	if strings.Count(string(cron), "truedeploy-job-shop-") != 1 || !strings.Contains(string(cron), "certbot") {
		t.Errorf("after removing:\n%s", cron)
	}
	if _, err := os.Stat(filepath.Join(jd, "job2bbbb.sh")); err == nil {
		t.Error("removed job's script should be deleted")
	}
	s, _ = ApplyScript(target, nil)
	if out, err := run(s); err != nil || !strings.Contains(out, "0 scheduled job(s)") {
		t.Fatal(out)
	}
	cron, _ = os.ReadFile(filepath.Join(dir, "crontab.txt"))
	if strings.Contains(string(cron), "truedeploy-job-") {
		t.Errorf("all jobs removed:\n%s", cron)
	}
}

func TestDockerfileProjectsRunInTheirContainer(t *testing.T) {
	run, dir, _ := setup(t)
	j := Job{ID: "abcd1234", Name: "Send digest", Schedule: "0 8 * * 1", Command: "node dist/digest.js", Enabled: true}
	s, err := ApplyScript(Target{Slug: "api", Dockerfile: true}, []Job{j})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(s); err != nil {
		t.Fatal(out)
	}
	if out, err := run(`bash "$HOME/truedeploy/jobs/api/abcd1234.sh" --here`); err != nil || !strings.Contains(out, "ran in container: node dist/digest.js") {
		t.Errorf("%v\n%s", err, out)
	}
	if dl, _ := os.ReadFile(filepath.Join(dir, "docker.log")); !strings.Contains(string(dl), "exec td-api sh -c node dist/digest.js") {
		t.Errorf("docker calls:\n%s", dl)
	}
}

func TestValidation(t *testing.T) {
	for _, ok := range []string{"* * * * *", "*/5 * * * *", "15 3 * * *", "0 9-17 * * 1-5", "0 0 1,15 * *", "@hourly", "@daily", " 0 0 * * 0 "} {
		if err := ValidSchedule(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "*/0 * * * *", "@reboot", "* * * * * rm -rf /", "a b c d e", "*; rm * * * *"} {
		if ValidSchedule(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	base := Job{ID: "abcd1234", Name: "x", Schedule: "@daily", Service: "web", Command: "echo"}
	for _, bad := range []Job{
		{ID: "../x", Name: "x", Schedule: "@daily", Service: "web", Command: "echo"},
		{ID: base.ID, Name: " ", Schedule: "@daily", Service: "web", Command: "echo"},
		{ID: base.ID, Name: "x", Schedule: "@daily", Service: "web", Command: "a\nb"},
		{ID: base.ID, Name: "x", Schedule: "@daily", Service: "web; rm", Command: "echo"},
		{ID: base.ID, Name: "x", Schedule: "@daily", Service: "web", Command: "echo", TimeoutMinutes: -1},
	} {
		if bad.Validate(false) == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if (Job{ID: base.ID, Name: "x", Schedule: "@daily", Command: "echo"}).Validate(true) != nil {
		t.Error("Dockerfile projects need no service")
	}
}
