// Package proxy manages one Caddy reverse proxy per server. Caddy runs as a
// Docker container with host networking, terminates HTTPS with automatic
// Let's Encrypt certificates, and forwards each domain to localhost:<port>.
//
// Host networking matters: with a bridged container, traffic to apps on the
// host passes ufw's INPUT chain and is silently dropped on firewalled servers.
package proxy

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	ContainerName = "truedeploy-caddy"
	Image         = "caddy:2"
	// ConfigDir is mounted at /etc/caddy in the container.
	ConfigDir = "$HOME/truedeploy/caddy"
)

// Site routes one or more routes to a local port. A route is a domain
// ("example.com") or a domain plus path prefix ("example.com/api"), so a
// frontend and a backend from different repositories can share one domain.
type Site struct {
	Project string
	Domains []string // routes
	Port    int
	// StripPath removes the route's path prefix before forwarding
	// (/api/users reaches the app as /users). Default: forwarded as is.
	StripPath bool
	// AuthUser and AuthHash (bcrypt) put a password in front of the site.
	AuthUser, AuthHash string
	// Maintenance serves a "back soon" page (503) instead of the app.
	Maintenance        bool
	MaintenanceMessage string
	// Redirects are extra hosts (www.example.com, an old domain) that send
	// visitors permanently to the first domain, keeping the path.
	Redirects []string
}

// RedirectTarget is the host a site's redirects point to: its first domain.
func (s Site) RedirectTarget() string {
	if len(s.Domains) == 0 {
		return ""
	}
	h, _ := SplitRoute(s.Domains[0])
	return h
}

// WWWAlternate suggests the matching www/apex host for a domain:
// example.com -> www.example.com, www.example.com -> example.com.
func WWWAlternate(route string) string {
	h, _ := SplitRoute(route)
	if rest, ok := strings.CutPrefix(h, "www."); ok {
		return rest
	}
	return "www." + h
}

var (
	domainRe = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	pathRe   = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
)

// NormalizeDomain cleans a route: lowercases the host, strips the scheme,
// query, trailing dot and trailing slash, and keeps a path prefix.
func NormalizeDomain(d string) string {
	d = strings.TrimSpace(d)
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "?#"); i >= 0 {
		d = d[:i]
	}
	host, path, _ := strings.Cut(d, "/")
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	path = strings.Trim(path, "/")
	if path == "" {
		return host
	}
	return host + "/" + path
}

// SplitRoute returns a route's host and path prefix ("" or "/api").
func SplitRoute(r string) (host, path string) {
	host, path, found := strings.Cut(r, "/")
	if found {
		path = "/" + path
	}
	return host, path
}

func ValidDomain(d string) bool {
	return len(d) <= 253 && domainRe.MatchString(d)
}

