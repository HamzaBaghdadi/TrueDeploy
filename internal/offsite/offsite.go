// Package offsite copies backups to S3-compatible storage, so they survive
// the server. Servers upload with rclone (run in Docker, nothing to install);
// the app lists and downloads copies itself (internal/s3), which also works
// when the server is gone.
//
// Layout in the bucket: <prefix>/<server id>/databases/<file> and
// <prefix>/<server id>/projects/<slug>/<file>.
package offsite

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"truedeploy/internal/s3"
)

// Target is the storage to copy backups to.
type Target struct {
	Provider  string `json:"provider"` // aws, r2, b2, wasabi, spaces, other (for the UI)
	Endpoint  string `json:"endpoint"` // empty for AWS
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"` // folder inside the bucket
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// Image is the rclone image servers upload with.
const Image = "rclone/rclone:1"

const (
	// Files on each server: rclone's settings (credentials, mode 600) and
	// the destination folder for this server.
	EnvPath  = "$HOME/truedeploy/offsite.env"
	DestPath = "$HOME/truedeploy/offsite.dest"
	remote   = "tdoff"
)

var (
	bucketRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{1,61}[A-Za-z0-9]$`)
	prefixRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	regionRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	keyRe    = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)
	idRe     = regexp.MustCompile(`^[a-z0-9]{1,64}$`)
	slugRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
)

// Clean trims the fields and fills defaults.
func (t Target) Clean() Target {
	t.Endpoint = strings.TrimRight(strings.TrimSpace(t.Endpoint), "/")
	t.Region = strings.TrimSpace(t.Region)
	t.Bucket = strings.TrimSpace(t.Bucket)
	t.Prefix = strings.Trim(strings.TrimSpace(t.Prefix), "/")
	t.AccessKey = strings.TrimSpace(t.AccessKey)
	t.SecretKey = strings.TrimSpace(t.SecretKey)
	if t.Prefix == "" {
		t.Prefix = "truedeploy"
	}
	if t.Region == "" {
		t.Region = "us-east-1"
	}
	return t
}

func (t Target) Validate() error {
	if t.Endpoint != "" {
		u, err := url.Parse(t.Endpoint)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" {
			return fmt.Errorf("%q isn't a storage URL like https://s3.example.com", t.Endpoint)
		}
	}
	if !regionRe.MatchString(t.Region) {
		return fmt.Errorf("%q isn't a region (like us-east-1, or auto for Cloudflare R2)", t.Region)
	}
	if !bucketRe.MatchString(t.Bucket) || strings.Contains(t.Bucket, "..") {
		return errors.New("enter the bucket's name (letters, digits, dots and dashes)")
	}
	if !prefixRe.MatchString(t.Prefix) || len(t.Prefix) > 100 {
		return errors.New("the folder may contain letters, digits, dots, dashes and slashes")
	}
	for _, seg := range strings.Split(t.Prefix, "/") {
		if seg == "." || seg == ".." {
			return errors.New("the folder can't contain . or ..")
		}
	}
	if !keyRe.MatchString(t.AccessKey) || !keyRe.MatchString(t.SecretKey) {
		return errors.New("enter the access key and secret key (no spaces)")
	}
	return nil
}

// Client talks to the bucket from the app.
func (t Target) Client() *s3.Client {
	return s3.New(s3.Config{Endpoint: t.Endpoint, Region: t.Region, Bucket: t.Bucket, AccessKey: t.AccessKey, SecretKey: t.SecretKey}, nil)
}

// ServerDir is the folder for one server's backups.
func (t Target) ServerDir(serverID string) string { return t.Prefix + "/" + serverID }

// DatabaseKey and ProjectKey are object keys of backup files.
func (t Target) DatabaseKey(serverID, name string) string {
	return t.ServerDir(serverID) + "/databases/" + name
}

func (t Target) ProjectKey(serverID, slug, name string) string {
	return t.ServerDir(serverID) + "/projects/" + slug + "/" + name
}

// envFile is rclone's configuration, passed to the container as --env-file.
func (t Target) envFile() string {
	p := "RCLONE_CONFIG_" + strings.ToUpper(remote) + "_"
	lines := []string{
		p + "TYPE=s3",
		p + "ACCESS_KEY_ID=" + t.AccessKey,
		p + "SECRET_ACCESS_KEY=" + t.SecretKey,
		p + "REGION=" + t.Region,
		p + "NO_CHECK_BUCKET=true",
	}
	if t.Endpoint == "" {
		lines = append(lines, p+"PROVIDER=AWS")
	} else {
		lines = append(lines, p+"PROVIDER=Other", p+"ENDPOINT="+t.Endpoint, p+"FORCE_PATH_STYLE=true")
	}
	return strings.Join(lines, "\n") + "\n"
}

// InstallScript writes the server's offsite settings (credentials stay in a
// file only this user can read) and pulls rclone.
func (t Target) InstallScript(serverID string) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	if !idRe.MatchString(serverID) {
		return "", fmt.Errorf("invalid server id %q", serverID)
	}
	return fmt.Sprintf(`set -e
mkdir -p "$HOME/truedeploy" && chmod 700 "$HOME/truedeploy"
(umask 077; printf '%%s' %[1]s > "%[2]s"; printf '%%s\n' %[3]s > "%[4]s")
`+dockerEnv+`docker pull -q %[5]s >/dev/null || echo "Couldn't pull %[5]s yet; it will be pulled on the first backup."
echo "Offsite copies are set up on this server."`,
		shellQuote(t.envFile()), EnvPath, shellQuote(t.Bucket+"/"+t.ServerDir(serverID)), DestPath, Image), nil
}

// RemoveScript deletes the server's offsite settings (copies in the bucket stay).
const RemoveScript = `rm -f "` + EnvPath + `" "` + DestPath + `"
echo "Offsite settings removed from this server."`

// dockerEnv finds Docker (system or rootless) even under cron's bare PATH.
const dockerEnv = `export PATH="$HOME/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
`

// rcloneCmd runs rclone with the server's settings; stdin and stdout pass
// through, so files are read and written by this user, not by the container.
const rcloneCmd = `docker run --rm -i --env-file "` + EnvPath + `" ` + Image

// UploadSnippet copies "$F" to <dest>/<sub>/ when offsite copies are set up
// on the server. It prints ::td::offsite=ok|failed and sets OFFSITE_FAILED=1
// on failure, so scheduled jobs can report it after their local work.
func UploadSnippet(sub string) string {
	return fmt.Sprintf(`OFFSITE_FAILED=""
if [ -f "%[1]s" ] && [ -f "%[2]s" ]; then
  echo "==> Copying $(basename "$F") to offsite storage"
  if %[3]s rcat "%[4]s:$(cat "%[2]s")/%[5]s/$(basename "$F")" < "$F"; then
    echo "::td::offsite=ok"
    echo "Offsite copy saved."
  else
    OFFSITE_FAILED=1
    echo "::td::offsite=failed"
    echo "The offsite copy failed (the backup on the server is fine). Check the storage settings in TrueDeploy." >&2
  fi
else
  echo "::td::offsite=notset"
fi
`, EnvPath, DestPath, rcloneCmd, remote, sub)
}

// PruneSnippet keeps the newest keep files matching pattern (a shell glob
// like "shop-2*") in <dest>/<sub>/, like local retention.
func PruneSnippet(sub, pattern string, keep int) string {
	return fmt.Sprintf(`if [ -z "${OFFSITE_FAILED:-}" ] && [ -f "%[1]s" ] && [ -f "%[2]s" ]; then
  D="%[4]s:$(cat "%[2]s")/%[5]s"
  { %[3]s lsf "$D/" --files-only --include %[6]s </dev/null 2>/dev/null || true; } | sort -r | tail -n +%[7]d | while read -r old; do
    %[3]s deletefile "$D/$old" </dev/null && echo "pruned offsite $old"
  done
fi
`, EnvPath, DestPath, rcloneCmd, remote, sub, shellQuote(pattern), keep+1)
}

// FailIfUploadFailed ends a scheduled job with an error when the upload failed.
const FailIfUploadFailed = `[ -z "${OFFSITE_FAILED:-}" ] || fail "offsite copy failed"
`

// DatabasesSub and ProjectSub are the folders below the server's folder.
const DatabasesSub = "databases"

func ProjectSub(slug string) string { return "projects/" + slug }

// FetchScript downloads an offsite copy into the server's backup folder so
// it can be restored. dir is the local folder (e.g. $HOME/truedeploy/backups).
func FetchScript(sub, name, dir string) (string, error) {
	if !strings.HasPrefix(sub, "projects/") && sub != DatabasesSub {
		return "", fmt.Errorf("invalid folder %q", sub)
	}
	if s, ok := strings.CutPrefix(sub, "projects/"); ok && !slugRe.MatchString(s) {
		return "", fmt.Errorf("invalid folder %q", sub)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`).MatchString(name) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%q isn't a backup file name", name)
	}
	return fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
[ -f "%[1]s" ] && [ -f "%[2]s" ] || fail "Offsite copies aren't set up on this server. Save the storage settings again."
`+dockerEnv+`docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un)."
mkdir -p "%[6]s" && chmod 700 "%[6]s" 2>/dev/null || true
T="%[6]s/.%[5]s.part"
%[3]s cat "%[4]s:$(cat "%[2]s")/%[7]s/%[5]s" > "$T" || { rm -f "$T"; fail "Couldn't download %[5]s from offsite storage."; }
[ -s "$T" ] || { rm -f "$T"; fail "%[5]s is empty in offsite storage."; }
chmod 600 "$T" && mv "$T" "%[6]s/%[5]s"
echo "Copied %[5]s to the server ($(du -h "%[6]s/%[5]s" | cut -f1)). You can restore it now."`,
		EnvPath, DestPath, rcloneCmd, remote, name, dir, sub), nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
