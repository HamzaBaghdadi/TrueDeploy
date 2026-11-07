package offsite

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

// fakeDocker plays rclone against a folder that stands in for the bucket.
const fakeDocker = `#!/bin/sh
B='DIR/bucket'
case "$1" in
  info|pull) exit 0 ;;
  run)
    while [ "$1" != "rclone/rclone:1" ]; do
      [ "$1" = "--env-file" ] && { [ -f "$2" ] || exit 9; }
      shift
    done
    shift; cmd=$1; p=${2#tdoff:}
    [ -f 'DIR/broken' ] && { echo "rclone: connection refused" >&2; exit 1; }
    case $cmd in
      rcat) mkdir -p "$B/$(dirname "$p")"; cat > "$B/$p" ;;
      cat) cat "$B/$p" ;;
      lsf) ls -1 "$B/$p" 2>/dev/null | while read -r f; do case $f in $5) echo "$f" ;; esac; done ;;
      deletefile) rm "$B/$p" ;;
    esac ;;
esac
`

func setup(t *testing.T) (run func(string) (string, error), home, dir string) {
	t.Helper()
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
		bash = p
	}
	if exec.Command(bash, "-c", "true").Run() != nil {
		t.Skip("no bash")
	}
	dir = t.TempDir()
	home = filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(home, 0o755)
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "docker"), []byte(strings.ReplaceAll(fakeDocker, "DIR", bashPath(dir))), 0o755)
	run = func(script string) (string, error) {
		cmd := exec.Command(bash, "-c", "set -euo pipefail\nfail() { echo \"$*\" >&2; exit 1; }\n"+script)
		cmd.Env = append(os.Environ(), "HOME="+bashPath(home), "PATH="+bashPath(bin)+":/usr/bin:/mingw64/bin:/bin")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	return run, home, dir
}

var target = Target{Provider: "r2", Endpoint: "https://acc.r2.cloudflarestorage.com", Region: "auto", Bucket: "backups", AccessKey: "AK", SecretKey: "s3cr/t+'x"}.Clean()

func TestUploadPruneAndFetch(t *testing.T) {
	run, home, dir := setup(t)
	bucket := filepath.Join(dir, "bucket", "backups", "truedeploy", "s1", "databases")

	// Not set up yet: backups carry on without an offsite copy.
	local := filepath.Join(home, "truedeploy", "backups")
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(local, "shop-20261106-010000.sql.gz"), []byte("dump-1"), 0o600)
	upload := `F="$HOME/truedeploy/backups/shop-20261106-010000.sql.gz"` + "\n" + dockerEnv + UploadSnippet(DatabasesSub)
	if out, err := run(upload); err != nil || !strings.Contains(out, "::td::offsite=notset") {
		t.Fatalf("%v\n%s", err, out)
	}

	install, err := target.InstallScript("s1")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(install); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	env, _ := os.ReadFile(filepath.Join(home, "truedeploy", "offsite.env"))
	for _, want := range []string{"RCLONE_CONFIG_TDOFF_SECRET_ACCESS_KEY=s3cr/t+'x\n", "RCLONE_CONFIG_TDOFF_ENDPOINT=https://acc.r2.cloudflarestorage.com\n", "PROVIDER=Other"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("env file missing %q:\n%s", want, env)
		}
	}
	if d, _ := os.ReadFile(filepath.Join(home, "truedeploy", "offsite.dest")); strings.TrimSpace(string(d)) != "backups/truedeploy/s1" {
		t.Errorf("dest = %q", d)
	}

	out, err := run(upload)
	if err != nil || !strings.Contains(out, "::td::offsite=ok") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(bucket, "shop-20261106-010000.sql.gz")); string(b) != "dump-1" {
		t.Fatalf("uploaded = %q", b)
	}

	// Retention: keep the newest 2 of this database's scheduled backups only.
	for _, n := range []string{"shop-20261104-010000.sql.gz", "shop-20261105-010000.sql.gz", "shop-prerestore-20261101-000000.sql.gz", "shop-api-20261101-000000.sql.gz"} {
		os.WriteFile(filepath.Join(bucket, n), []byte("x"), 0o644)
	}
	if out, err := run(PruneSnippet(DatabasesSub, "shop-2*", 2)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var left []string
	entries, _ := os.ReadDir(bucket)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if got := strings.Join(left, " "); got != "shop-20261105-010000.sql.gz shop-20261106-010000.sql.gz shop-api-20261101-000000.sql.gz shop-prerestore-20261101-000000.sql.gz" {
		t.Errorf("after prune: %s", got)
	}

	// Fetch a copy back to the server.
	os.Remove(filepath.Join(local, "shop-20261106-010000.sql.gz"))
	fetch, err := FetchScript(DatabasesSub, "shop-20261106-010000.sql.gz", "$HOME/truedeploy/backups")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(fetch); err != nil || !strings.Contains(out, "You can restore it now") {
		t.Fatalf("%v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(local, "shop-20261106-010000.sql.gz")); string(b) != "dump-1" {
		t.Errorf("fetched = %q", b)
	}
	if _, err := os.Stat(filepath.Join(local, ".shop-20261106-010000.sql.gz.part")); err == nil {
		t.Error("the partial file should be renamed")
	}

	// A failed upload is reported, and fails a scheduled job at the end.
	os.WriteFile(filepath.Join(dir, "broken"), nil, 0o644)
	out, err = run(upload + `echo "local work continues"` + "\n" + FailIfUploadFailed)
	if err == nil || !strings.Contains(out, "::td::offsite=failed") || !strings.Contains(out, "local work continues") || !strings.Contains(out, "offsite copy failed") {
		t.Errorf("failed upload: %v\n%s", err, out)
	}

	if out, err := run(RemoveScript); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "truedeploy", "offsite.env")); err == nil {
		t.Error("remove should delete the credentials")
	}
}

func TestValidate(t *testing.T) {
	ok := Target{Bucket: "my-backups", AccessKey: "AK", SecretKey: "SK"}.Clean()
	if err := ok.Validate(); err != nil || ok.Prefix != "truedeploy" || ok.Region != "us-east-1" {
		t.Fatalf("%v %+v", err, ok)
	}
	bad := map[string]Target{
		"endpoint": {Endpoint: "s3.example.com"},
		"bucket":   {Bucket: "a"},
		"folder":   {Prefix: "x/../y"},
		"region":   {Region: "US East"},
		"keys":     {AccessKey: "has space"},
	}
	for name, change := range bad {
		tg := ok
		if change.Endpoint != "" {
			tg.Endpoint = change.Endpoint
		}
		if change.Bucket != "" {
			tg.Bucket = change.Bucket
		}
		if change.Prefix != "" {
			tg.Prefix = change.Prefix
		}
		if change.Region != "" {
			tg.Region = change.Region
		}
		if change.AccessKey != "" {
			tg.AccessKey = change.AccessKey
		}
		if err := tg.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := FetchScript("projects/../x", "a.gz", "$HOME"); err == nil {
		t.Error("bad folder accepted")
	}
	if _, err := FetchScript(DatabasesSub, "../../.ssh/id", "$HOME"); err == nil {
		t.Error("bad name accepted")
	}
}
