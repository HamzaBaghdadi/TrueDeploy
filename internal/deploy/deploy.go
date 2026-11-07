// Package deploy builds the shell scripts that deploy a project on a server:
// fetch the repo into ~/apps/<slug>, write .env, then build and start it
// with Docker Compose or a plain Dockerfile. Scripts run as the login user
// and work with both system Docker and rootless Docker.
package deploy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	RuntimeCompose    = "compose"
	RuntimeDockerfile = "dockerfile"
)

// Spec is everything a deploy script needs.
type Spec struct {
	Slug          string
	RepoURL       string
	Branch        string
	Runtime       string
	ComposeFile   string
	HostPort      int
	ContainerPort int
	Env           map[string]string
	// KeepEnv leaves the server's existing .env alone (used by auto-deploy,
	// so secrets never have to live in CI).
	KeepEnv bool
	// Commit pins the deploy to a full commit SHA (rollback). Empty = branch tip.
	Commit string
	// Network, if set, is an existing private Docker network (shared with
	// databases) that the project's containers join.
	Network string
	// Compose, if set, is the compose file itself (one-click apps): there is
	// no Git repository; the script writes compose.yaml into the project folder.
	Compose string
	// Folder is the folder to build, inside the repository (Dockerfile
	// runtime; "" or "." is the root). Useful for monorepos.
	Folder string
	// Files are generated build files (a framework preset), by path inside
	// Folder. When set, .truedeploy/Dockerfile is used instead of the repo's.
	Files      map[string]string
	PresetName string
	// LocalOnly publishes the container port on 127.0.0.1 only, for apps
	// served through a domain (Docker's published ports bypass the firewall).
	LocalOnly bool
	// HealthCheck waits for the app after starting it: HealthPort (on the
	// server, 0 = only check containers aren't crash-looping) must answer
	// HealthPath with a non-5xx status within HealthTimeout seconds.
	HealthCheck   bool
	HealthPort    int
	HealthPath    string
	HealthTimeout int
	// AutoRollback redeploys the last healthy commit when the check fails.
	AutoRollback bool
	// MemoryMB and CPUs cap each of the project's containers (0 = no limit).
	MemoryMB int
	CPUs     float64
}

// MinMemoryMB is the smallest memory limit allowed; less and most apps can't start.
const MinMemoryMB = 32

// Markers are printed by scripts on their own line so the app can pick out
// facts (like the deployed commit) from the log.
const markerPrefix = "::td::"

