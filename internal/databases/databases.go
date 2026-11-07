// Package databases runs managed database containers on a server. Each
// database lives on the private "truedeploy" Docker network (no published
// ports), keeps its data in a named volume, and receives its password via a
// short-lived env file so it never appears on a command line.
package databases

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"truedeploy/internal/offsite"
)

// Network is the private Docker network shared by databases and the
// projects linked to them.
const Network = "truedeploy"

type Engine struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Versions    []string `json:"versions"` // newest first
	Port        int      `json:"port"`
	DefaultEnv  string   `json:"defaultEnv"` // env var name used when linking
	HasDatabase bool     `json:"hasDatabase"`
	Backup      bool     `json:"backup"`
}

var Engines = []Engine{
	{ID: "postgres", Name: "PostgreSQL", Versions: []string{"17", "16", "15"}, Port: 5432, DefaultEnv: "DATABASE_URL", HasDatabase: true, Backup: true},
	{ID: "mysql", Name: "MySQL", Versions: []string{"8.4", "8.0"}, Port: 3306, DefaultEnv: "DATABASE_URL", HasDatabase: true, Backup: true},
	{ID: "redis", Name: "Redis", Versions: []string{"7", "6"}, Port: 6379, DefaultEnv: "REDIS_URL"},
	{ID: "mongo", Name: "MongoDB", Versions: []string{"8", "7"}, Port: 27017, DefaultEnv: "MONGODB_URL", HasDatabase: true, Backup: true},
}

func FindEngine(id string) (Engine, bool) {
	for _, e := range Engines {
		if e.ID == id {
			return e, true
		}
	}
	return Engine{}, false
}

// Spec describes one database instance.
type Spec struct {
	Slug     string // container td-db-<slug>, volume td-db-<slug>-data
	Engine   string
	Version  string
	User     string
	Database string
	Password string
}

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
	identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
)

func (s Spec) Validate() error {
	e, ok := FindEngine(s.Engine)
	if !ok {
		return fmt.Errorf("unknown database engine %q", s.Engine)
	}
	if !slugRe.MatchString(s.Slug) {
		return fmt.Errorf("database name must be lowercase letters, digits and dashes")
	}
	found := false
	for _, v := range e.Versions {
		found = found || v == s.Version
	}
	if !found {
		return fmt.Errorf("%s %s isn't supported (choose %s)", e.Name, s.Version, strings.Join(e.Versions, ", "))
	}
	if e.HasDatabase && (!identRe.MatchString(s.User) || !identRe.MatchString(s.Database)) {
		return fmt.Errorf("user and database names must be lowercase letters, digits and underscores")
	}
	if len(s.Password) < 16 {
		return fmt.Errorf("password is too short")
	}
	return nil
}

func ContainerName(slug string) string { return "td-db-" + slug }
func volumeName(slug string) string    { return "td-db-" + slug + "-data" }

// GeneratePassword returns a URL-safe random password (no characters that
// need escaping in connection strings).
func GeneratePassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, 32)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

// URL is the connection string apps on the private network use.
func URL(s Spec) string {
	host := ContainerName(s.Slug)
	pw := url.QueryEscape(s.Password)
	switch s.Engine {
	case "postgres":
		return fmt.Sprintf("postgresql://%s:%s@%s:5432/%s", s.User, pw, host, s.Database)
	case "mysql":
		return fmt.Sprintf("mysql://%s:%s@%s:3306/%s", s.User, pw, host, s.Database)
	case "redis":
		return fmt.Sprintf("redis://:%s@%s:6379/0", pw, host)
	case "mongo":
		return fmt.Sprintf("mongodb://%s:%s@%s:27017/%s?authSource=admin", s.User, pw, host, s.Database)
	}
	return ""
}

// containerEnv is written to a temporary env file for docker run.
func containerEnv(s Spec) map[string]string {
	switch s.Engine {
	case "postgres":
		return map[string]string{"POSTGRES_USER": s.User, "POSTGRES_PASSWORD": s.Password, "POSTGRES_DB": s.Database}
	case "mysql":
		return map[string]string{"MYSQL_USER": s.User, "MYSQL_PASSWORD": s.Password, "MYSQL_DATABASE": s.Database, "MYSQL_ROOT_PASSWORD": s.Password}
	case "redis":
		return map[string]string{"REDIS_PASSWORD": s.Password}
	case "mongo":
		return map[string]string{"MONGO_INITDB_ROOT_USERNAME": s.User, "MONGO_INITDB_ROOT_PASSWORD": s.Password, "MONGO_INITDB_DATABASE": s.Database}
	}
	return nil
}

func image(s Spec) string { return s.Engine + ":" + s.Version }

