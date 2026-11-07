// Package volbackup backs up a compose project's Docker volumes (uploads,
// app data, embedded databases) into one .tar.gz on the server, and restores
// it. Containers are stopped for the few seconds the copy takes, so files
// that change together (like a database's) are captured consistently.
//
// Backups live in ~/truedeploy/backups/projects/<slug>/, one folder per
// project, named <UTC time>.tar.gz (or prerestore-<UTC time>.tar.gz for the
// safety copy taken before a restore).
package volbackup

import (
	"encoding/base64"
	"fmt"
	"regexp"

	"truedeploy/internal/databases"
	"truedeploy/internal/offsite"
)

// Image runs tar inside Docker, so volumes owned by root (or by the rootless
// Docker user) can be read without sudo.
const Image = "alpine:3"

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	nameRe = regexp.MustCompile(`^(prerestore-)?\d{8}-\d{6}\.tar\.gz$`)
)

// Dir is the project's backup folder on the server.
func Dir(slug string) string { return "$HOME/truedeploy/backups/projects/" + slug }

// ValidName reports whether name is a backup file name (and nothing else).
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Path is the remote path of a validated backup.
func Path(slug, name string) string { return Dir(slug) + "/" + name }

func checkSlug(slug string) error {
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("invalid project folder %q", slug)
	}
	return nil
}

// dockerEnv finds Docker (system or rootless) even under cron's bare PATH.
const dockerEnv = `export PATH="$HOME/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un)."
`

// setup sets $P, $DIR and $VOLS (the project's named volumes).
func setup(slug string) string {
	return fmt.Sprintf(`P=%[1]s
DIR="%[2]s"
mkdir -p "$DIR" && chmod 700 "$DIR" 2>/dev/null || true
VOLS=$(docker volume ls -q --filter "label=com.docker.compose.project=$P" | grep -E "^${P}_[A-Za-z0-9_.-]+$" || true)
`, slug, Dir(slug))
}

// pause stops the project's running containers and restarts them on exit.
const pause = `RUNNING=$(docker ps -q --filter "label=com.docker.compose.project=$P" || true)
resume() { if [ -n "$RUNNING" ]; then docker start $RUNNING >/dev/null 2>&1 || echo "Couldn't restart some containers; deploy the project again." >&2; fi; }
trap resume EXIT
if [ -n "$RUNNING" ]; then
  echo "==> Stopping $(echo "$RUNNING" | wc -l | tr -d ' ') container(s) for a consistent copy"
  docker stop $RUNNING >/dev/null
fi
`

// archive writes $VOLS into $DIR/<prefix><time>.tar.gz and sets $F.
func archive(prefix string) string {
	return fmt.Sprintf(`ARGS=""
for v in $VOLS; do ARGS="$ARGS -v $v:/v/$v:ro"; done
F="$DIR/%s$(date -u +%%Y%%m%%d-%%H%%M%%S).tar.gz"
set -o pipefail
docker run --rm $ARGS %s tar czf - -C /v . > "$F" || { rm -f "$F"; fail "Backup failed (see output above)."; }
chmod 600 "$F"
echo "::td::backup=$(basename "$F")"
echo "Backup saved: $(basename "$F") ($(du -h "$F" | cut -f1)) with $(echo $VOLS | wc -w | tr -d ' ') volume(s): $(echo $VOLS)"
`, prefix, Image)
}

const noVolumes = `[ -n "$VOLS" ] || fail "This project has no Docker volumes to back up. (Its data lives in the containers or nowhere; define named volumes in compose.yaml to keep it.)"
`

// BackupScript backs up the project's volumes now.
func BackupScript(slug string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	return `fail() { echo "$*" >&2; exit 1; }
` + dockerEnv + setup(slug) + noVolumes +
		// Pull first so containers are only down for the copy itself.
		"docker pull -q " + Image + " >/dev/null || fail \"Couldn't pull " + Image + " (needed to read volumes).\"\n" +
		pause + archive("") + "resume\ntrap - EXIT\n", nil
}

// ListScript prints name<TAB>bytes<TAB>unix-time per backup, newest first.
func ListScript(slug string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	return fmt.Sprintf(`ls -1t "%s"/*.tar.gz 2>/dev/null | head -n 50 | while read -r f; do
  printf '%%s\t%%s\t%%s\n' "$(basename "$f")" "$(stat -c %%s "$f")" "$(stat -c %%Y "$f")"
done`, Dir(slug)), nil
}