var (
	slugStrip    = regexp.MustCompile(`[^a-z0-9_-]+`)
	branchRe     = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	envKeyRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	composeRe    = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	scpLikeRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._/~-]+$`)
	shaRe        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoPathRe   = regexp.MustCompile(`^/?([^/]+)/(.+?)(\.git)?/?$`)
	healthPathRe = regexp.MustCompile(`^/[A-Za-z0-9._~/?=&%-]{0,200}$`)
)

// Slug turns a project name into a safe directory / compose project name.
func Slug(name string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-_")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-_")
	}
	return s
}

// Validate checks a spec before it is saved or deployed.
func (s Spec) Validate() error {
	if s.Slug == "" || s.Slug != Slug(s.Slug) {
		return errors.New("project name must contain letters or digits")
	}
	if s.Compose != "" {
		if s.Runtime != RuntimeCompose || s.ComposeFile != "" {
			return errors.New("an app without a repository must use the Docker Compose runtime")
		}
		if s.Commit != "" {
			return errors.New("apps without a repository can't be rolled back to a commit")
		}
	} else {
		if err := ValidateRepoURL(s.RepoURL); err != nil {
			return err
		}
		if !branchRe.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.HasPrefix(s.Branch, "-") {
			return fmt.Errorf("%q is not a valid branch name", s.Branch)
		}
		if s.Commit != "" && !shaRe.MatchString(s.Commit) {
			return fmt.Errorf("%q is not a full commit SHA", s.Commit)
		}
	}
	switch s.Runtime {
	case RuntimeCompose:
		if s.ComposeFile != "" && (!composeRe.MatchString(s.ComposeFile) || strings.Contains(s.ComposeFile, "..")) {
			return fmt.Errorf("%q is not a valid compose file path", s.ComposeFile)
		}
	case RuntimeDockerfile:
		if s.ContainerPort < 1 || s.ContainerPort > 65535 {
			return errors.New("container port must be between 1 and 65535")
		}
		if s.HostPort < 1 || s.HostPort > 65535 {
			return errors.New("public port must be between 1 and 65535")
		}
		if s.Folder != "" && s.Folder != "." && (!composeRe.MatchString(s.Folder) || strings.Contains(s.Folder, "..") || strings.HasPrefix(s.Folder, "/")) {
			return fmt.Errorf("%q is not a folder inside the repository", s.Folder)
		}
		for n := range s.Files {
			if !strings.HasPrefix(n, ".truedeploy/") || strings.Contains(n, "..") || !composeRe.MatchString(n) {
				return fmt.Errorf("generated file %q must be inside .truedeploy/", n)
			}
		}
	default:
		return fmt.Errorf("unknown runtime %q", s.Runtime)
	}
	if s.HealthCheck {
		if s.HealthPort < 0 || s.HealthPort > 65535 {
			return errors.New("the health check port must be between 1 and 65535")
		}
		if s.HealthPath != "" && !healthPathRe.MatchString(s.HealthPath) {
			return fmt.Errorf("%q isn't a path like /health", s.HealthPath)
		}
		if s.HealthTimeout != 0 && (s.HealthTimeout < 5 || s.HealthTimeout > 900) {
			return errors.New("the health check wait must be between 5 and 900 seconds")
		}
	}
	if s.MemoryMB != 0 && (s.MemoryMB < MinMemoryMB || s.MemoryMB > 1<<20) {
		return fmt.Errorf("the memory limit must be between %d MB and 1 TB", MinMemoryMB)
	}
	if s.CPUs != 0 && (s.CPUs < 0.1 || s.CPUs > 256) {
		return errors.New("the CPU limit must be between 0.1 and 256 CPUs")
	}
	for k, v := range s.Env {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("%q is not a valid environment variable name", k)
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("the value of %s can't contain line breaks", k)
		}
	}
	return nil
}

// ValidateRepoURL accepts SSH (git@host:owner/repo.git, ssh://) and HTTPS URLs.
func ValidateRepoURL(raw string) error {
	if scpLikeRe.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(raw)
	if err == nil && (u.Scheme == "https" || u.Scheme == "ssh") && u.Host != "" && strings.Trim(u.Path, "/") != "" {
		return nil
	}
	return fmt.Errorf("%q doesn't look like a Git URL (use git@github.com:owner/repo.git or https://...)", raw)
}

// UsesSSH reports whether cloning needs an SSH key (deploy key).
func UsesSSH(repo string) bool {
	return scpLikeRe.MatchString(repo) || strings.HasPrefix(repo, "ssh://")
}

// DeployKeyURL links to the page for adding a deploy key on GitHub or GitLab,
// or returns "" for other hosts.
func DeployKeyURL(repo string) string {
	host, path := "", ""
	if scpLikeRe.MatchString(repo) {
		at := strings.Index(repo, "@")
		colon := strings.Index(repo, ":")
		host, path = repo[at+1:colon], repo[colon+1:]
	} else if u, err := url.Parse(repo); err == nil {
		host, path = u.Hostname(), u.Path
	}
	m := repoPathRe.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	owner, name := m[1], m[2]
	switch host {
	case "github.com":
		return fmt.Sprintf("https://github.com/%s/%s/settings/keys/new", owner, name)
	case "gitlab.com":
		return fmt.Sprintf("https://gitlab.com/%s/%s/-/settings/repository#js-deploy-keys-settings", owner, name)
	}
	return ""
}

// q single-quotes a value for bash.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// AppDir is where a project lives on the server.
func AppDir(slug string) string { return "$HOME/apps/" + slug }

// KeyPath is the project's own deploy key. GitHub lets a key be a deploy key
// on only one repository, so each project gets its own.
func KeyPath(slug string) string { return "$HOME/.ssh/truedeploy_" + slug }

// KeyScript prints the project's public deploy key, creating it if needed.
func KeyScript(slug string) string {
	return fmt.Sprintf(`mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh" 2>/dev/null || true
