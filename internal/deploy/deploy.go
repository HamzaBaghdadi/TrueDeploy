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
}

// Markers are printed by scripts on their own line so the app can pick out
// facts (like the deployed commit) from the log.
const markerPrefix = "::td::"

var (
	slugStrip  = regexp.MustCompile(`[^a-z0-9-]+`)
	branchRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	envKeyRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	composeRe  = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	scpLikeRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._/~-]+$`)
	repoPathRe = regexp.MustCompile(`^/?([^/]+)/(.+?)(\.git)?/?$`)
)

// Slug turns a project name into a safe directory / compose project name.
func Slug(name string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// Validate checks a spec before it is saved or deployed.
func (s Spec) Validate() error {
	if s.Slug == "" || s.Slug != Slug(s.Slug) {
		return errors.New("project name must contain letters or digits")
	}
	if err := ValidateRepoURL(s.RepoURL); err != nil {
		return err
	}
	if !branchRe.MatchString(s.Branch) || strings.Contains(s.Branch, "..") || strings.HasPrefix(s.Branch, "-") {
		return fmt.Errorf("%q is not a valid branch name", s.Branch)
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
	default:
		return fmt.Errorf("unknown runtime %q", s.Runtime)
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

// prelude sets up git auth and finds Docker (system or rootless).
const prelude = `KEY="$HOME/.ssh/truedeploy_ed25519"
GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
if [ -f "$KEY" ]; then GIT_SSH_COMMAND="$GIT_SSH_COMMAND -i $KEY -o IdentitiesOnly=yes"; fi
export GIT_SSH_COMMAND
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export PATH="$HOME/bin:$PATH"
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
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
	return prelude + fmt.Sprintf(`REPO=%s
BRANCH=%s
command -v git >/dev/null || fail "git is not installed on the server. Run the 'Install essential tools' setup step."
`, q(s.RepoURL), q(s.Branch)) + accessCheck + `echo "Access OK: the server can read branch '$BRANCH'."`
}

// Script returns the deploy script for a spec. Call Validate first.
func Script(s Spec) string {
	var b strings.Builder
	b.WriteString(prelude)
	fmt.Fprintf(&b, `APP_DIR=%s
REPO=%s
BRANCH=%s
command -v git >/dev/null || fail "git is not installed on the server. Run the 'Install essential tools' setup step."
command -v docker >/dev/null || [ -x "$HOME/bin/docker" ] || fail "Docker is not installed. Run the Docker setup step first."
docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un). Add this user to the docker group or install rootless Docker (Set up)."
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
echo "%scommit=$(git rev-parse --short HEAD)"
echo "%smessage=$(git log -1 --pretty=%%s)"
`, `"`+AppDir(s.Slug)+`"`, q(s.RepoURL), q(s.Branch), accessCheck, markerPrefix, markerPrefix)

	// .env travels base64-encoded so values can't break out of the script.
	fmt.Fprintf(&b, `(umask 077; echo %s | base64 -d > .env)
echo "==> Wrote .env (%d variables)"
`, base64.StdEncoding.EncodeToString([]byte(envFile(s.Env))), len(s.Env))

	switch s.Runtime {
	case RuntimeCompose:
		b.WriteString(composeDetect(s.ComposeFile))
		fmt.Fprintf(&b, `echo "==> docker compose up ($CF)"
docker compose -p %[1]s -f "$CF" up -d --build --remove-orphans
docker compose -p %[1]s -f "$CF" ps
`, q(s.Slug))
	case RuntimeDockerfile:
		fmt.Fprintf(&b, `[ -f Dockerfile ] || fail "No Dockerfile in the repository root. Add one, or switch the project to Docker Compose."
IMG=%[1]s:$(git rev-parse --short HEAD)
echo "==> docker build $IMG"
docker build -t "$IMG" -t %[1]s:latest .
docker rm -f %[2]s >/dev/null 2>&1 || true
echo "==> Starting %[2]s on port %[3]d"
docker run -d --name %[2]s --label truedeploy.project=%[4]s --restart unless-stopped --env-file .env -p %[3]d:%[5]d "$IMG"
docker ps --filter name=^/%[2]s$
`, q("truedeploy/"+s.Slug), q(containerName(s.Slug)), s.HostPort, q(s.Slug), s.ContainerPort)
	}
	b.WriteString(`echo "==> Deployed $(git rev-parse --short HEAD)"`)
	return b.String()
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
	return prelude + fmt.Sprintf(`docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un)."
{
  docker ps -a --filter label=com.docker.compose.project=%[1]s --format '{{.Names}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
  docker ps -a --filter label=truedeploy.project=%[1]s --format '{{.Names}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'
} | sort -u`, q(slug))
}

// StopScript stops and removes the project's containers (data volumes are kept).
func StopScript(s Spec) string {
	if s.Runtime == RuntimeDockerfile {
		return prelude + fmt.Sprintf(`docker rm -f %s && echo "Stopped."`, q(containerName(s.Slug)))
	}
	return prelude + fmt.Sprintf(`cd "%s" || fail "Project folder not found: nothing to stop."
%sdocker compose -p %s -f "$CF" down --remove-orphans
echo "Stopped (volumes kept)."`, AppDir(s.Slug), composeDetect(s.ComposeFile), q(s.Slug))
}

// ParseMarker extracts key/value from a marker line.
func ParseMarker(line string) (key, value string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), markerPrefix)
	if !found {
		return "", "", false
	}
	return strings.Cut(rest, "=")
}
