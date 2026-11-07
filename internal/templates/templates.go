// Package templates is the catalog of one-click apps: ready-made compose
// files for popular self-hosted software. An app becomes a normal project
// whose compose file lives in TrueDeploy instead of a Git repository.
//
// Compose files read their settings from .env. Five variables are managed by
// TrueDeploy and kept in sync with the project's port and domains:
//
//	PORT        the port the app listens on, on the server
//	BIND        127.0.0.1 (only reachable through a domain) or 0.0.0.0
//	PUBLIC_URL  https://<first domain>, or http://<server>:PORT without one
//	PUBLIC_HOST the first domain, or the server's host
//	HTTPS       "true" with a domain, else "false"
//
// Everything else is listed in Vars: secrets are generated, the rest have
// defaults the user can change.
package templates

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Managed variable names.
const (
	VarPort       = "PORT"
	VarBind       = "BIND"
	VarPublicURL  = "PUBLIC_URL"
	VarPublicHost = "PUBLIC_HOST"
	VarHTTPS      = "HTTPS"
)

// Managed lists the variables TrueDeploy sets itself.
var Managed = []string{VarPort, VarBind, VarPublicURL, VarPublicHost, VarHTTPS}

// Var is a setting the app needs in .env.
type Var struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Secret values are generated (Length random characters) and masked in the UI.
	Secret  bool   `json:"secret,omitempty"`
	Length  int    `json:"length,omitempty"`
	Default string `json:"default,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// Template is one app in the catalog.
type Template struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
	Category string `json:"category"`
	Website  string `json:"website"`
	// Image is the main image, shown in the gallery.
	Image string `json:"image"`
	// DefaultPort is the first port to try on the server.
	DefaultPort int `json:"defaultPort"`
	// WantsDomain: the app needs HTTPS to work properly (it is still allowed without).
	WantsDomain bool `json:"wantsDomain,omitempty"`
	// DBNetwork joins the private network of TrueDeploy databases.
	DBNetwork bool     `json:"dbNetwork,omitempty"`
	Vars      []Var    `json:"vars"`
	Notes     []string `json:"notes"`
	Compose   string   `json:"compose"`
}

// All returns the catalog in display order.
func All() []Template { return catalog }

// Get finds a template by ID.
func Get(id string) (Template, bool) {
	for _, t := range catalog {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// Secret returns n random characters that need no escaping in YAML, URLs or shells.
func Secret(n int) string {
	b := make([]byte, n)
	for i := range b {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[k.Int64()]
	}
	return string(b)
}

// Env returns the app's starting variables: generated secrets and defaults.
// overrides (from the form) win over defaults but never over empty secrets.
func (t Template) Env(overrides map[string]string) map[string]string {
	env := map[string]string{}
	for _, v := range t.Vars {
		val := v.Default
		if v.Secret {
			n := v.Length
			if n == 0 {
				n = 32
			}
			val = Secret(n)
		}
		if o, ok := overrides[v.Key]; ok && strings.TrimSpace(o) != "" {
			val = strings.TrimSpace(o)
		}
		env[v.Key] = val
	}
	return env
}

// ManagedEnv returns the managed variables for a port, the project's domains
// and the server's host. Without a domain the app is published on all
// interfaces (so it can be reached at all); with one, only locally.
func ManagedEnv(port int, domains []string, serverHost string) map[string]string {
	m := map[string]string{VarPort: strconv.Itoa(port)}
	if len(domains) > 0 {
		m[VarBind] = "127.0.0.1"
		m[VarPublicURL] = "https://" + domains[0]
		m[VarPublicHost], _, _ = strings.Cut(domains[0], "/")
		m[VarHTTPS] = "true"
	} else {
		m[VarBind] = "0.0.0.0"
		m[VarPublicURL] = fmt.Sprintf("http://%s:%d", serverHost, port)
		m[VarPublicHost] = serverHost
		m[VarHTTPS] = "false"
	}
	return m
}

var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?-[^}]*)?\}`)

// References lists the variables a compose file reads without a default.
func References(compose string) []string {
	seen := map[string]bool{}
	for _, m := range refRe.FindAllStringSubmatch(compose, -1) {
		if m[2] == "" {
			seen[m[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
