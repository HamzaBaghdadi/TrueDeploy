// Package importer finds what already runs on a server set up without
// TrueDeploy (Compose projects, database containers, nginx sites) and
// builds the scripts that adopt them.
//
// Adopting a Compose project links ~/apps/<name> to its existing folder and
// keeps the Docker project name, so containers, volumes, bind-mounted
// folders and .env stay exactly where they are. Databases in standalone
// containers are copied into a TrueDeploy-managed database with a dump.
package importer

import (
	"encoding/base64"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ScanScript reads (never changes) the server and prints tab-separated
// records. Fields that may contain anything are base64-encoded.
const ScanScript = `export PATH="$HOME/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
b64() { base64 | tr -d '\n'; }
T=$(printf '\t')
if docker info >/dev/null 2>&1; then
  echo "D${T}ok"
  # Compose projects, from the labels Compose puts on its containers.
  docker ps -a --format '{{.Label "com.docker.compose.project"}}	{{.Label "com.docker.compose.project.working_dir"}}	{{.Label "com.docker.compose.project.config_files"}}' 2>/dev/null \
    | awk -F'\t' '$1 != ""' | sort -u | while IFS="$T" read -r name wd cfg; do
      remote=$(git -C "$wd" remote get-url origin 2>/dev/null || true)
      branch=$(git -C "$wd" rev-parse --abbrev-ref HEAD 2>/dev/null || true)
      dirty=$(git -C "$wd" status --porcelain --untracked-files=no 2>/dev/null | wc -l | tr -d ' ')
      w=0; [ -w "$wd" ] && w=1
      envb=""; [ -r "$wd/.env" ] && envb=$(head -c 65536 "$wd/.env" | b64)
      printf 'C\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$wd" "$cfg" "$remote" "$branch" "$dirty" "$w" "$envb"
      ids=$(docker ps -aq --filter "label=com.docker.compose.project=$name")
      docker ps -a --filter "label=com.docker.compose.project=$name" --format 'P	'"$name"'	{{.Names}}	{{.State}}	{{.Ports}}'
      [ -n "$ids" ] && docker inspect -f '{{range .Mounts}}{{if eq .Type "bind"}}{{.Source}}{{"\n"}}{{end}}{{end}}' $ids 2>/dev/null \
        | sort -u | awk -v p="$name" 'NF { print "B\t" p "\t" $0 }'
    done
  # Containers outside Compose (docker run), except TrueDeploy's own.
  docker ps -a --format '{{.Names}}	{{.Image}}	{{.State}}	{{.Ports}}	{{.Label "com.docker.compose.project"}}' \
    | awk -F'\t' '$5 == "" && $1 !~ /^(td-|truedeploy-)/ { print "X\t" $1 "\t" $2 "\t" $3 "\t" $4 }' | while IFS="$T" read -r tag name image state ports; do
      printf 'X\t%s\t%s\t%s\t%s\n' "$name" "$image" "$state" "$ports"
      case "$image" in
        *postgres*|*mysql*|*mariadb*|*mongo*)
          printf 'E\t%s\t%s\n' "$name" "$(docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$name" | b64)" ;;
      esac
    done
else
  echo "D${T}none"
fi
# nginx sites (server_name and the local ports they forward to).
for f in /etc/nginx/sites-enabled/* /etc/nginx/conf.d/*.conf; do
  [ -r "$f" ] || continue
  names=$(grep -hoE '^\s*server_name\s+[^;]+' "$f" | sed -E 's/^\s*server_name\s+//' | tr '\n' ' ')
  pass=$(grep -hoE 'proxy_pass\s+[^;]+' "$f" | sed -E 's/proxy_pass\s+//' | tr '\n' ' ')
  printf 'N\t%s\t%s\t%s\n' "$f" "$names" "$pass"
done
# Who holds the web ports.
if command -v ss >/dev/null; then
  ss -Hltnp 2>/dev/null | awk '{ print $4 "\t" $6 }' | while IFS="$T" read -r addr proc; do
    port=${addr##*:}
    case "$port" in 80|443)
      p=$(echo "$proc" | grep -o 'users:(("[^"]*' | head -n1 | cut -d'"' -f2)
      printf 'L\t%s\t%s\n' "$port" "$p" ;;
    esac
  done
fi
for s in nginx apache2 httpd caddy traefik postgresql mysql mariadb; do
  st=$(systemctl is-active "$s" 2>/dev/null || true)
  [ "$st" = active ] && printf 'S\t%s\n' "$s"
done
true`

type Container struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Ports []int  `json:"ports"`
}

