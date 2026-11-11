package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func release(base string) Release {
	names := []string{
		"TrueDeploy-0.3.0-windows-amd64-setup.exe", "TrueDeploy-0.3.0-windows-amd64-portable.exe",
		"TrueDeploy-0.3.0-macos-universal.zip", "TrueDeploy-0.3.0-macos-universal.dmg",
		"TrueDeploy-0.3.0-linux-amd64.AppImage", "TrueDeploy-0.3.0-linux-amd64.deb", "SHA256SUMS",
	}
	r := Release{Version: "0.3.0"}
	for _, n := range names {
		r.Assets = append(r.Assets, Asset{Name: n, URL: base + "/download/" + n})
	}
	return r
}

func TestPlanFor(t *testing.T) {
	rel := release("https://github.com/o/r/releases")
	lad := `C:\Users\me\AppData\Local`
	cases := []struct {
		name   string
		target Target
		method Method
		suffix string
	}{
		{"installed on Windows", Target{GOOS: "windows", Exe: lad + `\Programs\TrueDeploy\truedeploy.exe`, LocalAppData: lad}, WindowsSetup, "setup.exe"},
		{"portable on Windows", Target{GOOS: "windows", Exe: `D:\tools\truedeploy.exe`, LocalAppData: lad}, WindowsPortable, "portable.exe"},
		{"macOS bundle", Target{GOOS: "darwin", Exe: "/Applications/TrueDeploy.app/Contents/MacOS/truedeploy"}, MacBundle, ".zip"},
		{"AppImage", Target{GOOS: "linux", Exe: "/tmp/.mount_x/truedeploy", AppImage: "/home/me/Apps/TrueDeploy.AppImage"}, LinuxAppImage, ".AppImage"},
	}
	for _, c := range cases {
		p, why := PlanFor(rel, c.target)
		if p.Method != c.method || !strings.HasSuffix(p.Asset.Name, c.suffix) || why != "" {
			t.Errorf("%s: %+v %q", c.name, p, why)
		}
	}
	if p, _ := PlanFor(rel, cases[2].target); p.Path != "/Applications/TrueDeploy.app" {
		t.Errorf("bundle path %q", p.Path)
	}
	if p, why := PlanFor(rel, Target{GOOS: "linux", Exe: "/usr/local/bin/truedeploy"}); p.Method != "" || !strings.Contains(why, ".deb") {
		t.Errorf("deb install: %+v %q", p, why)
	}
	if p, why := PlanFor(rel, Target{GOOS: "darwin", Exe: "/Users/me/go/bin/truedeploy"}); p.Method != "" || why == "" {
		t.Errorf("bare binary on macOS: %+v %q", p, why)
	}
	if p, why := PlanFor(Release{}, cases[0].target); p.Method != "" || why == "" {
		t.Errorf("no assets: %+v %q", p, why)
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("new TrueDeploy build")
	sum := sha256.Sum256(payload)
	sums := fmt.Sprintf("%s  TrueDeploy-0.3.0-linux-amd64.AppImage\n%s  other.zip\n", hex.EncodeToString(sum[:]), strings.Repeat("0", 64))
	served := payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			fmt.Fprint(w, sums)
		case strings.HasSuffix(r.URL.Path, ".AppImage"):
			w.Write(served)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := API
	API = srv.URL // lets the test server stand in for GitHub
	defer func() { API = old }()

	rel := release(srv.URL)
	p, _ := PlanFor(rel, Target{GOOS: "linux", AppImage: "/x/TrueDeploy.AppImage"})
	dir := t.TempDir()
	var last int64
	got, err := Download(context.Background(), srv.Client(), rel, p, dir, func(done, _ int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got); string(b) != string(payload) || last != int64(len(payload)) {
		t.Errorf("downloaded %q, progress %d", b, last)
	}

	// A tampered file is rejected and removed.
	os.Remove(got)
	served = []byte("tampered")
	if _, err := Download(context.Background(), srv.Client(), rel, p, dir, nil); err != ErrChecksum {
		t.Errorf("tampered download: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("nothing should be left behind: %v", entries)
	}

	// No checksums file: no update.
	rel.Assets = rel.Assets[:len(rel.Assets)-1]
	if _, err := Download(context.Background(), srv.Client(), rel, p, dir, nil); err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
		t.Errorf("missing checksums: %v", err)
	}

	// Only GitHub URLs.
	evil := release("https://evil.example.com")
	pe, _ := PlanFor(evil, Target{GOOS: "linux", AppImage: "/x/a.AppImage"})
	if _, err := Download(context.Background(), srv.Client(), evil, pe, dir, nil); err == nil {
		t.Error("downloads outside GitHub must be refused")
	}
}

// A process id that isn't running, so the scripts don't wait.
const gonePID = 999999

func TestAppImageScriptReplacesTheFile(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if runtime.GOOS != "windows" {
		bash = "/bin/sh"
	}
	if exec.Command(bash, "-c", "true").Run() != nil {
		t.Skip("no shell")
	}
	dir := t.TempDir()
	cur := filepath.Join(dir, "TrueDeploy.AppImage")
	dl := filepath.Join(dir, "new.AppImage")
	os.WriteFile(cur, []byte("old"), 0o755)
	os.WriteFile(dl, []byte("new"), 0o644)
	p := Plan{Method: LinuxAppImage, Path: toSh(cur)}
	script := Script(p, toSh(dl), gonePID, false)
	if out, err := exec.Command(bash, "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, script)
	}
	if b, _ := os.ReadFile(cur); string(b) != "new" {
		t.Errorf("AppImage = %q", b)
	}
	if _, err := os.Stat(dl); err == nil {
		t.Error("the download should be moved into place")
	}
	if !strings.Contains(Script(p, "/x", 1, true), "nohup") {
		t.Error("relaunch missing")
	}
}

func toSh(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	p = filepath.ToSlash(p)
	return "/" + strings.ToLower(p[:1]) + p[2:]
}

func TestWindowsPortableScript(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell test")
	}
	dir := t.TempDir()
	cur := filepath.Join(dir, "it's truedeploy.exe") // a quote in the path must survive
	dl := filepath.Join(dir, "new.exe")
	os.WriteFile(cur, []byte("old"), 0o755)
	os.WriteFile(dl, []byte("new"), 0o644)
	script := Script(Plan{Method: WindowsPortable, Path: cur}, dl, gonePID, false)
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, script)
	}
	if b, _ := os.ReadFile(cur); string(b) != "new" {
		t.Errorf("exe = %q\n%s", b, out)
	}
	setup := Script(Plan{Method: WindowsSetup, Path: `C:\x\truedeploy.exe`}, `C:\t\setup.exe`, 42, true)
	for _, want := range []string{"Wait-Process -Id 42", "-ArgumentList '/S' -Wait", `Start-Process -FilePath 'C:\x\truedeploy.exe'`} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup script missing %q:\n%s", want, setup)
		}
	}
}

func TestMacScript(t *testing.T) {
	s := Script(Plan{Method: MacBundle, Path: "/Applications/TrueDeploy.app"}, "/tmp/u/TrueDeploy.zip", 7, true)
	for _, want := range []string{"kill -0 7", "ditto -x -k '/tmp/u/TrueDeploy.zip'", "APP='/Applications/TrueDeploy.app'", `mv "$APP.td-old" "$APP"`, `open "$APP"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}