K="%s"
[ -f "$K" ] || ssh-keygen -q -t ed25519 -N "" -C "truedeploy-%s@$(hostname)" -f "$K" >/dev/null
cat "$K.pub"`, KeyPath(slug), slug)
}

// prelude sets up git auth (the project's key, else the shared server key
// from setup) and finds Docker (system or rootless).
func prelude(slug string) string {
	return fmt.Sprintf(`KEY="%s"
[ -f "$KEY" ] || KEY="$HOME/.ssh/truedeploy_ed25519"
`, KeyPath(slug)) + preludeRest
}

const preludeRest = `GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
if [ -f "$KEY" ]; then GIT_SSH_COMMAND="$GIT_SSH_COMMAND -i $KEY -o IdentitiesOnly=yes"; fi
export GIT_SSH_COMMAND
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export PATH="$HOME/bin:$PATH"
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
  export DOCKER_SOCK="/run/user/$(id -u)/docker.sock"
fi
fail() { echo "$*" >&2; exit 1; }
`

// composeDetect sets $CF to the compose file to use.
func composeDetect(file string) string {
	if file != "" {
		return fmt.Sprintf(`CF=%s
[ -f "$CF" ] || fail "Compose file $CF not found in the repository."
`, q(file))
	}
	return `CF=""
for f in compose.yaml compose.yml docker-compose.yaml docker-compose.yml; do
  if [ -f "$f" ]; then CF="$f"; break; fi
done
[ -n "$CF" ] || fail "No compose.yaml or docker-compose.yml in the repository. Add one, or switch the project to the Dockerfile runtime."
`
}

func containerName(slug string) string { return "td-" + slug }

// accessCheck needs $REPO and $BRANCH. It turns git's errors into plain advice.
const accessCheck = `if ! OUT=$(git ls-remote --heads "$REPO" "$BRANCH" 2>&1); then
  if echo "$OUT" | grep -qiE "permission denied|could not read|authentication failed|repository not found"; then
    fail "The server can't read this repository. Add the server's deploy key to the repo (read-only is enough), then try again."
  fi
  fail "$(echo "$OUT" | grep -v '^\s*$' | tail -n 1)"
fi
[ -n "$OUT" ] || fail "Branch '$BRANCH' doesn't exist in the repository."
`

// AccessScript checks that the server can read the repo and branch.
func AccessScript(s Spec) string {
	return prelude(s.Slug) + fmt.Sprintf(`REPO=%s
BRANCH=%s
command -v git >/dev/null || fail "git is not installed on the server. Run the 'Install essential tools' setup step."
`, q(s.RepoURL), q(s.Branch)) + accessCheck + `echo "Access OK: the server can read branch '$BRANCH'."`
}

// Script returns the deploy script for a spec. Call Validate first.
func Script(s Spec) string {
	var b strings.Builder
	b.WriteString(prelude(s.Slug))
	fmt.Fprintf(&b, `APP_DIR="%s"
command -v docker >/dev/null || [ -x "$HOME/bin/docker" ] || fail "Docker is not installed. Run the Docker setup step first."
docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un). Add this user to the docker group or install rootless Docker (Set up)."
`, AppDir(s.Slug))
	if s.Compose != "" {
		b.WriteString(inlineSource(s.Compose))
	} else {
		b.WriteString(gitSource(s))
	}

	if s.KeepEnv {
		b.WriteString(`[ -f .env ] || (umask 077; touch .env)
