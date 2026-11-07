package volbackup

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"truedeploy/internal/databases"
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
	if exec.Command(bash, "-c", "command -v tar && command -v gzip && command -v stat").Run() != nil {
		t.Skip("no bash with tar/gzip/stat")
	}
	return bash
}

const fakeDocker = `#!/bin/sh
echo "$*" >> 'DIR/docker.log'
case "$*" in
  info*) exit 0 ;;
  "volume ls"*) printf 'shop_data\nshop_db\nshop-api_data\nother_x\n' ;;
  "ps -q"*) [ -f 'DIR/stopped' ] && exit 0; printf 'c1\nc2\n' ;;
  "stop "*) touch 'DIR/stopped' ;;
  "start "*) rm -f 'DIR/stopped' ;;
  "pull "*) exit 0 ;;
  *"tar czf -"*) tar czf - -C 'DIR/vols' . ;;
  *"run --rm -i"*) cat > 'DIR/restored.tgz' ;;
esac
`

const fakeCrontab = `#!/bin/sh
f='DIR/crontab.txt'
if [ "$1" = "-l" ]; then [ -f "$f" ] && cat "$f" || exit 1; else cat > "$f"; fi
`

// fakeServer: docker knows two volumes of "shop" (plus look-alikes that must
// be ignored), "runs" tar over a fixture tree, and records restores.
func fakeServer(t *testing.T) (run func(string) (string, error), home, dir string) {
	t.Helper()
	bash := findBash(t)
	dir = t.TempDir()
	home = filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	vols := filepath.Join(dir, "vols")
	for _, d := range []string{home, bin, filepath.Join(vols, "shop_data"), filepath.Join(vols, "shop_db")} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(vols, "shop_data", "upload.jpg"), []byte("jpg"), 0o644)
	os.WriteFile(filepath.Join(vols, "shop_db", "pg.dat"), []byte("rows"), 0o644)
	b := bashPath(dir)
	os.WriteFile(filepath.Join(bin, "docker"), []byte(strings.ReplaceAll(fakeDocker, "DIR", b)), 0o755)
	os.WriteFile(filepath.Join(bin, "crontab"), []byte(strings.ReplaceAll(fakeCrontab, "DIR", b)), 0o755)
	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home), "PATH="+bashPath(bin)+":/usr/bin:/mingw64/bin:/bin")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return run, home, dir
}

func dockerLog(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "docker.log"))
	return string(b)
}

var markerRe = regexp.MustCompile(`::td::backup=(\S+)`)

func backupNow(t *testing.T, run func(string) (string, error)) (string, string) {
	t.Helper()
	s, err := BackupScript("shop")
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(s)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := markerRe.FindStringSubmatch(out)
	if m == nil || !ValidName(m[1]) {
		t.Fatalf("no backup marker:\n%s", out)
	}
	return m[1], out
}

func TestBackupStopsCopiesAndRestarts(t *testing.T) {
	run, home, dir := fakeServer(t)
	name, out := backupNow(t, run)

	f := filepath.Join(home, "truedeploy", "backups", "projects", "shop", name)
	list, err := exec.Command("tar", "tzf", bashPath(f)).CombinedOutput()
	if err != nil {
		list, _ = exec.Command("tar", "tzf", f).CombinedOutput()
	}
	for _, want := range []string{"./shop_data/upload.jpg", "./shop_db/pg.dat"} {
		if !strings.Contains(string(list), want) {
			t.Errorf("archive misses %s:\n%s", want, list)
		}
	}
	log := dockerLog(dir)
	run1 := regexp.MustCompile(`run --rm .*`).FindString(log)
	if !strings.Contains(run1, "-v shop_data:/v/shop_data:ro -v shop_db:/v/shop_db:ro") || strings.Contains(run1, "shop-api") || strings.Contains(run1, "other_x") {
		t.Errorf("wrong volumes mounted: %s", run1)
	}
	// Pull before stopping (short downtime), stop before copying, start after.
	i, j, k, l := strings.Index(log, "pull"), strings.Index(log, "stop c1 c2"), strings.Index(log, "run --rm"), strings.Index(log, "start c1 c2")
	if !(i >= 0 && i < j && j < k && k < l) {
		t.Errorf("order pull/stop/run/start wrong:\n%s", log)
	}
	if !strings.Contains(out, "with 2 volume(s): shop_data shop_db") {
		t.Errorf("summary:\n%s", out)
	}

	ls, _ := ListScript("shop")
	if out, _ := run(ls); !strings.HasPrefix(out, name+"\t") {
		t.Errorf("list:\n%s", out)
	}
}