// RestoreScript replaces the project's volumes with a backup's contents,
// after saving the current data as a prerestore backup.
func RestoreScript(slug, name string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	if !ValidName(name) {
		return "", fmt.Errorf("%q is not a backup of this project", name)
	}
	return `fail() { echo "$*" >&2; exit 1; }
` + dockerEnv + setup(slug) + fmt.Sprintf(`R="$DIR/%[1]s"
[ -f "$R" ] || fail "Backup %[1]s no longer exists on the server."
RVOLS=$(tar tzf "$R" | cut -d/ -f2 | grep -v '^$' | sort -u)
[ -n "$RVOLS" ] || fail "The backup is empty or damaged."
for v in $RVOLS; do
  echo "$v" | grep -qE "^${P}_[A-Za-z0-9_.-]+$" || fail "The backup contains '$v', which isn't a volume of this project."
done
docker pull -q %[2]s >/dev/null || fail "Couldn't pull %[2]s (needed to write volumes)."
`, name, Image) + pause + `if [ -n "$VOLS" ]; then
  echo "==> Safety backup of the current data"
` + archive("prerestore-") + `fi
echo "==> Restoring ` + name + `"
ARGS=""
for v in $RVOLS; do ARGS="$ARGS -v $v:/v/$v"; done
docker run --rm -i $ARGS ` + Image + ` sh -c 'for d in /v/*/; do find "$d" -mindepth 1 -delete; done; tar xzf - -C /v' < "$R" \
  || fail "Restore failed.${F:+ Your data before the restore is in $(basename "$F").}"
resume
trap - EXIT
echo "Restored ` + name + `.${F:+ The previous data is saved as $(basename "$F").}"
`, nil
}

// DeleteScript removes one backup.
func DeleteScript(slug, name string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	if !ValidName(name) {
		return "", fmt.Errorf("%q is not a backup of this project", name)
	}
	return fmt.Sprintf(`rm -f "%s/%s" && echo "Deleted %s."`, Dir(slug), name, name), nil
}

func marker(slug string) string { return "# truedeploy-volbackup-" + slug }

// ScheduleScript installs (or replaces) the cron job. Only timestamped
// backups count towards Keep; prerestore copies are never pruned.
func ScheduleScript(slug string, sc databases.Schedule) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	if err := sc.Validate(); err != nil {
		return "", err
	}
	job := fmt.Sprintf(`#!/usr/bin/env bash
# Managed by TrueDeploy: scheduled volume backup of %[1]s.
set -euo pipefail
fail() { echo "$(date -u +%%FT%%TZ) $*" >&2; exit 1; }
echo "$(date -u +%%FT%%TZ) starting backup"
`, slug) + dockerEnv + setup(slug) + noVolumes + pause + archive("") + `resume
trap - EXIT
` + offsiteUpload(slug, sc) + fmt.Sprintf(`ls -1 "$DIR"/2*.tar.gz 2>/dev/null | sort -r | tail -n +%d | while read -r old; do rm -f "$old"; echo "pruned $(basename "$old")"; done
`, sc.Keep+1) + offsitePrune(slug, sc)
	return fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
command -v crontab >/dev/null || fail "cron isn't installed on this server. Ask your admin to run: sudo apt-get install -y cron"
DIR="%[1]s"
mkdir -p "$DIR" && chmod 700 "$DIR" 2>/dev/null || true
S="$DIR/.backup.sh"
echo %[2]s | base64 -d > "$S"
chmod 700 "$S"
# Read the whole crontab first: piping "crontab -l" straight into "crontab -" can race.
CUR=$(crontab -l 2>/dev/null || true)
{ printf '%%s\n' "$CUR" | grep -vF '%[3]s' | sed '/^$/d' || true; echo "%[4]s $S >> $DIR/.backup.log 2>&1 %[3]s"; } | crontab -
crontab -l | grep -qF '%[3]s' || fail "Couldn't save the cron job."
echo "Scheduled backups are on."`, Dir(slug), base64.StdEncoding.EncodeToString([]byte(job)), marker(slug), sc.Cron()), nil
}

// UnscheduleScript removes the cron job (backups are kept).
func UnscheduleScript(slug string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	return fmt.Sprintf(`if command -v crontab >/dev/null; then
  CUR=$(crontab -l 2>/dev/null || true)
  { printf '%%s\n' "$CUR" | grep -vF '%[1]s' | sed '/^$/d' || true; } | crontab -
fi
rm -f "%[2]s/.backup.sh"
echo "Scheduled backups are off. Existing backups were kept."`, marker(slug), Dir(slug)), nil
}

// StatusScript prints "cron=<line>" if scheduled, then "log=" lines.
func StatusScript(slug string) (string, error) {
	if err := checkSlug(slug); err != nil {
		return "", err
	}
	return fmt.Sprintf(`if command -v crontab >/dev/null; then
  l=$(crontab -l 2>/dev/null | grep -F '%[1]s' || true)
  [ -n "$l" ] && echo "cron=$l"
fi
tail -n 5 "%[2]s/.backup.log" 2>/dev/null | sed 's/^/log=/' || true`, marker(slug), Dir(slug)), nil
}

// offsiteUpload copies the new backup ($F) offsite when the schedule asks for it.
func offsiteUpload(slug string, sc databases.Schedule) string {
	if !sc.Offsite {
		return ""
	}
	return offsite.UploadSnippet(offsite.ProjectSub(slug))
}

// offsitePrune applies the same retention offsite, then fails the job if the
// upload failed (so it shows up in the log).
func offsitePrune(slug string, sc databases.Schedule) string {
	if !sc.Offsite {
		return ""
	}
	return offsite.PruneSnippet(offsite.ProjectSub(slug), "2*", sc.Keep) + offsite.FailIfUploadFailed
}