echo "==> Using the existing .env"
`)
	} else {
		// .env travels base64-encoded so values can't break out of the script.
		fmt.Fprintf(&b, `(umask 077; echo %s | base64 -d > .env)
echo "==> Wrote .env (%d variables)"
`, base64.StdEncoding.EncodeToString([]byte(envFile(s.Env))), len(s.Env))
	}

	// td_up builds and starts the checked-out revision. It's a function so a
	// rollback can run it again for the last healthy commit.
	b.WriteString("td_up() {\n")
	switch s.Runtime {
	case RuntimeCompose:
		b.WriteString(composeDetect(s.ComposeFile))
		b.WriteString("OVR=\"\"\n")
		if s.Network != "" {
			b.WriteString(composeNetworkOverride(s.Network))
		}
		b.WriteString(composeLimitsOverride(s.MemoryMB, s.CPUs))
		if s.Compose != "" {
			// App images are pinned to a major version; pulling picks up its updates.
			fmt.Fprintf(&b, `echo "==> docker compose pull"
docker compose -p %[1]s -f "$CF" ${OVR:+-f "$OVR"} ${LIM:+-f "$LIM"} pull
`, q(s.Slug))
		}
		fmt.Fprintf(&b, `echo "==> docker compose up ($CF)"
docker compose -p %[1]s -f "$CF" ${OVR:+-f "$OVR"} ${LIM:+-f "$LIM"} up -d --build --remove-orphans
docker compose -p %[1]s -f "$CF" ${OVR:+-f "$OVR"} ${LIM:+-f "$LIM"} ps
`, q(s.Slug))
	case RuntimeDockerfile:
		b.WriteString(buildContext(s))
		bind := ""
		if s.LocalOnly {
			bind = "127.0.0.1:"
		}
		fmt.Fprintf(&b, `IMG=%[1]s:$REV
echo "==> docker build $IMG"
docker build -f "$DF" -t "$IMG" -t %[1]s:latest "$CTX"
docker rm -f %[2]s >/dev/null 2>&1 || true
echo "==> Starting %[2]s on port %[3]d"
%[6]sdocker run -d --name %[2]s --label truedeploy.project=%[4]s --restart unless-stopped --env-file .env -p %[8]s%[3]d:%[5]d %[7]s%[9]s"$IMG"
docker ps --filter name=^/%[2]s$
`, q("truedeploy/"+s.Slug), q(containerName(s.Slug)), s.HostPort, q(s.Slug), s.ContainerPort, ensureNetwork(s.Network), networkFlag(s.Network), bind, limitFlags(s.MemoryMB, s.CPUs))
	}
	b.WriteString(limitsNote(s.MemoryMB, s.CPUs))
	b.WriteString("}\ntd_up\n")
	b.WriteString(healthSection(s))
	return b.String()
}

// healthSection checks the started app and records the last healthy commit
// (in .git, so it survives resets). With AutoRollback, an unhealthy deploy
// of a Git project redeploys that commit and checks it again.
func healthSection(s Spec) string {
	good := `"$APP_DIR/.truedeploy-last-good"`
	if s.Compose == "" {
		good = `"$APP_DIR/.git/truedeploy-last-good"`
	}
	if !s.HealthCheck {
		if s.Compose == "" {
			return `git rev-parse HEAD > ` + good + "\necho \"==> Deployed $REV\""
		}
		return `echo "==> Deployed $REV"`
	}
	label := "label=com.docker.compose.project=" + s.Slug
	logs := fmt.Sprintf(`docker compose -p %s -f "$CF" ${OVR:+-f "$OVR"} logs --no-color --tail 30 2>&1 | tail -n 30`, q(s.Slug))
	if s.Runtime == RuntimeDockerfile {
		label = "label=truedeploy.project=" + s.Slug
		logs = fmt.Sprintf(`docker logs --tail 30 %s 2>&1`, q(containerName(s.Slug)))
	}
	timeout := s.HealthTimeout
	if timeout <= 0 {
		timeout = 60
	}
	var b strings.Builder
	fmt.Fprintf(&b, `td_get() {
  if command -v curl >/dev/null; then curl -s -o /dev/null -w '%%{http_code}' --max-time 5 "$1" 2>/dev/null || true
  elif command -v wget >/dev/null; then wget -q -S -O /dev/null -T 5 "$1" 2>&1 | awk '/HTTP\//{ c = $2 } END { print c }'
  else echo none; fi
}
td_restarting() { [ -n "$(docker ps -q --filter %[1]s --filter status=restarting)" ]; }
# td_health waits for the app to answer; it sets HEALTH_MSG when it doesn't.
td_health() {
  HEALTH_MSG=""
  sleep 3
`, q(label))
	if s.HealthPort > 0 {
		path := s.HealthPath
		if path == "" {
			path = "/"
		}
		fmt.Fprintf(&b, `  url=%[1]s
  echo "==> Health check: $url (up to %[2]ds)"
  end=$(( $(date +%%s) + %[2]d )); code=""
  while :; do
    code=$(td_get "$url")
    case "$code" in
      none) echo "==> Skipping the HTTP check: neither curl nor wget is installed on the server."; break ;;
      [234][0-9][0-9]) echo "==> Healthy: the app answered $code"; return 0 ;;
    esac
    if td_restarting; then HEALTH_MSG="a container keeps restarting (crashing)"; return 1; fi
    [ "$(date +%%s)" -ge "$end" ] && { HEALTH_MSG="$url answered ${code:-nothing} for %[2]d seconds"; [ "$code" = 000 ] && HEALTH_MSG="nothing answered on $url for %[2]d seconds"; return 1; }
    sleep 2
  done