const failingDocker = `#!/bin/sh
echo "$*" >> 'DIR/docker.log'
case "$*" in
  info*|pull*|stop*|start*) exit 0 ;;
  "volume ls"*) echo shop_data ;;
  "ps -q"*) echo c1 ;;
  run*) echo "disk full" >&2; exit 1 ;;
esac
`

func TestContainersRestartWhenBackupFails(t *testing.T) {
	run, home, dir := fakeServer(t)
	os.WriteFile(filepath.Join(dir, "bin", "docker"), []byte(strings.ReplaceAll(failingDocker, "DIR", bashPath(dir))), 0o755)
	s, _ := BackupScript("shop")
	out, err := run(s)
	if err == nil || !strings.Contains(out, "Backup failed") {
		t.Fatalf("expected failure:\n%s", out)
	}
	if !strings.Contains(dockerLog(dir), "start c1") {
		t.Errorf("containers not restarted after failure:\n%s", dockerLog(dir))
	}
	files, _ := filepath.Glob(filepath.Join(home, "truedeploy", "backups", "projects", "shop", "*.tar.gz"))
	if len(files) != 0 {
		t.Errorf("partial backup left behind: %v", files)
	}
}

func TestRestoreTakesSafetyBackupFirst(t *testing.T) {
	run, home, dir := fakeServer(t)
	name, _ := backupNow(t, run)

	r, err := RestoreScript("shop", name)
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(r)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "previous data is saved as prerestore-") {
		t.Errorf("restore output:\n%s", out)
	}
	orig, _ := os.ReadFile(filepath.Join(home, "truedeploy", "backups", "projects", "shop", name))
	got, _ := os.ReadFile(filepath.Join(dir, "restored.tgz"))
	if len(orig) == 0 || string(orig) != string(got) {
		t.Error("restore didn't stream the chosen backup")
	}
	log := dockerLog(dir)
	restoreRun := regexp.MustCompile(`run --rm -i .*`).FindString(log)
	if !strings.Contains(restoreRun, "-v shop_data:/v/shop_data -v shop_db:/v/shop_db alpine:3") {
		t.Errorf("restore mounts: %s", restoreRun)
	}
	if strings.LastIndex(log, "start c1 c2") < strings.Index(log, "run --rm -i") {
		t.Errorf("containers must restart after the restore:\n%s", log)
	}

	// A backup with foreign volumes is refused before anything is touched.
	evil := filepath.Join(dir, "evil")
	os.MkdirAll(filepath.Join(evil, "other_db"), 0o755)
	os.WriteFile(filepath.Join(evil, "other_db", "x"), []byte("x"), 0o644)
	bdir := bashPath(filepath.Join(home, "truedeploy", "backups", "projects", "shop"))
	if out, err := run(`tar czf "` + bdir + `/20990101-000000.tar.gz" -C "` + bashPath(evil) + `" .`); err != nil {
		t.Skipf("tar: %v %s", err, out)
	}
	os.Remove(filepath.Join(dir, "docker.log"))
	r, _ = RestoreScript("shop", "20990101-000000.tar.gz")
	if out, err := run(r); err == nil || !strings.Contains(out, "isn't a volume of this project") {
		t.Errorf("foreign volume accepted:\n%s", out)
	}
	if strings.Contains(dockerLog(dir), "stop") {
		t.Error("nothing should be stopped for a refused restore")
	}
}