// ValidRoute checks a domain with an optional path prefix.
func ValidRoute(r string) bool {
	host, path := SplitRoute(r)
	if !ValidDomain(host) {
		return false
	}
	if path == "" {
		return true
	}
	if len(path) > 200 || !pathRe.MatchString(path) {
		return false
	}
	for _, seg := range strings.Split(path[1:], "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Validate checks that sites are well-formed and no route is used twice.
func Validate(sites []Site) error {
	seen := map[string]string{}
	for _, s := range sites {
		if s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("%s: the app port must be between 1 and 65535", s.Project)
		}
		if err := checkAccess(s); err != nil {
			return err
		}
		for _, d := range s.Domains {
			if !ValidRoute(d) {
				return fmt.Errorf("%q is not a valid domain (use example.com or example.com/api)", d)
			}
			if other, ok := seen[d]; ok && other != s.Project {
				return fmt.Errorf("%s is used by both %s and %s", d, other, s.Project)
			}
			seen[d] = s.Project
		}
	}
	// A redirect host can't also serve a site or redirect for another project.
	hosts := map[string]string{}
	for _, s := range sites {
		for _, d := range s.Domains {
			h, _ := SplitRoute(d)
			hosts[h] = s.Project
		}
	}
	redirs := map[string]string{}
	for _, s := range sites {
		if len(s.Redirects) > 0 && len(s.Domains) == 0 {
			return fmt.Errorf("%s: redirects need a domain to send visitors to", s.Project)
		}
		for _, r := range s.Redirects {
			if !ValidDomain(r) {
				return fmt.Errorf("%q is not a valid domain to redirect (use www.example.com, without a path)", r)
			}
			if other, ok := hosts[r]; ok {
				if other == s.Project {
					return fmt.Errorf("%s is already one of %s's domains, so it can't also redirect", r, s.Project)
				}
				return fmt.Errorf("%s is served by %s, so it can't redirect to %s", r, other, s.Project)
			}
			if other, ok := redirs[r]; ok && other != s.Project {
				return fmt.Errorf("%s redirects for both %s and %s", r, other, s.Project)
			}
			redirs[r] = s.Project
		}
	}
	return nil
}

type route struct {
	path  string
	site  Site
	order int
}

// Caddyfile renders the full config for a server's sites. Hosts that only
// have one whole-domain route keep the simple form; hosts shared by path
// routes get one block with the longest paths matched first.
func Caddyfile(sites []Site) string {
	hosts := map[string][]route{}
	for _, s := range sites {
		for _, d := range s.Domains {
			h, p := SplitRoute(d)
			hosts[h] = append(hosts[h], route{path: p, site: s})
		}
	}

	// Simple hosts (one whole-domain route) are grouped per project, as before.
	simple := map[string][]string{}
	var simpleSites []Site
	var shared []string
	for h, rs := range hosts {
		if len(rs) == 1 && rs[0].path == "" {
			name := rs[0].site.Project
			if _, ok := simple[name]; !ok {
				simpleSites = append(simpleSites, rs[0].site)
			}
			simple[name] = append(simple[name], h)
		} else {
			shared = append(shared, h)
		}
	}
	sort.Slice(simpleSites, func(i, j int) bool { return simpleSites[i].Project < simpleSites[j].Project })
	sort.Strings(shared)

	var b strings.Builder
	b.WriteString("# Managed by TrueDeploy. Changes here are overwritten on the next apply.\n")
	if len(simpleSites) == 0 && len(shared) == 0 {
		b.WriteString("# No domains configured.\n")
	}
	for _, s := range simpleSites {
		ds := simple[s.Project]
		sort.Strings(ds)
		fmt.Fprintf(&b, "\n# %s\n%s {\n\tencode zstd gzip\n%s}\n", s.Project, strings.Join(ds, ", "), siteBody(s, "\t"))
	}
	for _, h := range shared {
		rs := hosts[h]
		sort.SliceStable(rs, func(i, j int) bool {
			if len(rs[i].path) != len(rs[j].path) {
				return len(rs[i].path) > len(rs[j].path) // most specific first
			}
			return rs[i].path < rs[j].path
		})
		fmt.Fprintf(&b, "\n%s {\n\tencode zstd gzip\n", h)
		hasRoot := false
		for i, r := range rs {
			if r.path == "" {
				hasRoot = true
				fmt.Fprintf(&b, "\n\t# %s\n\thandle {\n%s\t}\n", r.site.Project, siteBody(r.site, "\t\t"))
				continue
			}
			fmt.Fprintf(&b, "\n\t# %s\n\t@r%d path %s %s/*\n\thandle @r%d {\n", r.site.Project, i, r.path, r.path, i)
			if r.site.StripPath {
				fmt.Fprintf(&b, "\t\turi strip_prefix %s\n", r.path)
			}
			fmt.Fprintf(&b, "%s\t}\n", siteBody(r.site, "\t\t"))
		}
		if !hasRoot {
			b.WriteString("\n\thandle {\n\t\trespond \"Not found\" 404\n\t}\n")
		}
		b.WriteString("}\n")
	}
	redirSites := slices.Clone(sites)
	sort.SliceStable(redirSites, func(i, j int) bool { return redirSites[i].Project < redirSites[j].Project })
	for _, s := range redirSites {
		if len(s.Redirects) == 0 || s.RedirectTarget() == "" {
			continue
		}
		rs := slices.Clone(s.Redirects)
		sort.Strings(rs)
		// 308 keeps the method (a POST stays a POST); browsers cache it like a 301.
		fmt.Fprintf(&b, "\n# %s (redirects)\n%s {\n\tredir https://%s{uri} 308\n}\n", s.Project, strings.Join(rs, ", "), s.RedirectTarget())
	}
	return b.String()
}

// prelude finds a Docker that can bind ports 80/443 (system Docker, not rootless).
const prelude = `fail() { echo "$*" >&2; exit 1; }
command -v docker >/dev/null || fail "Docker is not installed. Run the Docker step in server Set up first."
docker info >/dev/null 2>&1 || fail "Can't reach Docker as $(id -un). Domains need the system Docker: ask your admin to add you to the docker group."
if docker info --format '{{.SecurityOptions}}' 2>/dev/null | grep -q rootless; then
  fail "This is rootless Docker, which can't use ports 80 and 443. Domains need the system Docker (ask your admin to add you to the docker group)."
fi
`

// ApplyScript writes the Caddyfile and starts or reloads Caddy.
func ApplyScript(caddyfile string) string {
	return prelude + fmt.Sprintf(`DIR="%[1]s"
mkdir -p "$DIR"
echo %[2]s | base64 -d > "$DIR/Caddyfile.new"

if docker ps --format '{{.Names}}' | grep -qx %[3]s; then
  # Validate before swapping so a bad config never replaces a working one.
  docker exec %[3]s caddy validate --config /etc/caddy/Caddyfile.new --adapter caddyfile >/dev/null 2>&1 \
    || { docker exec %[3]s caddy validate --config /etc/caddy/Caddyfile.new --adapter caddyfile 2>&1 | tail -n 5 >&2; rm -f "$DIR/Caddyfile.new"; fail "The proxy config is invalid; nothing was changed."; }
  cat "$DIR/Caddyfile.new" > "$DIR/Caddyfile"
  rm -f "$DIR/Caddyfile.new"
  docker exec %[3]s caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
  echo "Proxy reloaded."
else
  cat "$DIR/Caddyfile.new" > "$DIR/Caddyfile"
  rm -f "$DIR/Caddyfile.new"
  docker rm -f %[3]s >/dev/null 2>&1 || true
  for p in 80 443; do
    if command -v ss >/dev/null && ss -Hltn "sport = :$p" | grep -q .; then
      who=$(ss -Hltnp "sport = :$p" 2>/dev/null | grep -o 'users:(("[^"]*' | head -n1 | cut -d'"' -f2)
      fail "Port $p is already in use${who:+ by $who}. Stop that service (for example nginx or apache) so TrueDeploy's proxy can serve HTTPS."
    fi
  done
  echo "==> Starting the HTTPS proxy (Caddy)"
  docker run -d --name %[3]s --network host --restart unless-stopped \
    -v "$DIR:/etc/caddy" -v truedeploy_caddy_data:/data -v truedeploy_caddy_config:/config \
    %[4]s >/dev/null
  sleep 2
  docker ps --format '{{.Names}}' | grep -qx %[3]s || { docker logs --tail 20 %[3]s >&2; fail "The proxy didn't start (see output above)."; }
  echo "Proxy started."
fi`, ConfigDir, base64.StdEncoding.EncodeToString([]byte(caddyfile)), ContainerName, Image)
}

// StatusScript prints "running" or "stopped" and the Caddy version if running.
func StatusScript() string {
	return `if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx ` + ContainerName + `; then
  echo "running $(docker exec ` + ContainerName + ` caddy version 2>/dev/null | cut -d' ' -f1)"
else
  echo stopped
fi`
}

// ErrNoPort explains why a compose project needs an explicit port for domains.
var ErrNoPort = errors.New("set the app port (the port your compose file publishes on the server) to use domains")

var (
	authUserRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
	bcryptRe   = regexp.MustCompile(`^\$2[aby]\$\d\d\$[./A-Za-z0-9]{53}$`)
)

// checkAccess validates the password and maintenance settings of a site.
func checkAccess(s Site) error {
	if s.AuthUser != "" || s.AuthHash != "" {
		if !authUserRe.MatchString(s.AuthUser) {
			return fmt.Errorf("%s: the username may only use letters, digits and . _ @ -", s.Project)
		}
		if !bcryptRe.MatchString(s.AuthHash) {
			return fmt.Errorf("%s: the stored password is invalid; set it again", s.Project)
		}
	}
	if len(s.MaintenanceMessage) > 500 {
		return fmt.Errorf("%s: keep the maintenance message under 500 characters", s.Project)
	}
	return nil
}

// siteBody is what serves one route: an optional password, then either the
// maintenance page or the app.
func siteBody(s Site, in string) string {
	var b strings.Builder
	if s.AuthUser != "" {
		fmt.Fprintf(&b, "%sbasic_auth {\n%s\t%s %s\n%s}\n", in, in, s.AuthUser, s.AuthHash, in)
	}
	if s.Maintenance {
		fmt.Fprintf(&b, "%sheader Content-Type \"text/html; charset=utf-8\"\n%sheader Retry-After 600\n%srespond <<HTML\n%s\nHTML 503\n",
			in, in, in, MaintenancePage(s.MaintenanceMessage))
		return b.String()
	}
	fmt.Fprintf(&b, "%sreverse_proxy localhost:%d\n", in, s.Port)
	return b.String()
}

// MaintenancePage is the "back soon" page, on one line so the heredoc can't
// be ended early; the message is HTML-escaped.
func MaintenancePage(message string) string {
	if strings.TrimSpace(message) == "" {
		message = "We're making some improvements and will be back shortly."
	}
	msg := strings.ReplaceAll(html.EscapeString(strings.TrimSpace(message)), "\n", "<br>")
	msg = strings.ReplaceAll(msg, "\r", "")
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta name="robots" content="noindex"><title>Back soon</title><style>` +
		`body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,-apple-system,Segoe UI,sans-serif;background:#f6f7f9;color:#1d2433}` +
		`@media (prefers-color-scheme:dark){body{background:#0b1020;color:#e6e9f0}p{color:#a3abbd}}` +
		`main{max-width:32rem;padding:2rem;text-align:center}h1{font-size:1.6rem;margin:0 0 .5rem}p{color:#5b6475;margin:0}` +
		`</style></head><body><main><h1>Back soon</h1><p>` + msg + `</p></main></body></html>`
}