`, q(fmt.Sprintf("http://127.0.0.1:%d%s", s.HealthPort, path)), timeout)
	}
	b.WriteString(`  sleep 5
  if td_restarting; then HEALTH_MSG="a container keeps restarting (crashing)"; return 1; fi
  echo "==> Healthy: containers are running"
  return 0
}
if td_health; then
`)
	if s.Compose == "" {
		b.WriteString(`  git rev-parse HEAD > ` + good + "\n")
	}
	fmt.Fprintf(&b, `  echo "==> Deployed $REV"
else
  FIRST="$HEALTH_MSG"
  echo "::td::unhealthy=$FIRST"
  echo "==> Unhealthy: $FIRST. Last log lines:"
  %s || true
`, logs)
	if s.AutoRollback && s.Compose == "" {
		fmt.Fprintf(&b, `  GOOD=$(cat %[1]s 2>/dev/null || true)
  if [ -n "$GOOD" ] && [ "$GOOD" != "$(git rev-parse HEAD)" ]; then
    BAD=$REV
    echo "==> Rolling back to the last healthy commit ${GOOD:0:7}"
    git fetch --depth 1 origin "$GOOD" && git reset --hard FETCH_HEAD || fail "$BAD is unhealthy ($FIRST), and the rollback couldn't fetch ${GOOD:0:7}."
    REV=$(git rev-parse --short HEAD)
    td_up
    if td_health; then
      echo "::td::rolledback=$REV"
      fail "$BAD failed its health check ($FIRST). Rolled back to $REV, which is healthy."
    fi
    fail "$BAD failed its health check ($FIRST), and the rollback to $REV is unhealthy too ($HEALTH_MSG)."
  fi
`, good)
	}
	b.WriteString(`  fail "$REV started but isn't healthy: $FIRST."
fi`)
	return b.String()
}

// buildContext sets $CTX (the folder to build) and $DF (the Dockerfile),
// writing generated files first when the project uses a framework preset.
func buildContext(s Spec) string {
	folder := s.Folder
	if folder == "" {
		folder = "."
	}
	var b strings.Builder
	fmt.Fprintf(&b, `CTX=%s
