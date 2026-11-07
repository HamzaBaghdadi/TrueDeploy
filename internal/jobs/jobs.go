// Package jobs runs a project's scheduled commands with cron. Each job runs
// inside the project's running container (its environment and private
// network included), from the SSH user's own crontab, so no admin rights
// are needed and jobs keep running while TrueDeploy is closed.
//
// Files live in ~/truedeploy/jobs/<slug>/: <id>.sh (the job), <id>.log
// (its output, trimmed), <id>.status (start, exit code and duration of the
// last run) and <id>.lock (prevents overlapping runs).
package jobs

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"truedeploy/internal/deploy"
)

// Job is one scheduled command.
type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"` // cron expression (UTC), e.g. "*/5 * * * *" or "@daily"
	// Service is the compose service to run in ("" for Dockerfile projects).
	Service string `json:"service"`
	Command string `json:"command"`
	// TimeoutMinutes stops runs that take longer (0 = 60).
	TimeoutMinutes int  `json:"timeoutMinutes"`
	Enabled        bool `json:"enabled"`
}

// Target says where jobs run.
type Target struct {
	Slug        string
	Dockerfile  bool   // runtime: one container td-<slug>
	ComposeFile string // compose runtime ("" = auto-detect)
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9]{4,40}$`)
	serviceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	fieldRe   = regexp.MustCompile(`^[0-9*/,\-]+$`)
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	macros    = map[string]bool{"@hourly": true, "@daily": true, "@weekly": true, "@monthly": true, "@yearly": true, "@annually": true, "@midnight": true}
)

// ValidSchedule checks a 5-field cron expression or a macro like @daily.
func ValidSchedule(s string) error {
	s = strings.TrimSpace(s)
	if macros[s] {
		return nil
	}
	f := strings.Fields(s)
	if len(f) != 5 {
		return fmt.Errorf("%q isn't a schedule: use 5 cron fields (minute hour day month weekday), e.g. */15 * * * *", s)
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range f {
		if !fieldRe.MatchString(field) {
			return fmt.Errorf("%q isn't a valid cron field (only numbers, *, /, , and - are allowed)", field)
		}
		for _, n := range regexp.MustCompile(`\d+`).FindAllString(field, -1) {
			v, _ := strconv.Atoi(n)
			if strings.Contains(field, "/") && strings.HasSuffix(field, "/"+n) {
				if v < 1 {
					return fmt.Errorf("the step in %q must be at least 1", field)
				}
				continue
			}
			if v < limits[i][0] || v > limits[i][1] {
				return fmt.Errorf("%d is out of range in %q", v, field)
			}
		}
	}
	return nil
}

func (j Job) Validate(dockerfile bool) error {
	if !idRe.MatchString(j.ID) {
		return errors.New("invalid job id")
	}
	if strings.TrimSpace(j.Name) == "" {
		return errors.New("give the job a name")
	}
	if strings.TrimSpace(j.Command) == "" {
		return errors.New("enter the command to run")
	}
	if strings.ContainsAny(j.Command, "\r\n") {
		return errors.New("the command must be one line (chain commands with &&)")
	}
	if !dockerfile && !serviceRe.MatchString(j.Service) {
		return errors.New("choose the service to run the job in")
	}
	if j.TimeoutMinutes < 0 || j.TimeoutMinutes > 24*60 {
		return errors.New("the timeout must be between 1 and 1440 minutes")
	}
	return ValidSchedule(j.Schedule)
}

func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Dir is where a project's job files live.
func Dir(slug string) string { return "$HOME/truedeploy/jobs/" + slug }

func marker(slug, id string) string { return "# truedeploy-job-" + slug + "-" + id }

// prefix matches every job line of a project (for replacing them all).
func prefix(slug string) string { return "# truedeploy-job-" + slug + "-" }

// Script is the file cron runs. With --here (Run now) its output also goes
// to stdout.
func Script(t Target, j Job) string {
	timeout := j.TimeoutMinutes
	if timeout == 0 {
		timeout = 60
	}
	var run string
	if t.Dockerfile {
		run = fmt.Sprintf(`docker exec %s sh -c "$JOBCMD"`, q(deploy.ContainerName(t.Slug)))
	} else {
		run = fmt.Sprintf(`docker compose -p %s -f "$CF" exec -T %s sh -c "$JOBCMD"`, q(t.Slug), q(j.Service))
	}
	detect := ""
	if !t.Dockerfile {
		detect = deploy.ComposeDetect(t.ComposeFile)
	}
	return fmt.Sprintf(`#!/usr/bin/env bash