func dataDir(engine string) string {
	switch engine {
	case "postgres":
		return "/var/lib/postgresql/data"
	case "mysql":
		return "/var/lib/mysql"
	case "redis":
		return "/data"
	case "mongo":
		return "/data/db"
	}
	return "/data"
}

const dockerCheck = `fail() { echo "$*" >&2; exit 1; }
docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un). Run the Docker steps in server Set up first."
`

// NetworkScript makes sure the shared private network exists.
const NetworkScript = `docker network inspect ` + Network + ` >/dev/null 2>&1 || docker network create ` + Network + ` >/dev/null`

// CreateScript starts the database (or recreates the container with the same
// volume, which keeps its data).
func CreateScript(s Spec) string {
	keys := make([]string, 0)
	env := containerEnv(s)
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var envFile strings.Builder
	for _, k := range keys {
		envFile.WriteString(k + "=" + env[k] + "\n")
	}
	cmd := ""
	if s.Engine == "redis" {
		cmd = ` sh -c 'exec redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"'`
	}
	name := ContainerName(s.Slug)
	return dockerCheck + NetworkScript + fmt.Sprintf(`
ENVF=$(mktemp)
trap 'rm -f "$ENVF"' EXIT
(umask 077; echo %[1]s | base64 -d > "$ENVF")
docker rm -f %[2]s >/dev/null 2>&1 || true
echo "==> Pulling %[3]s"
docker pull -q %[3]s
echo "==> Starting %[2]s"
docker run -d --name %[2]s --network %[4]s --network-alias %[2]s --restart unless-stopped \
  --label truedeploy.database=%[5]s --env-file "$ENVF" -v %[6]s:%[7]s %[3]s%[8]s >/dev/null
sleep 3
docker ps --format '{{.Names}}' | grep -qx %[2]s || { docker logs --tail 30 %[2]s >&2; fail "The database didn't start (see output above)."; }
echo "Database is running."`,
		base64.StdEncoding.EncodeToString([]byte(envFile.String())), name, image(s), Network, s.Slug, volumeName(s.Slug), dataDir(s.Engine), cmd)
}

// StatusScript prints: state<TAB>health<TAB>status<TAB>volume size.
func StatusScript(slug string) string {
	n := ContainerName(slug)
	return fmt.Sprintf(`if docker inspect %[1]s >/dev/null 2>&1; then
  printf '%%s\t%%s\t%%s\t' "$(docker inspect -f '{{.State.Status}}' %[1]s)" "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' %[1]s)" "$(docker ps -a --filter name=^/%[1]s$ --format '{{.Status}}')"
  docker system df -v --format '{{range .Volumes}}{{if eq .Name "%[2]s"}}{{.Size}}{{end}}{{end}}' 2>/dev/null || true
else
  printf 'missing\t\t\t\n'
fi`, n, volumeName(slug))
}

// DeleteScript removes the container; the volume (data) only if asked.
func DeleteScript(slug string, deleteData bool) string {
	s := fmt.Sprintf(`docker rm -f %s >/dev/null 2>&1 || true
echo "Container removed."`, ContainerName(slug))
	if deleteData {
		s += fmt.Sprintf(`
docker volume rm %s >/dev/null 2>&1 && echo "Data volume deleted." || echo "No data volume to delete."`, volumeName(slug))
	}
	return s
}

// BackupsDir holds compressed dumps on the server.
const BackupsDir = "$HOME/truedeploy/backups"

// backupBody dumps the database to BackupsDir/<slug>-[<tag>-]<UTC time>.<ext>.gz.
// It needs fail() and leaves the file path in $F.
func backupBody(s Spec, tag string) (string, error) {
	n := ContainerName(s.Slug)
	var dump, ext string
	switch s.Engine {
	case "postgres":
		dump, ext = fmt.Sprintf(`docker exec %s pg_dump -U %s -d %s --no-owner`, n, s.User, s.Database), "sql"
	case "mysql":
		dump, ext = fmt.Sprintf(`docker exec -e MYSQL_PWD="$PW" %s mysqldump -u %s --single-transaction --routines %s`, n, s.User, s.Database), "sql"
	case "mongo":
		dump, ext = fmt.Sprintf(`docker exec %s mongodump --quiet --archive -u %s -p "$PW" --authenticationDatabase admin --db %s`, n, s.User, s.Database), "archive"
	default:
		return "", fmt.Errorf("backups aren't available for this engine")
	}
	prefix := s.Slug + "-"
	if tag != "" {
		prefix += tag + "-"
	}
	return fmt.Sprintf(`PW=$(echo %[1]s | base64 -d)
DIR="%[2]s"
mkdir -p "$DIR" && chmod 700 "$DIR" 2>/dev/null || true
F="$DIR/%[3]s$(date -u +%%Y%%m%%d-%%H%%M%%S).%[4]s.gz"
set -o pipefail
%[5]s | gzip > "$F" || { rm -f "$F"; fail "Backup failed. Is the database running?"; }
chmod 600 "$F"
echo "::td::backup=$F"
echo "Backup saved: $(basename "$F") ($(du -h "$F" | cut -f1))"
`, base64.StdEncoding.EncodeToString([]byte(s.Password)), BackupsDir, prefix, ext, dump), nil
}