[ -d "$CTX" ] || fail "The folder $CTX doesn't exist in the repository. Check the project's app folder."
`, q(folder))
	if folder != "." {
		// Build tools read .env for build-time variables (VITE_*, NEXT_PUBLIC_*).
		b.WriteString(`(umask 077; cp .env "$CTX/.env")
`)
	}
	if len(s.Files) == 0 {
		b.WriteString(`DF="$CTX/Dockerfile"
[ -f "$DF" ] || fail "No Dockerfile in $CTX. Add one, pick a framework preset in the project settings, or switch to Docker Compose."
`)
		return b.String()
	}
	names := make([]string, 0, len(s.Files))
	for n := range s.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, `mkdir -p "$CTX/$(dirname %[1]s)" && echo %[2]s | base64 -d > "$CTX/"%[1]s
`, q(n), base64.StdEncoding.EncodeToString([]byte(s.Files[n])))
	}
	fmt.Fprintf(&b, `DF="$CTX/.truedeploy/Dockerfile"
echo "==> Using the %s preset (generated Dockerfile)"
`, s.PresetName)
	return b.String()
}

// inlineSource writes the app's compose.yaml into $APP_DIR and cds there. The
// revision is a hash of the file, so history shows when the configuration changed.
func inlineSource(compose string) string {
	return fmt.Sprintf(`mkdir -p "$APP_DIR" && cd "$APP_DIR" || fail "Can't create $APP_DIR."
echo %s | base64 -d > compose.yaml
REV=$(sha256sum compose.yaml | cut -c1-7)
echo "==> Wrote compose.yaml ($REV)"
echo "%scommit=$REV"
echo "%smessage=App configuration $REV"
`, base64.StdEncoding.EncodeToString([]byte(compose)), markerPrefix, markerPrefix)
}

// gitSource clones or updates the repository into $APP_DIR and cds there.
func gitSource(s Spec) string {
	return fmt.Sprintf(`REPO=%s
BRANCH=%s
command -v git >/dev/null || fail "git is not installed on the server. Run the 'Install essential tools' setup step."
%s
if [ -d "$APP_DIR/.git" ]; then
  echo "==> Updating $APP_DIR ($BRANCH)"
  cd "$APP_DIR"
  git remote set-url origin "$REPO"
  git fetch --depth 1 origin "$BRANCH"
  git reset --hard FETCH_HEAD
else
  echo "==> Cloning $REPO ($BRANCH)"
  mkdir -p "$(dirname "$APP_DIR")"
  git clone --depth 1 --branch "$BRANCH" "$REPO" "$APP_DIR"
  cd "$APP_DIR"
fi
%s
REV=$(git rev-parse --short HEAD)
echo "%scommit=$REV"
echo "%ssha=$(git rev-parse HEAD)"
echo "%smessage=$(git log -1 --pretty=%%s)"
`, q(s.RepoURL), q(s.Branch), accessCheck, pinCommit(s.Commit), markerPrefix, markerPrefix, markerPrefix)
}

func ensureNetwork(n string) string {
	if n == "" {
		return ""
	}
	return fmt.Sprintf("docker network inspect %[1]s >/dev/null 2>&1 || docker network create %[1]s >/dev/null\n", q(n))
}

func networkFlag(n string) string {
	if n == "" {
		return ""
	}
	return "--network " + q(n) + " "
}

// composeNetworkOverride writes a compose override that attaches every
// service to the private network, leaving the repo's compose file untouched.
// It sets $OVR to the override's path, or leaves it empty when a service uses
// network_mode (which can't be combined with networks).
func composeNetworkOverride(n string) string {
	return ensureNetwork(n) + fmt.Sprintf(`if docker compose -f "$CF" config 2>/dev/null | grep -q 'network_mode:'; then
  echo "==> Note: a service uses network_mode, so it can't join the database network automatically."
