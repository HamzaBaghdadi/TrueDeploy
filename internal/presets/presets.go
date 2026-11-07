// Package presets generates Dockerfiles (and nginx configs for static
// sites) for common frameworks, so a repository without a Dockerfile can
// be deployed as is. The files are written next to the code at deploy time
// (in .truedeploy/), never committed unless the user asks for that.
package presets

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// Kinds, shown as groups in the picker.
const (
	Static  = "Static site"
	SSR     = "Server-rendered"
	Backend = "Backend"
)

// Option is one setting of a preset.
type Option struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type is "text" or "bool".
	Type    string `json:"type"`
	Default string `json:"default"`
	Hint    string `json:"hint,omitempty"`
	// Advanced options are folded away in the UI.
	Advanced bool `json:"advanced,omitempty"`
}

type Preset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Language    string   `json:"language"` // "node", "python", "php", ...
	Description string   `json:"description"`
	Port        int      `json:"port"` // the port the container listens on
	Options     []Option `json:"options"`
	// Notes are things to check in the repo (e.g. a config flag).
	Notes []string `json:"notes,omitempty"`

	dockerfile string
	nginx      bool
}

// Files are the generated files, by path relative to the app folder.
type Files map[string]string

// Dir is where generated files are written inside the app folder.
const Dir = ".truedeploy"

// All lists the presets in display order.
func All() []Preset { return catalog }

func Get(id string) (Preset, bool) {
	for _, p := range catalog {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

var (
	versionRe  = regexp.MustCompile(`^[0-9][0-9a-z.-]{0,30}$`)
	pathOptRe  = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	sizeRe     = regexp.MustCompile(`^[0-9]{1,5}[kKmMgG]?$`)
	routeRe    = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
	moduleRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*(:[A-Za-z_][A-Za-z0-9_]*)?$`)
	identRe    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	versionKey = map[string]bool{"node": true, "python": true, "php": true, "java": true, "go": true, "dotnet": true, "ruby": true}
)

// Values merges options with defaults and validates them. Commands may
// contain anything a shell line can, but never line breaks (which would end
// the Dockerfile instruction).
func (p Preset) Values(opts map[string]string) (map[string]string, error) {
	v := map[string]string{}
	for _, o := range p.Options {
		val, ok := opts[o.Key]
		if !ok {
			val = o.Default
		}
		val = strings.TrimSpace(val)
		// A cleared setting means "the default" (versions, folders, paths...).
		// Commands are the exception: an empty build or "before" means none.
		if val == "" && o.Type == "text" && o.Key != "build" && o.Key != "start" && o.Key != "before" {
			val = o.Default
		}
		if strings.ContainsAny(val, "\r\n") {
			return nil, fmt.Errorf("%s can't contain line breaks", o.Label)
		}
		if o.Type == "bool" {
			b, err := strconv.ParseBool(orDefault(val, "false"))
			if err != nil {
				return nil, fmt.Errorf("%s must be on or off", o.Label)
			}
			val = strconv.FormatBool(b)
		}
		if err := check(o, val); err != nil {
			return nil, err
		}
		v[o.Key] = val
	}
	return v, nil
}

func check(o Option, val string) error {
	bad := func(what string) error { return fmt.Errorf("%s: %q isn't %s", o.Label, val, what) }
	switch {
	case versionKey[o.Key]:
		if !versionRe.MatchString(val) {
			return bad("a version like 22 or 3.12")
		}
	case o.Key == "output", o.Key == "main", o.Key == "public":
		if !pathOptRe.MatchString(val) || strings.Contains(val, "..") || strings.HasPrefix(val, "/") {
			return bad("a folder inside the app")
		}
	case o.Key == "max_body":
		if !sizeRe.MatchString(val) {
			return bad("a size like 20m")
		}
	case o.Key == "api_path":
		if !routeRe.MatchString(val) {
			return bad("a path like /api")
		}
	case o.Key == "api_upstream":
		if val == "" {
			return nil
		}
		u, err := url.Parse(val)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(val, " ;{}'\"") {
			return bad("a URL like https://api.example.com")
		}
	case o.Key == "app":
		if !moduleRe.MatchString(val) {
			return bad("a module like main:app")
		}
	case o.Key == "binary":
		if val != "" && !identRe.MatchString(val) {
			return bad("a program name")
		}
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Render generates the preset's files with the given options.
// port overrides the preset's container port (0 keeps the default); static
// sites always listen on 80 inside the container.
func Render(id string, opts map[string]string, port int) (Files, error) {
	p, ok := Get(id)
	if !ok {
		return nil, fmt.Errorf("unknown framework preset %q", id)
	}
	v, err := p.Values(opts)
	if err != nil {
		return nil, err
	}
	if port <= 0 || p.nginx {
		port = p.Port
	}
	data := map[string]any{"V": v, "Port": port, "Dir": Dir}
	out := Files{}
	df, err := execTpl(p.ID, p.dockerfile, data)
	if err != nil {
		return nil, err
	}
	out[Dir+"/Dockerfile"] = df
	if p.nginx {
		nc, err := execTpl(p.ID+"-nginx", nginxConf, data)
		if err != nil {
			return nil, err
		}
		out[Dir+"/nginx.conf"] = nc
	}
	return out, nil
}

var funcs = template.FuncMap{
	"on": func(s string) bool { return s == "true" },
	"q":  strconv.Quote,
	// shq single-quotes for sh -c.
	"shq": func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" },
	"trimslash": func(s string) string {
		return strings.TrimRight(s, "/")
	},
}

func execTpl(name, tpl string, data any) (string, error) {
	t, err := template.New(name).Funcs(funcs).Option("missingkey=error").Parse(tpl)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	// Collapse the blank lines left by skipped template blocks.
	s := regexp.MustCompile(`\n{3,}`).ReplaceAllString(b.String(), "\n\n")
	return strings.TrimLeft(s, "\n"), nil
}

// Kinds returns the presets grouped in display order.
func Kinds() []string { return []string{Static, SSR, Backend} }

// Sorted returns IDs sorted by name (for stable test output).
func Sorted() []string {
	var ids []string
	for _, p := range catalog {
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	return ids
}
