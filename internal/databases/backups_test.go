package databases

import (
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func bashPath(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 1 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

// fakeServer gives a HOME with fake `docker` (pg_dump prints a unique dump,
// psql stores what it receives) and fake `crontab` backed by a file.
func fakeServer(t *testing.T) (run func(string) (string, error), home, dir string) {
	t.Helper()
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	if exec.Command(bash, "-c", "command -v gzip && command -v stat").Run() != nil {
		t.Skip("no bash with gzip/stat")
	}
	dir = t.TempDir()
	home = filepath.Join(dir, "home")
	// The fakes live in $HOME/bin: scripts search it before /usr/bin, so they
	// win over a real docker or crontab (as on CI runners).
	bin := filepath.Join(dir, "home", "bin")
	os.MkdirAll(home, 0o755)
	os.MkdirAll(bin, 0o755)
	b := bashPath(dir)
	os.WriteFile(filepath.Join(bin, "docker"), []byte(`#!/bin/sh
echo "$*" >> '`+b+`/docker.log'
case "$*" in
  info*) exit 0 ;;
  *pg_dump*) echo "-- dump made at $(date +%s%N)"; echo "CREATE TABLE items (id int);" ;;
  *"psql"*" -c "*) exit 0 ;;
  *psql*) cat > '`+b+`/restored.sql' ;;
esac
`), 0o755)
	os.WriteFile(filepath.Join(bin, "crontab"), []byte(`#!/bin/sh
f='`+b+`/crontab.txt'
if [ "$1" = "-l" ]; then [ -f "$f" ] && cat "$f" || exit 1; else cat > "$f"; fi
`), 0o755)
	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home), "PATH="+bashPath(bin)+":/usr/bin:/mingw64/bin:/bin")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return run, home, dir
}

func pg() Spec {
	return Spec{Slug: "main", Engine: "postgres", Version: "17", User: "app", Database: "main", Password: GeneratePassword()}
}

func TestScheduleIsIdempotentAndKeepsOtherCronJobs(t *testing.T) {
	run, _, dir := fakeServer(t)
	cron := filepath.Join(dir, "crontab.txt")
	os.WriteFile(cron, []byte("0 1 * * * /usr/bin/certbot renew\n"), 0o644)

	sc := Schedule{Enabled: true, Frequency: "daily", Hour: 3, Keep: 7}
	for i := 0; i < 2; i++ {
		s, err := ScheduleScript(pg(), sc)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := run(s); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	b, _ := os.ReadFile(cron)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[0] != "0 1 * * * /usr/bin/certbot renew" {
		t.Fatalf("crontab = %q", b)
	}
	if !strings.HasPrefix(lines[1], "15 3 * * * ") || !strings.HasSuffix(lines[1], "# truedeploy-backup-main") {
		t.Fatalf("backup line = %q", lines[1])
	}

	status, _ := run(ScheduleStatusScript("main"))
	if !strings.Contains(status, "cron=15 3 * * *") {
		t.Fatalf("status = %q", status)
	}

	if out, err := run(UnscheduleScript("main")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, _ = os.ReadFile(cron)
	if strings.TrimSpace(string(b)) != "0 1 * * * /usr/bin/certbot renew" {
		t.Fatalf("unschedule must leave other jobs alone: %q", b)
	}
}

func TestScheduledBackupsPruneButKeepSafetyBackups(t *testing.T) {
	run, home, _ := fakeServer(t)
	s := pg()
	sched, _ := ScheduleScript(s, Schedule{Enabled: true, Frequency: "6h", Keep: 2})
	if out, err := run(sched); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	backups := filepath.Join(home, "truedeploy", "backups")
	os.WriteFile(filepath.Join(backups, "main-prerestore-20200101-000000.sql.gz"), []byte("x"), 0o600)

	script := bashPath(filepath.Join(backups, ".main.backup.sh"))
	for i := 0; i < 4; i++ {
		if out, err := run(script); err != nil {
			t.Fatalf("run %d: %v\n%s", i, err, out)
		}
		time.Sleep(1100 * time.Millisecond) // file names have one-second resolution
	}
	var regular, safety int
	entries, _ := os.ReadDir(backups)
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Name(), "main-prerestore-"):
			safety++
		case strings.HasPrefix(e.Name(), "main-2") && strings.HasSuffix(e.Name(), ".sql.gz"):
			regular++
		}
	}
	if regular != 2 || safety != 1 {
		t.Fatalf("want 2 scheduled backups kept and the safety backup untouched, got %d and %d", regular, safety)
	}
}