else
  OVR=".truedeploy.compose.yml"
  {
    echo "# Generated by TrueDeploy on every deploy: joins all services to the private database network."
    echo "services:"
    docker compose -f "$CF" config --services | while read -r svc; do
      printf '  %%s:\n    networks:\n      default: {}\n      %[1]s: {}\n' "$svc"
    done
    printf 'networks:\n  %[1]s:\n    external: true\n'
  } > "$OVR"
  echo "==> Joined the private database network"
fi
`, n)
}

// cpuString formats a CPU count without trailing zeros (1.50 -> 1.5).
func cpuString(c float64) string { return strconv.FormatFloat(c, 'f', -1, 64) }

// limitFlags are docker run flags for the resource limits. Swap is capped at
// the same value so a limited app can't spill into swap and slow the server.
func limitFlags(memMB int, cpus float64) string {
	f := ""
	if memMB > 0 {
		f += fmt.Sprintf("--memory %[1]dm --memory-swap %[1]dm ", memMB)
	}
	if cpus > 0 {
		f += "--cpus " + cpuString(cpus) + " "
	}
	return f
}

// composeLimitsOverride writes a compose override that gives every service
// the limits, and sets $LIM to its path. Without limits $LIM stays empty, so
// removing them recreates the containers without limits.
func composeLimitsOverride(memMB int, cpus float64) string {
	if memMB <= 0 && cpus <= 0 {
		return "LIM=\"\"\n"
	}
	lim := ""
	if cpus > 0 {
		lim += `          cpus: \"` + cpuString(cpus) + `\"\n`
	}
	if memMB > 0 {
		lim += fmt.Sprintf(`          memory: %dM\n`, memMB)
	}
	swap := ""
	if memMB > 0 {
		swap = fmt.Sprintf(`    memswap_limit: %dM\n`, memMB)
	}
	return fmt.Sprintf(`LIM=".truedeploy.limits.yml"
{
  echo "# Generated by TrueDeploy on every deploy: resource limits for each service."
  echo "services:"
  docker compose -f "$CF" config --services | while read -r svc; do
    printf '  %%s:\n%[2]s    deploy:\n      resources:\n        limits:\n%[1]s' "$svc"
  done
} > "$LIM"
`, lim, swap)
}

// limitsNote warns when the server's Docker can't enforce the limits
// (rootless Docker without cgroup v2 delegation ignores them).
func limitsNote(memMB int, cpus float64) string {
	if memMB <= 0 && cpus <= 0 {
		return ""
	}
	return `[ "$(docker info --format '{{.MemoryLimit}}' 2>/dev/null)" = "false" ] && echo "==> Note: this server's Docker can't enforce memory limits (rootless Docker needs cgroup v2 delegation)."
[ "$(docker info --format '{{.CPUCfsQuota}}' 2>/dev/null)" = "false" ] && echo "==> Note: this server's Docker can't enforce CPU limits."
true
`
}

// pinCommit checks out an exact commit after the branch was fetched.
func pinCommit(sha string) string {
	if sha == "" {
		return ""
	}
	return fmt.Sprintf(`echo "==> Rolling back to %[1]s"
git fetch --depth 1 origin %[1]s || fail "Commit %[1]s is no longer in the repository (was the history rewritten?)."
git reset --hard FETCH_HEAD`, sha)
}

func envFile(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	return b.String()
}

// StatusScript lists the project's containers as tab-separated
// name, state, status, ports lines.
func StatusScript(slug string) string {
	return prelude(slug) + fmt.Sprintf(`docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un)."
{
  docker ps -a --filter label=com.docker.compose.project=%[1]s --format '{{.Names}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
  docker ps -a --filter label=truedeploy.project=%[1]s --format '{{.Names}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
} | sort -u
# Live usage of the running ones: stat, name, CPU %%, memory used / limit, memory %%.
RUN=$( { docker ps -q --filter label=com.docker.compose.project=%[1]s; docker ps -q --filter label=truedeploy.project=%[1]s; } | sort -u)
[ -n "$RUN" ] && docker stats --no-stream --format 'stat\t{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}' $RUN 2>/dev/null
true`, q(slug))
}