// BackupScript writes a compressed dump to BackupsDir and prints its path.
func BackupScript(s Spec) (string, error) {
	body, err := backupBody(s, "")
	if err != nil {
		return "", err
	}
	return dockerCheck + body, nil
}

// ListBackupsScript prints name<TAB>bytes<TAB>unix-time per backup, newest first.
func ListBackupsScript(slug string) string {
	return fmt.Sprintf(`ls -1t "%[1]s"/%[2]s-*.gz 2>/dev/null | grep -E '/%[2]s-(prerestore-)?[0-9]{8}-[0-9]{6}\.[a-z]+\.gz$' | head -n 50 | while read -r f; do
  printf '%%s\t%%s\t%%s\n' "$(basename "$f")" "$(stat -c %%s "$f")" "$(stat -c %%Y "$f")"
done`, BackupsDir, slug)
}

// ValidBackupName checks a file name belongs to this database's backups, so
// names from the UI can't point anywhere else on the server.
func ValidBackupName(slug, name string) bool {
	// Exact shape: another database's backups (shop-api-… for "shop") never match.
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(slug) + `-(prerestore-)?\d{8}-\d{6}\.(sql|archive)\.gz$`)
	return re.MatchString(name)
}

// BackupPath is the remote path of a validated backup file.
func BackupPath(name string) string { return BackupsDir + "/" + name }

// Schedule is how often scheduled backups run, and how many to keep.
type Schedule struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"` // "daily" | "12h" | "6h"
	Hour      int    `json:"hour"`      // UTC hour for daily backups
	Keep      int    `json:"keep"`      // scheduled backups kept (older ones are deleted)
	// Offsite also copies each backup to the offsite storage (same retention).
	Offsite bool `json:"offsite,omitempty"`
}

func (sc Schedule) Validate() error {
	switch sc.Frequency {
	case "daily", "12h", "6h":
	default:
		return fmt.Errorf("unknown backup frequency %q", sc.Frequency)
	}
	if sc.Hour < 0 || sc.Hour > 23 {
		return fmt.Errorf("hour must be between 0 and 23")
	}
	if sc.Keep < 1 || sc.Keep > 365 {
		return fmt.Errorf("keep between 1 and 365 backups")
	}
	return nil
}

// Cron returns the crontab time fields (minute 15 to avoid the top-of-hour rush).
func (sc Schedule) Cron() string {
	switch sc.Frequency {
	case "12h":
		return "15 */12 * * *"
	case "6h":
		return "15 */6 * * *"
	}
	return fmt.Sprintf("15 %d * * *", sc.Hour)
}

func cronMarker(slug string) string { return "# truedeploy-backup-" + slug }

// scheduledScript is the file cron runs: a backup plus retention. Only
// timestamped (untagged) backups count towards Keep, so safety backups taken
// before a restore are never pruned automatically.
func scheduledScript(s Spec, sc Schedule) (string, error) {
	keep := sc.Keep
	body, err := backupBody(s, "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`#!/usr/bin/env bash
# Managed by TrueDeploy: scheduled backup of %[1]s.
set -euo pipefail
fail() { echo "$(date -u +%%FT%%TZ) $*" >&2; exit 1; }
export PATH="$HOME/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
echo "$(date -u +%%FT%%TZ) starting backup"
%[2]s
%[4]sls -1t "$DIR"/%[1]s-%[6]s.*.gz 2>/dev/null | tail -n +%[3]d | while read -r old; do rm -f "$old"; echo "pruned $(basename "$old")"; done
%[5]s`, s.Slug, body, keep+1, offsiteUpload(sc), offsitePrune(s.Slug, sc), scheduledGlob), nil
}

// ScheduleScript installs (or replaces) the cron job for scheduled backups.
// It uses the login user's own crontab, so no admin rights are needed.
func ScheduleScript(s Spec, sc Schedule) (string, error) {
	if err := sc.Validate(); err != nil {
		return "", err
	}
	script, err := scheduledScript(s, sc)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
command -v crontab >/dev/null || fail "cron isn't installed on this server. Ask your admin to run: sudo apt-get install -y cron"
DIR="%[1]s"
mkdir -p "$DIR" && chmod 700 "$DIR" 2>/dev/null || true
S="$DIR/.%[2]s.backup.sh"
echo %[3]s | base64 -d > "$S"
chmod 700 "$S"
# Read the whole crontab first: piping "crontab -l" straight into "crontab -" can race.
CUR=$(crontab -l 2>/dev/null || true)
{ printf '%%s\n' "$CUR" | grep -vF '%[4]s' | sed '/^$/d' || true; echo "%[5]s $S >> $DIR/.%[2]s.backup.log 2>&1 %[4]s"; } | crontab -
crontab -l | grep -qF '%[4]s' || fail "Couldn't save the cron job."
echo "Scheduled backups are on."`, BackupsDir, s.Slug, base64.StdEncoding.EncodeToString([]byte(script)), cronMarker(s.Slug), sc.Cron()), nil
}