func TestRestoreTakesSafetyBackupFirst(t *testing.T) {
	run, home, dir := fakeServer(t)
	s := pg()
	b, _ := BackupScript(s)
	out, err := run(b)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var name string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "::td::backup="); ok {
			name = filepath.Base(v)
		}
	}
	original := gunzipFile(t, filepath.Join(home, "truedeploy", "backups", name))

	r, err := RestoreScript(s, name)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	out, err = run(r)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	restored, _ := os.ReadFile(filepath.Join(dir, "restored.sql"))
	if len(original) == 0 || string(restored) != string(original) {
		t.Fatalf("restored data differs:\n%q\nvs\n%q", restored, original)
	}
	if !strings.Contains(out, "main-prerestore-") {
		t.Fatalf("restore must report the safety backup:\n%s", out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "docker.log"))
	if !strings.Contains(string(log), "DROP SCHEMA public CASCADE") {
		t.Fatal("postgres restore should start from an empty schema")
	}
}

func TestBackupNamesAreValidated(t *testing.T) {
	ok := []string{"main-20261105-091500.sql.gz", "main-prerestore-20261105-091500.archive.gz"}
	bad := []string{"../../etc/passwd", "other-20261105.sql.gz", "main-x.sql.gz; rm -rf ~", "main-20261105.sql", "main-$(id).sql.gz"}
	for _, n := range ok {
		if !ValidBackupName("main", n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range bad {
		if ValidBackupName("main", n) {
			t.Errorf("%q must be rejected", n)
		}
		if _, err := RestoreScript(pg(), n); err == nil {
			t.Errorf("restore of %q must be rejected", n)
		}
		if _, err := DeleteBackupScript("main", n); err == nil {
			t.Errorf("delete of %q must be rejected", n)
		}
	}
	if (Schedule{Frequency: "hourly", Keep: 3}).Validate() == nil || (Schedule{Frequency: "daily", Keep: 0}).Validate() == nil {
		t.Error("invalid schedules must be rejected")
	}
}

func TestBackupNamesDontMatchOtherDatabases(t *testing.T) {
	for _, n := range []string{"shop-api-20261105-091500.sql.gz", "shop-2nd-20261105-091500.sql.gz", "shop-prerestore-x.sql.gz"} {
		if ValidBackupName("shop", n) {
			t.Errorf("%q belongs to another database", n)
		}
	}
}

func TestOffsiteScheduleUploadsAndPrunes(t *testing.T) {
	plain, _ := scheduledScript(pg(), Schedule{Frequency: "daily", Keep: 3})
	if strings.Contains(plain, "rclone") {
		t.Error("no offsite step without Offsite")
	}
	s, err := scheduledScript(pg(), Schedule{Frequency: "daily", Keep: 3, Offsite: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"rcat \"tdoff:$(cat", "/databases/$(basename \"$F\")", "--include 'main-[0-9]", "tail -n +4", "offsite copy failed"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// The upload comes right after the backup, before local retention.
	if strings.Index(s, "rcat") > strings.Index(s, "pruned $(basename") {
		t.Error("upload should run before local pruning")
	}
}

// gunzipFile reads a gzip file (in Go, so it works on any OS).
func gunzipFile(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