func TestNames(t *testing.T) {
	for _, n := range []string{"20261105-030000.tar.gz", "prerestore-20261105-030000.tar.gz"} {
		if !ValidName(n) {
			t.Errorf("%s should be valid", n)
		}
	}
	for _, n := range []string{"../x.tar.gz", "20261105-030000.tar.gz; rm -rf ~", "x.tar.gz", ".backup.sh"} {
		if ValidName(n) {
			t.Errorf("%s should be invalid", n)
		}
		if _, err := RestoreScript("shop", n); err == nil {
			t.Errorf("restore accepted %s", n)
		}
		if _, err := DeleteScript("shop", n); err == nil {
			t.Errorf("delete accepted %s", n)
		}
	}
	if _, err := BackupScript("Bad Slug"); err == nil {
		t.Error("bad slug accepted")
	}
}

func TestScheduleInstallsWorkingJob(t *testing.T) {
	run, home, dir := fakeServer(t)
	os.WriteFile(filepath.Join(dir, "crontab.txt"), []byte("0 1 * * * certbot renew\n"), 0o644)
	sc := databases.Schedule{Enabled: true, Frequency: "daily", Hour: 4, Keep: 2}
	for i := 0; i < 2; i++ {
		s, err := ScheduleScript("shop", sc)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := run(s); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	cron, _ := os.ReadFile(filepath.Join(dir, "crontab.txt"))
	if strings.Count(string(cron), "truedeploy-volbackup-shop") != 1 || !strings.Contains(string(cron), "certbot") || !strings.Contains(string(cron), "15 4 * * *") {
		t.Errorf("crontab:\n%s", cron)
	}

	// The installed job works on its own and prunes beyond Keep, never
	// touching prerestore copies.
	bdir := filepath.Join(home, "truedeploy", "backups", "projects", "shop")
	for _, old := range []string{"20200101-000000.tar.gz", "20200102-000000.tar.gz", "prerestore-20200101-000000.tar.gz"} {
		os.WriteFile(filepath.Join(bdir, old), []byte("x"), 0o600)
	}
	// Make the old ones clearly older for ls -t.
	if out, err := run(`touch -d '2020-01-01' "` + bashPath(bdir) + `"/2020*.tar.gz "` + bashPath(bdir) + `"/prerestore-*.tar.gz`); err != nil {
		t.Fatal(out)
	}
	if out, err := run(`bash "` + bashPath(filepath.Join(bdir, ".backup.sh")) + `"`); err != nil {
		t.Fatalf("job: %v\n%s", err, out)
	}
	left, _ := filepath.Glob(filepath.Join(bdir, "*.tar.gz"))
	var names []string
	for _, f := range left {
		names = append(names, filepath.Base(f))
	}
	sort.Strings(names)
	// Keep=2: the new backup and 20200102 stay, 20200101 goes, prerestore stays.
	if len(names) != 3 || names[0] != "20200102-000000.tar.gz" || names[2] != "prerestore-20200101-000000.tar.gz" {
		t.Errorf("retention kept %v", names)
	}

	u, _ := UnscheduleScript("shop")
	if out, err := run(u); err != nil {
		t.Fatal(out)
	}
	cron, _ = os.ReadFile(filepath.Join(dir, "crontab.txt"))
	if strings.Contains(string(cron), "volbackup") || !strings.Contains(string(cron), "certbot") {
		t.Errorf("after unschedule:\n%s", cron)
	}
}

func TestOffsiteScheduleForVolumes(t *testing.T) {
	s, err := ScheduleScript("shop", databases.Schedule{Frequency: "daily", Keep: 5, Offsite: true})
	if err != nil {
		t.Fatal(err)
	}
	// The job script is base64 inside the install script; check it decoded.
	job := decodeJob(t, s)
	for _, want := range []string{"/projects/shop/$(basename \"$F\")", "--include '2*'", "tail -n +6"} {
		if !strings.Contains(job, want) {
			t.Errorf("missing %q in:\n%s", want, job)
		}
	}
}

func decodeJob(t *testing.T, install string) string {
	t.Helper()
	m := regexp.MustCompile(`echo (\S+) \| base64 -d`).FindStringSubmatch(install)
	if m == nil {
		t.Fatal("no job script in the install script")
	}
	b, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