// UnscheduleScript removes the cron job and its script (backups are kept).
func UnscheduleScript(slug string) string {
	return fmt.Sprintf(`if command -v crontab >/dev/null; then
  CUR=$(crontab -l 2>/dev/null || true)
  { printf '%%s\n' "$CUR" | grep -vF '%[1]s' | sed '/^$/d' || true; } | crontab -
fi
rm -f "%[2]s/.%[3]s.backup.sh"
echo "Scheduled backups are off. Existing backups were kept."`, cronMarker(slug), BackupsDir, slug)
}

// ScheduleStatusScript prints "cron=<line>" if scheduled, then the last log lines.
func ScheduleStatusScript(slug string) string {
	return fmt.Sprintf(`if command -v crontab >/dev/null; then
  l=$(crontab -l 2>/dev/null | grep -F '%[1]s' || true)
  [ -n "$l" ] && echo "cron=$l"
fi
tail -n 5 "%[2]s/.%[3]s.backup.log" 2>/dev/null | sed 's/^/log=/' || true`, cronMarker(slug), BackupsDir, slug)
}

// RestoreScript restores a backup into the running database, after taking a
// safety backup of the current data (tagged "prerestore").
func RestoreScript(s Spec, name string) (string, error) {
	if !ValidBackupName(s.Slug, name) {
		return "", fmt.Errorf("%q is not a backup of this database", name)
	}
	safety, err := backupBody(s, "prerestore")
	if err != nil {
		return "", err
	}
	n := ContainerName(s.Slug)
	var load string
	switch s.Engine {
	case "postgres":
		// Plain pg_dump output can't replace existing tables, so start from an empty schema.
		load = fmt.Sprintf(`docker exec %[1]s psql -q -U %[2]s -d %[3]s -v ON_ERROR_STOP=1 -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
gunzip -c "$R" | docker exec -i %[1]s psql -q -U %[2]s -d %[3]s -v ON_ERROR_STOP=1 >/dev/null`, n, s.User, s.Database)
	case "mysql":
		load = fmt.Sprintf(`gunzip -c "$R" | docker exec -i -e MYSQL_PWD="$PW" %s mysql -u %s %s`, n, s.User, s.Database)
	case "mongo":
		load = fmt.Sprintf(`gunzip -c "$R" | docker exec -i %s mongorestore --quiet --archive --drop -u %s -p "$PW" --authenticationDatabase admin`, n, s.User)
	}
	return dockerCheck + fmt.Sprintf(`R="%[1]s/%[2]s"
[ -f "$R" ] || fail "Backup %[2]s no longer exists on the server."
echo "==> Safety backup of the current data"
%[3]s
echo "==> Restoring %[2]s"
set -o pipefail
%[4]s || fail "Restore failed. Your data before the restore is in $(basename "$F")."
echo "Restored %[2]s. The previous data is saved as $(basename "$F")."`, BackupsDir, name, safety, load), nil
}

// DeleteBackupScript removes one backup file.
func DeleteBackupScript(slug, name string) (string, error) {
	if !ValidBackupName(slug, name) {
		return "", fmt.Errorf("%q is not a backup of this database", name)
	}
	return fmt.Sprintf(`rm -f "%s/%s" && echo "Deleted %s."`, BackupsDir, name, name), nil
}

// offsiteUpload copies the new backup ($F) offsite when the schedule asks for it.
func offsiteUpload(sc Schedule) string {
	if !sc.Offsite {
		return ""
	}
	return offsite.UploadSnippet(offsite.DatabasesSub)
}

// offsitePrune applies the same retention offsite, then fails the job if
// the upload failed (so it shows up in the log and alerts).
func offsitePrune(slug string, sc Schedule) string {
	if !sc.Offsite {
		return ""
	}
	return offsite.PruneSnippet(offsite.DatabasesSub, slug+"-"+scheduledGlob+".*.gz", sc.Keep) + offsite.FailIfUploadFailed
}

// scheduledGlob matches the time stamp of scheduled (untagged) backups, so
// retention for "shop" never touches "shop-2nd" or "shop-api" backups.
const scheduledGlob = "[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]"