type ComposeProject struct {
	Name        string            `json:"name"`
	WorkDir     string            `json:"workDir"`
	ConfigFiles []string          `json:"configFiles"`
	RepoURL     string            `json:"repoUrl"`
	Branch      string            `json:"branch"`
	Dirty       int               `json:"dirty"` // changed tracked files (a deploy resets them)
	Writable    bool              `json:"writable"`
	Env         map[string]string `json:"env"`
	Containers  []Container       `json:"containers"`
	Ports       []int             `json:"ports"` // published host ports
	// BindMounts outside the project folder keep working; listed for info.
	BindMounts []string `json:"bindMounts"`
	// SuggestedDomains come from nginx sites forwarding to one of Ports.
	SuggestedDomains []string `json:"suggestedDomains"`
	SuggestedPort    int      `json:"suggestedPort,omitempty"`
}

// ComposeFile is the compose file to use, relative to WorkDir ("" = auto).
func (c ComposeProject) ComposeFile() string {
	if len(c.ConfigFiles) == 0 {
		return ""
	}
	f := c.ConfigFiles[0]
	if rel, ok := strings.CutPrefix(f, strings.TrimSuffix(c.WorkDir, "/")+"/"); ok {
		f = rel
	}
	switch f {
	case "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return ""
	}
	return f
}

// Problems explain why (or how) a project can't be adopted as is.
func (c ComposeProject) Problems(validName func(string) bool) []string {
	var out []string
	if c.RepoURL == "" {
		out = append(out, "The folder isn't a Git clone with an origin remote. Push it to a repository first, then scan again.")
	}
	if !c.Writable {
		out = append(out, "Your SSH user can't write to "+c.WorkDir+", so deploys can't update it. Log in as its owner, or change the folder's owner.")
	}
	if !validName(c.Name) {
		out = append(out, "The Compose project name \""+c.Name+"\" can't be used as a TrueDeploy project folder name.")
	}
	if len(c.ConfigFiles) > 1 {
		out = append(out, "It uses several compose files; only "+path.Base(c.ConfigFiles[0])+" will be used.")
	}
	return out
}

type Database struct {
	Container string `json:"container"`
	Image     string `json:"image"`
	State     string `json:"state"`
	Engine    string `json:"engine"`  // postgres, mysql, mongo
	Version   string `json:"version"` // from the image tag, e.g. "16"
	MariaDB   bool   `json:"mariadb"`
	User      string `json:"user"`
	Database  string `json:"database"`

	password string
}

type Site struct {
	File        string   `json:"file"`
	ServerNames []string `json:"serverNames"`
	Ports       []int    `json:"ports"` // local ports it forwards to
}

type Listener struct {
	Port    int    `json:"port"`
	Process string `json:"process"`
}

type Scan struct {
	Docker     bool              `json:"docker"`
	Compose    []ComposeProject  `json:"compose"`
	Databases  []Database        `json:"databases"`
	Containers []Container       `json:"containers"` // other standalone containers
	Images     map[string]string `json:"images"`     // container -> image, for standalone ones
	Sites      []Site            `json:"sites"`
	Listeners  []Listener        `json:"listeners"`
	Services   []string          `json:"services"` // active host services (nginx, postgresql, ...)
}

var hostPortRe = regexp.MustCompile(`:(\d+)->`)

func hostPorts(s string) []int {
	seen := map[int]bool{}
	var out []int
	for _, m := range hostPortRe.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}

var proxyPortRe = regexp.MustCompile(`^https?://(?:127\.0\.0\.1|localhost|0\.0\.0\.0|\[::1\]):(\d+)`)

