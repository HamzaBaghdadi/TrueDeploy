package updates

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Method is how an update replaces the running app.
type Method string

const (
	// WindowsSetup runs the per-user installer silently (installed copies).
	WindowsSetup Method = "windows-setup"
	// WindowsPortable replaces a portable .exe.
	WindowsPortable Method = "windows-portable"
	// MacBundle swaps the .app bundle for the one in the release's .zip.
	MacBundle Method = "mac-bundle"
	// LinuxAppImage replaces the AppImage file.
	LinuxAppImage Method = "linux-appimage"
)

// Target describes the running app, to pick how to update it.
type Target struct {
	GOOS     string
	Exe      string // os.Executable()
	AppImage string // $APPIMAGE when running as an AppImage
	// LocalAppData is %LOCALAPPDATA% (Windows), where the installer puts the app.
	LocalAppData string
}

// Plan says which file updates the app and how. ok=false means this install
// can't update itself (a .deb/.rpm, or an unknown layout): the person
// downloads the new version instead, and Reason says why.
type Plan struct {
	Method Method
	Asset  Asset
	// Path is what gets replaced: the .exe, the .app bundle or the AppImage.
	Path string
}

func findAsset(rel Release, suffix string) (Asset, bool) {
	for _, a := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(a.Name), suffix) {
			return a, true
		}
	}
	return Asset{}, false
}

// PlanFor picks the update method for the running app.
func PlanFor(rel Release, t Target) (Plan, string) {
	switch t.GOOS {
	case "windows":
		// Compared as text with one slash style, so this also works when
		// tested on another OS.
		norm := func(p string) string { return strings.ToLower(strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")) }
		installed := t.LocalAppData != "" && strings.HasPrefix(norm(t.Exe), norm(t.LocalAppData)+"/programs/")
		if installed {
			if a, ok := findAsset(rel, "-windows-amd64-setup.exe"); ok {
				return Plan{Method: WindowsSetup, Asset: a, Path: t.Exe}, ""
			}
		} else if a, ok := findAsset(rel, "-windows-amd64-portable.exe"); ok {
			return Plan{Method: WindowsPortable, Asset: a, Path: t.Exe}, ""
		}
	case "darwin":
		// .../TrueDeploy.app/Contents/MacOS/truedeploy
		macOS := path.Dir(filepath.ToSlash(t.Exe))
		bundle := path.Dir(path.Dir(macOS))
		if path.Base(macOS) == "MacOS" && strings.HasSuffix(bundle, ".app") {
			if a, ok := findAsset(rel, "-macos-universal.zip"); ok {
				return Plan{Method: MacBundle, Asset: a, Path: bundle}, ""
			}
		}
		return Plan{}, "Run TrueDeploy from its .app to update it in place."
	case "linux":
		if t.AppImage == "" {
			return Plan{}, "Installed from a .deb or .rpm package: install the new package to update."
		}
		if a, ok := findAsset(rel, "-linux-amd64.appimage"); ok {
			return Plan{Method: LinuxAppImage, Asset: a, Path: t.AppImage}, ""
		}
	}
	return Plan{}, "This release has no update for this platform yet."
}

// ErrChecksum means the downloaded file doesn't match the release's checksums.
var ErrChecksum = errors.New("the download doesn't match the release's checksum, so it wasn't installed")

// checksums reads the release's SHA256SUMS file ("hash  name" lines).
func checksums(ctx context.Context, client *http.Client, rel Release) (map[string]string, error) {
	a, ok := findAsset(rel, "sha256sums")
	if !ok {
		return nil, errors.New("this release has no SHA256SUMS file, so updates can't be verified")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TrueDeploy")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't download the checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("couldn't download the checksums (HTTP %d)", resp.StatusCode)
	}
	sums := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			sums[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return sums, sc.Err()
}

// Download saves the plan's file into dir after checking its SHA-256 against
// the release's SHA256SUMS. progress (optional) gets bytes done and total.
func Download(ctx context.Context, client *http.Client, rel Release, p Plan, dir string, progress func(done, total int64)) (string, error) {
	if !strings.HasPrefix(p.Asset.URL, "https://github.com/") && !strings.HasPrefix(p.Asset.URL, API) {
		return "", errors.New("refusing to download an update from outside GitHub")
	}
	sums, err := checksums(ctx, client, rel)
	if err != nil {
		return "", err
	}
	want, ok := sums[p.Asset.Name]
	if !ok {
		return "", fmt.Errorf("%s isn't listed in the release's checksums", p.Asset.Name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, p.Asset.Name)
	// Already downloaded and verified earlier?
	if got, err := fileSHA256(dst); err == nil && got == want {
		return dst, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "TrueDeploy")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't download the update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("couldn't download the update (HTTP %d)", resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = p.Asset.Size
	}
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return "", werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return "", fmt.Errorf("the download was interrupted: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		os.Remove(tmp)
		return "", ErrChecksum
	}
	os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Script returns the helper that installs a downloaded update once the app
// (pid) has exited, then starts the new version when relaunch is set. It's a
// PowerShell script on Windows and a sh script elsewhere.
func Script(p Plan, downloaded string, pid int, relaunch bool) string {
	switch p.Method {
	case WindowsSetup, WindowsPortable:
		var b strings.Builder
		fmt.Fprintf(&b, "$ErrorActionPreference = 'Stop'\n")
		fmt.Fprintf(&b, "try { Wait-Process -Id %d -Timeout 120 -ErrorAction SilentlyContinue } catch {}\n", pid)
		if p.Method == WindowsSetup {
			// /S: silent. The installer overwrites the per-user install in place.
			fmt.Fprintf(&b, "Start-Process -FilePath %s -ArgumentList '/S' -Wait\n", psQuote(downloaded))
		} else {
			// Retry: Windows can hold the old .exe for a moment after exit.
			fmt.Fprintf(&b, "for ($i = 0; $i -lt 20; $i++) { try { Copy-Item -LiteralPath %s -Destination %s -Force; break } catch { Start-Sleep -Milliseconds 500 } }\n",
				psQuote(downloaded), psQuote(p.Path))
		}
		if relaunch {
			fmt.Fprintf(&b, "Start-Process -FilePath %s\n", psQuote(p.Path))
		}
		return b.String()
	case MacBundle:
		s := fmt.Sprintf(`while kill -0 %[1]d 2>/dev/null; do sleep 0.3; done
T=$(mktemp -d) || exit 1
ditto -x -k %[2]s "$T" || exit 1
NEW=$(find "$T" -maxdepth 1 -name '*.app' | head -n 1)
[ -n "$NEW" ] || exit 1
APP=%[3]s
rm -rf "$APP.td-old"
mv "$APP" "$APP.td-old" || exit 1
if mv "$NEW" "$APP"; then rm -rf "$APP.td-old"; else mv "$APP.td-old" "$APP"; exit 1; fi
xattr -dr com.apple.quarantine "$APP" 2>/dev/null
rm -rf "$T"
`, pid, shQuote(downloaded), shQuote(p.Path))
		if relaunch {
			s += "open \"$APP\"\n"
		}
		return s
	case LinuxAppImage:
		s := fmt.Sprintf(`while kill -0 %[1]d 2>/dev/null; do sleep 0.3; done
chmod +x %[2]s && mv -f %[2]s %[3]s || exit 1
`, pid, shQuote(downloaded), shQuote(p.Path))
		if relaunch {
			s += fmt.Sprintf("nohup %s >/dev/null 2>&1 &\n", shQuote(p.Path))
		}
		return s
	}
	return ""
}