# Managed by TrueDeploy: scheduled job %[1]s of %[2]s. Edit it in the app.
set -uo pipefail
export PATH="$HOME/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
D=%[3]s
ID=%[4]s
LOG="$D/$ID.log"
mkdir -p "$D"
# Keep the log short, then send this run's output to it.
[ -f "$LOG" ] && tail -n 1000 "$LOG" > "$LOG.tmp" && mv "$LOG.tmp" "$LOG"
if [ "${1:-}" = "--here" ]; then exec > >(tee -a "$LOG") 2>&1; else exec >>"$LOG" 2>&1; fi
exec 9>"$D/$ID.lock"
if command -v flock >/dev/null && ! flock -n 9; then
  echo "$(date -u +%%FT%%TZ) skipped: the previous run is still going"
  exit 0
fi
JOBCMD=$(echo %[5]s | base64 -d)
start=$(date +%%s)
echo "$(date -u +%%FT%%TZ) ==> %[1]s: $JOBCMD"
(
  fail() { echo "$*" >&2; exit 1; }
  cd "$HOME/apps/"%[2]s 2>/dev/null || fail "The project isn't deployed on this server."
  %[6]sif command -v timeout >/dev/null; then
    timeout --kill-after=30 %[7]dm %[8]s
  else
    %[8]s
  fi
)
code=$?
dur=$(( $(date +%%s) - start ))
[ "$code" = 124 ] && echo "Stopped: it ran longer than %[7]d minutes."
printf '%%s\t%%s\t%%s\n' "$start" "$code" "$dur" > "$D/$ID.status"
echo "$(date -u +%%FT%%TZ) ==> exit $code (${dur}s)"
exit "$code"
`, q(j.Name), q(t.Slug), Dir(t.Slug), q(j.ID), base64.StdEncoding.EncodeToString([]byte(j.Command)),
		strings.ReplaceAll(detect, "\n", "\n  "), timeout, run)
}

// ApplyScript installs the project's jobs: one script per job and one cron
// line per enabled job, replacing the project's previous lines (other cron
// jobs are kept). Scripts of removed jobs are deleted; logs are kept.
func ApplyScript(t Target, list []Job) (string, error) {
	if !slugRe.MatchString(t.Slug) {
		return "", fmt.Errorf("invalid project folder %q", t.Slug)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `fail() { echo "$*" >&2; exit 1; }