func decode(s string) string {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseEnv reads KEY=VALUE lines (as in .env files or container env).
func ParseEnv(text string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

// Parse reads ScanScript's output.
func Parse(lines []string) Scan {
	sc := Scan{Images: map[string]string{}}
	byName := map[string]*ComposeProject{}
	envs := map[string]map[string]string{}
	var order []string
	for _, l := range lines {
		f := strings.Split(strings.TrimRight(l, "\r"), "\t")
		switch f[0] {
		case "D":
			sc.Docker = len(f) > 1 && f[1] == "ok"
		case "C":
			if len(f) < 9 {
				continue
			}
			dirty, _ := strconv.Atoi(f[6])
			c := &ComposeProject{Name: f[1], WorkDir: f[2], RepoURL: f[4], Branch: f[5], Dirty: dirty, Writable: f[7] == "1",
				Env: ParseEnv(decode(f[8])), Containers: []Container{}, BindMounts: []string{}, SuggestedDomains: []string{}, Ports: []int{}}
			for _, cf := range strings.Split(f[3], ",") {
				if cf = strings.TrimSpace(cf); cf != "" {
					c.ConfigFiles = append(c.ConfigFiles, cf)
				}
			}
			if c.Branch == "HEAD" {
				c.Branch = ""
			}
			if _, ok := byName[c.Name]; !ok {
				order = append(order, c.Name)
			}
			byName[c.Name] = c
		case "P":
			if c := byName[at(f, 1)]; c != nil {
				ports := hostPorts(at(f, 4))
				c.Containers = append(c.Containers, Container{Name: at(f, 2), State: at(f, 3), Ports: ports})
				for _, p := range ports {
					if !contains(c.Ports, p) {
						c.Ports = append(c.Ports, p)
					}
				}
				sort.Ints(c.Ports)
			}
		case "B":
			if c := byName[at(f, 1)]; c != nil && at(f, 2) != "" {
				c.BindMounts = append(c.BindMounts, at(f, 2))
			}
		case "X":
			sc.Containers = append(sc.Containers, Container{Name: at(f, 1), State: at(f, 3), Ports: hostPorts(at(f, 4))})
			sc.Images[at(f, 1)] = at(f, 2)
		case "E":
			envs[at(f, 1)] = ParseEnv(decode(at(f, 2)))
		case "N":
			s := Site{File: at(f, 1), ServerNames: []string{}, Ports: []int{}}
			for _, n := range strings.Fields(at(f, 2)) {
				if n != "_" && n != "localhost" && !contains(s.ServerNames, n) {
					s.ServerNames = append(s.ServerNames, n)
				}
			}
			for _, p := range strings.Fields(at(f, 3)) {
				if m := proxyPortRe.FindStringSubmatch(p); m != nil {
					n, _ := strconv.Atoi(m[1])
					if !contains(s.Ports, n) {
						s.Ports = append(s.Ports, n)
					}
				}
			}
			sc.Sites = append(sc.Sites, s)
		case "L":
			n, _ := strconv.Atoi(at(f, 1))
			sc.Listeners = append(sc.Listeners, Listener{Port: n, Process: at(f, 2)})
		case "S":
			sc.Services = append(sc.Services, at(f, 1))
		}
	}

	// Databases among standalone containers; the rest stay "other".
	var others []Container
	for _, c := range sc.Containers {
		if db, ok := database(c, sc.Images[c.Name], envs[c.Name]); ok {
			sc.Databases = append(sc.Databases, db)
		} else {
			others = append(others, c)
		}
	}
	sc.Containers = others

	for _, n := range order {
		c := byName[n]
		for _, s := range sc.Sites {
			for _, p := range s.Ports {
				if contains(c.Ports, p) {
					if c.SuggestedPort == 0 {
						c.SuggestedPort = p
					}
					for _, d := range s.ServerNames {
						if p == c.SuggestedPort && !contains(c.SuggestedDomains, d) {
							c.SuggestedDomains = append(c.SuggestedDomains, d)
						}
					}
				}
			}
		}
		if c.SuggestedPort == 0 && len(c.Ports) == 1 {
			c.SuggestedPort = c.Ports[0]
		}
		sc.Compose = append(sc.Compose, *c)
	}
	sort.Slice(sc.Compose, func(i, j int) bool { return sc.Compose[i].Name < sc.Compose[j].Name })
	return sc
}

var tagMajorRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)`)

func database(c Container, image string, env map[string]string) (Database, bool) {
	repo, tag, _ := strings.Cut(image, ":")
	base := path.Base(repo)
	db := Database{Container: c.Name, Image: image, State: c.State}
	if m := tagMajorRe.FindStringSubmatch(tag); m != nil {
		db.Version = m[1]
	}
	switch base {
	case "postgres", "postgis":
		db.Engine = "postgres"
		db.User = orDefault(env["POSTGRES_USER"], "postgres")
		db.Database = orDefault(env["POSTGRES_DB"], db.User)
		db.password = env["POSTGRES_PASSWORD"]
		if i := strings.Index(db.Version, "."); i > 0 {
			db.Version = db.Version[:i]
		}
	case "mysql", "mariadb":
		db.Engine, db.MariaDB = "mysql", base == "mariadb"
		db.User = "root"
		db.password = orDefault(env["MYSQL_ROOT_PASSWORD"], env["MARIADB_ROOT_PASSWORD"])
		db.Database = orDefault(env["MYSQL_DATABASE"], env["MARIADB_DATABASE"])
	case "mongo":
		db.Engine = "mongo"
		db.User = env["MONGO_INITDB_ROOT_USERNAME"]
		db.password = env["MONGO_INITDB_ROOT_PASSWORD"]
		db.Database = env["MONGO_INITDB_DATABASE"]
		if i := strings.Index(db.Version, "."); i > 0 {
			db.Version = db.Version[:i]
		}
	default:
		return db, false
	}
	return db, true
}

// HasPassword reports whether the container's environment had the password.
func (d Database) HasPassword() bool { return d.password != "" }

// WithPassword is used when the password can't be read from the container.
func (d Database) WithPassword(pw string) Database { d.password = pw; return d }

func at(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func contains[T comparable](s []T, v T) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// AdoptScript links ~/apps/<slug> to a Compose project's existing folder.
// Running it again for the same folder is fine.
func AdoptScript(slug, workDir string) string {
	return fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
W=%[1]s
A="$HOME/apps/"%[2]s
[ -d "$W" ] || fail "The folder $W no longer exists."
[ -w "$W" ] || fail "$(id -un) can't write to $W."
[ -d "$W/.git" ] || fail "$W isn't a Git clone."
mkdir -p "$HOME/apps"
if [ -L "$A" ] || [ -e "$A" ]; then
  [ "$A" -ef "$W" ] || fail "$A already exists and is a different folder. Rename the project or move that folder."
  echo "Already linked: $A -> $W"
else
  ln -s "$W" "$A"
  echo "Linked $A -> $W"
fi`, q(workDir), q(slug))
}