// StopScript stops and removes the project's containers (data volumes are kept).
func StopScript(s Spec) string {
	if s.Runtime == RuntimeDockerfile {
		return prelude(s.Slug) + fmt.Sprintf(`docker rm -f %s && echo "Stopped."`, q(containerName(s.Slug)))
	}
	return prelude(s.Slug) + fmt.Sprintf(`cd "%s" || fail "Project folder not found: nothing to stop."
%sdocker compose -p %s -f "$CF" down --remove-orphans
echo "Stopped (volumes kept)."`, AppDir(s.Slug), composeDetect(s.ComposeFile), q(s.Slug))
}

// LogsScript prints the project's container logs, following them if follow is set.
func LogsScript(s Spec, tail int, follow bool) string {
	return LogsSinceScript(s, tail, "", follow)
}

// LogRanges are the accepted values for LogsSinceScript's since ("" = any time).
var LogRanges = map[string]bool{"": true, "15m": true, "1h": true, "6h": true, "24h": true, "168h": true}

// LogsSinceScript prints (or follows) the last tail lines, limited to the
// last `since` (one of LogRanges) when set.
func LogsSinceScript(s Spec, tail int, since string, follow bool) string {
	f := ""
	if follow {
		f = " -f"
	}
	if since != "" && LogRanges[since] {
		f = " --since " + since + f
	}
	if s.Runtime == RuntimeDockerfile {
		return prelude(s.Slug) + fmt.Sprintf(`docker logs --tail %d --timestamps%s %s 2>&1`, tail, f, q(containerName(s.Slug)))
	}
	return prelude(s.Slug) + fmt.Sprintf(`cd "%s" || fail "The project isn't deployed on this server yet."
%sdocker compose -p %s -f "$CF" logs --no-color --tail %d --timestamps%s 2>&1`,
		AppDir(s.Slug), composeDetect(s.ComposeFile), q(s.Slug), tail, f)
}

// ParseMarker extracts key/value from a marker line.
func ParseMarker(line string) (key, value string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), markerPrefix)
	if !found {
		return "", "", false
	}
	return strings.Cut(rest, "=")
}

// ProbeScript shallow-clones a branch into a temporary folder and prints
// "F\t<path>" for every file and "C\t<path>\t<base64>" for files whose name
// is in keyNames (up to 64 KiB each), then removes the clone. It uses the
// same git credentials as deploys.
func ProbeScript(slug, repo, branch string, keyNames []string) string {
	var pattern strings.Builder
	for i, n := range keyNames {
		if i > 0 {
			pattern.WriteString("|")
		}
		pattern.WriteString(n)
	}
	return prelude(slug) + fmt.Sprintf(`REPO=%s
BRANCH=%s
command -v git >/dev/null || fail "git is not installed on the server. Run the 'Install essential tools' setup step."
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
if ! OUT=$(git clone -q --depth 1 --branch "$BRANCH" "$REPO" "$TMP/r" 2>&1); then
  if echo "$OUT" | grep -qiE "permission denied|could not read|authentication failed|repository not found"; then
    fail "The server can't read this repository yet. Save the project and add its deploy key, or connect GitHub in Settings."
  fi
  fail "$(echo "$OUT" | tail -n 1)"
fi
cd "$TMP/r"
git ls-files | head -n 20000 | while IFS= read -r f; do
  printf 'F\t%%s\n' "$f"
  case "$(basename "$f")" in
    %s) [ "$(wc -c < "$f")" -le 65536 ] && printf 'C\t%%s\t%%s\n' "$f" "$(base64 < "$f" | tr -d '\n')" ;;
  esac
done`, q(repo), q(branch), pattern.String())
}

// ComposeDetect sets $CF to the project's compose file (needs fail()).
func ComposeDetect(file string) string { return composeDetect(file) }

// ContainerName is the container of a Dockerfile-runtime project.
func ContainerName(slug string) string { return containerName(slug) }