D=%s
mkdir -p "$D" && chmod 700 "$D" 2>/dev/null || true
KEEP=" "
`, Dir(t.Slug))
	var lines []string
	enabled := 0
	for _, j := range list {
		if err := j.Validate(t.Dockerfile); err != nil {
			return "", fmt.Errorf("%s: %w", j.Name, err)
		}
		fmt.Fprintf(&b, "echo %s | base64 -d > \"$D/%s.sh\" && chmod 700 \"$D/%s.sh\"\nKEEP=\"${KEEP}%s.sh \"\n",
			base64.StdEncoding.EncodeToString([]byte(Script(t, j))), j.ID, j.ID, j.ID)
		if j.Enabled {
			enabled++
			lines = append(lines, fmt.Sprintf(`%s $D/%s.sh %s`, strings.TrimSpace(j.Schedule), j.ID, marker(t.Slug, j.ID)))
		}
	}
	b.WriteString(`for f in "$D"/*.sh; do
  [ -e "$f" ] || continue
  case "$KEEP" in *" $(basename "$f") "*) ;; *) rm -f "$f" ;; esac
done
`)
	if enabled > 0 {
		b.WriteString(`command -v crontab >/dev/null || fail "cron isn't installed on this server. Ask your admin to run: sudo apt-get install -y cron"
`)
	} else {
		b.WriteString(`command -v crontab >/dev/null || { echo "No scheduled jobs."; exit 0; }
`)
	}
	// Read the whole crontab first: piping "crontab -l" into "crontab -" can race.
	fmt.Fprintf(&b, `CUR=$(crontab -l 2>/dev/null || true)
{
  printf '%%s\n' "$CUR" | grep -vF %s | sed '/^$/d' || true
`, q(prefix(t.Slug)))
	for _, l := range lines {
		// $D must expand to the real path in the crontab line.
		fmt.Fprintf(&b, "  echo \"%s\"\n", strings.ReplaceAll(l, `"`, `\"`))
	}
	fmt.Fprintf(&b, `} | crontab -
echo "%d scheduled job(s) active."`, enabled)
	return b.String(), nil
}

// StatusScript prints, per job, "S\t<id>\t<start>\t<exit>\t<seconds>" for
// the last finished run and "R\t<id>" while a run is going.
func StatusScript(slug string, ids []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "D=%s\n", Dir(slug))
	for _, id := range ids {
		if !idRe.MatchString(id) {
			continue
		}
		fmt.Fprintf(&b, `[ -f "$D/%[1]s.status" ] && printf 'S\t%[1]s\t%%s\n' "$(cat "$D/%[1]s.status")"
if command -v flock >/dev/null && [ -f "$D/%[1]s.lock" ] && ! flock -n "$D/%[1]s.lock" true; then printf 'R\t%[1]s\n'; fi
`, id)
	}
	b.WriteString("true")
	return b.String()
}

// RunScript runs a job now (its output streams back and goes to its log).
func RunScript(slug, id string) (string, error) {
	if !idRe.MatchString(id) || !slugRe.MatchString(slug) {
		return "", errors.New("invalid job")
	}
	return fmt.Sprintf(`S="%s/%s.sh"
[ -x "$S" ] || { echo "The job isn't installed on the server yet: save it first." >&2; exit 1; }
exec bash "$S" --here`, Dir(slug), id), nil
}

// LogScript prints the last lines of a job's log.
func LogScript(slug, id string, lines int) (string, error) {
	if !idRe.MatchString(id) || !slugRe.MatchString(slug) {
		return "", errors.New("invalid job")
	}
	return fmt.Sprintf(`tail -n %d "%s/%s.log" 2>/dev/null || echo "No runs yet."`, lines, Dir(slug), id), nil
}

// Status is the last run of a job.
type Status struct {
	Start   int64 `json:"start"`   // unix time, 0 = never ran
	Exit    int   `json:"exit"`    // exit code
	Seconds int   `json:"seconds"` // duration
	Running bool  `json:"running"`
}

// ParseStatus reads StatusScript's output.
func ParseStatus(lines []string) map[string]Status {
	out := map[string]Status{}
	for _, l := range lines {
		f := strings.Split(strings.TrimSpace(l), "\t")
		switch {
		case f[0] == "S" && len(f) >= 5:
			st := out[f[1]]
			st.Start, _ = strconv.ParseInt(f[2], 10, 64)
			st.Exit, _ = strconv.Atoi(f[3])
			st.Seconds, _ = strconv.Atoi(f[4])
			out[f[1]] = st
		case f[0] == "R" && len(f) >= 2:
			st := out[f[1]]
			st.Running = true
			out[f[1]] = st
		}
	}
	return out
}