// CopyTarget is the TrueDeploy database the data goes into.
type CopyTarget struct {
	Container string
	User      string
	Database  string
	Password  string
}

// CopyScript dumps a standalone database container into a TrueDeploy one.
// The old container is only read.
func CopyScript(src Database, dst CopyTarget) (string, error) {
	if src.Database == "" && src.Engine != "postgres" {
		return "", fmt.Errorf("couldn't tell which database %s holds; it has no MYSQL_DATABASE / MONGO_INITDB_DATABASE", src.Container)
	}
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	head := fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
set -o pipefail
OPW=$(echo %s | base64 -d)
NPW=$(echo %s | base64 -d)
docker inspect %s >/dev/null 2>&1 || fail "Container %s no longer exists."
echo "==> Copying %s from %s"
`, enc(src.password), enc(dst.Password), q(src.Container), src.Container, orDefault(src.Database, "the database"), src.Container)
	var pipe string
	switch src.Engine {
	case "postgres":
		pipe = fmt.Sprintf(`docker exec -e PGPASSWORD="$OPW" %s pg_dump -U %s -d %s --no-owner --no-privileges \
  | docker exec -i -e PGPASSWORD="$NPW" %s psql -q -U %s -d %s -v ON_ERROR_STOP=1 >/dev/null`,
			q(src.Container), q(src.User), q(src.Database), q(dst.Container), q(dst.User), q(dst.Database))
	case "mysql":
		pipe = fmt.Sprintf(`docker exec -e MYSQL_PWD="$OPW" %s sh -c 'exec $(command -v mariadb-dump || command -v mysqldump) -u %s --single-transaction --routines --no-tablespaces %s' \
  | docker exec -i -e MYSQL_PWD="$NPW" %s mysql -u %s %s`,
			q(src.Container), src.User, src.Database, q(dst.Container), q(dst.User), q(dst.Database))
	case "mongo":
		auth := ""
		if src.User != "" {
			auth = fmt.Sprintf(` -u %s -p "$OPW" --authenticationDatabase admin`, q(src.User))
		}
		pipe = fmt.Sprintf(`docker exec %s mongodump --quiet --archive --db %s%s \
  | docker exec -i %s mongorestore --quiet --archive -u %s -p "$NPW" --authenticationDatabase admin --nsFrom %s --nsTo %s`,
			q(src.Container), q(src.Database), auth, q(dst.Container), q(dst.User), q(src.Database+".*"), q(dst.Database+".*"))
	default:
		return "", fmt.Errorf("copying %s databases isn't supported", src.Engine)
	}
	return head + pipe + ` || fail "Copying failed (see output above). The new database may be partly filled; the old one is unchanged."
echo "Copied. The old container ` + src.Container + ` is untouched: remove it once your apps use the new database."`, nil
}
