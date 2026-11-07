// Package detect guesses how to deploy a repository: which framework
// preset (and its settings), whether it already has a Dockerfile or a
// compose file, and which folders of a monorepo hold apps.
//
// It works on a file list plus the contents of a few key files, so the
// same rules apply whether the repo is read through GitHub or a clone.
package detect

import (
	"encoding/base64"
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Repo is what detection needs: every file path (slash-separated, relative
// to the repository root) and the contents of key files (see KeyFile).
type Repo struct {
	Files    []string
	Contents map[string]string
}

// Suggestion is one way to deploy (part of) the repository.
type Suggestion struct {
	Folder  string            `json:"folder"` // "" is the repository root
	Kind    string            `json:"kind"`   // "preset", "dockerfile" or "compose"
	Preset  string            `json:"preset,omitempty"`
	Options map[string]string `json:"options,omitempty"` // only values that differ from the preset's defaults
	Reason  string            `json:"reason"`
	Notes   []string          `json:"notes,omitempty"`
}

var keyNames = map[string]bool{
	"package.json": true, "requirements.txt": true, "pyproject.toml": true, "composer.json": true, "Gemfile": true,
	"go.mod": true, "Cargo.toml": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"next.config.js": true, "next.config.mjs": true, "next.config.ts": true,
	"nuxt.config.ts": true, "nuxt.config.js": true, "svelte.config.js": true,
	"astro.config.mjs": true, "astro.config.ts": true,
}

// KeyFile reports whether a file's contents are needed (fetchers read only these).
func KeyFile(p string) bool { return keyNames[path.Base(p)] }

var skipDirs = map[string]bool{"node_modules": true, ".git": true, "vendor": true, "dist": true, "build": true, ".next": true, "target": true, "out": true}

// MaxDepth is how deep apps are looked for (root, frontend/, apps/web/).
const MaxDepth = 2

func (r Repo) has(p string) bool {
	for _, f := range r.Files {
		if f == p {
			return true
		}
	}
	return false
}

func (r Repo) read(p string) string { return r.Contents[p] }

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// filesIn lists the direct children files of dir.
func (r Repo) filesIn(dir string) []string {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var out []string
	for _, f := range r.Files {
		if rest, ok := strings.CutPrefix(f, prefix); ok && !strings.Contains(rest, "/") {
			out = append(out, rest)
		}
	}
	return out
}

// Detect returns suggestions, best first: the root app (or the compose
// file) first, then apps found in subfolders.
func Detect(r Repo) []Suggestion {
	var out []Suggestion
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		if r.has(name) {
			out = append(out, Suggestion{Kind: "compose", Reason: "The repository has " + name + ": deploy it with Docker Compose (all its services together)."})
			break
		}
	}
	if s, ok := folder(r, ""); ok {
		out = append(out, s)
	}
	for _, dir := range candidateDirs(r) {
		if s, ok := folder(r, dir); ok {
			out = append(out, s)
		}
	}
	return out
}

var markers = []string{"package.json", "requirements.txt", "pyproject.toml", "composer.json", "Gemfile", "go.mod", "Cargo.toml",
	"pom.xml", "build.gradle", "build.gradle.kts", "Dockerfile", "index.html", "manage.py"}

// candidateDirs are subfolders (up to MaxDepth) that look like an app.
func candidateDirs(r Repo) []string {
	seen := map[string]bool{}
	for _, f := range r.Files {
		dir := path.Dir(f)
		if dir == "." {
			continue
		}
		parts := strings.Split(dir, "/")
		if len(parts) > MaxDepth || skip(parts) {
			continue
		}
		base := path.Base(f)
		isMarker := strings.HasSuffix(base, ".csproj")
		for _, m := range markers {
			isMarker = isMarker || base == m
		}
		if isMarker {
			seen[dir] = true
		}
	}
	var out []string
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func skip(parts []string) bool {
	for _, p := range parts {
		if skipDirs[p] || strings.HasPrefix(p, ".") {
			return true
		}
	}
	return false
}

type pkg struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Workspaces      json.RawMessage   `json:"workspaces"`
}

func (p pkg) dep(name string) bool {
	_, a := p.Dependencies[name]
	_, b := p.DevDependencies[name]
	return a || b
}

func (p pkg) depPrefix(prefix string) bool {
	for k := range p.Dependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	for k := range p.DevDependencies {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func folder(r Repo, dir string) (Suggestion, bool) {
	s := Suggestion{Folder: dir, Kind: "preset"}
	files := r.filesIn(dir)
	has := func(name string) bool { return r.has(join(dir, name)) }
	read := func(name string) string { return r.read(join(dir, name)) }
	firstOf := func(names ...string) string {
		for _, n := range names {
			if has(n) {
				return read(n)
			}
		}
		return ""
	}

	if has("Dockerfile") {
		return Suggestion{Folder: dir, Kind: "dockerfile", Reason: "Has its own Dockerfile."}, true
	}

	if raw := read("package.json"); has("package.json") {
		var p pkg
		_ = json.Unmarshal([]byte(raw), &p)
		if id, opts, reason, notes := node(p, firstOf); id != "" {
			s.Preset, s.Options, s.Reason, s.Notes = id, opts, reason, notes
			return s, true
		}
	}

	py := strings.ToLower(read("requirements.txt") + "\n" + read("pyproject.toml"))
	switch {
	case has("manage.py") || pyDep(py, "django"):
		s.Preset, s.Reason = "django", "Django project."
		if m := wsgiModule(r, dir); m != "" {
			s.Options = map[string]string{"app": m}
			s.Reason += " WSGI module: " + m + "."
		}
		return s, true
	case pyDep(py, "fastapi"):
		s.Preset, s.Reason = "fastapi", "FastAPI app."
		if app := pyApp(r, dir, "main:app"); app != "main:app" {
			s.Options = map[string]string{"app": app}
		}
		return s, true
	case pyDep(py, "flask"):
		s.Preset, s.Reason = "flask", "Flask app."
		if app := pyApp(r, dir, "app:app"); app != "app:app" {
			s.Options = map[string]string{"app": app}
		}
		return s, true
	}

	if has("composer.json") {
		if strings.Contains(read("composer.json"), "laravel/framework") {
			s.Preset, s.Reason = "laravel", "Laravel (laravel/framework in composer.json)."
		} else {
			s.Preset, s.Reason = "php", "PHP project (composer.json)."
			if has("public/index.php") {
				s.Options = map[string]string{"public": "public"}
			}
		}
		return s, true
	}
	if has("Gemfile") && regexp.MustCompile(`(?m)^\s*gem\s+["']rails["']`).MatchString(read("Gemfile")) {
		s.Preset, s.Reason = "rails", "Ruby on Rails (rails in the Gemfile)."
		return s, true
	}
	if has("go.mod") {
		s.Preset, s.Reason = "go", "Go module."
		if m := goMain(r, dir); m != "." {
			s.Options = map[string]string{"main": m}
			s.Reason += " Main package: " + m + "."
		}
		return s, true
	}
	if has("Cargo.toml") {
		s.Preset, s.Reason = "rust", "Rust crate (Cargo.toml)."
		return s, true
	}
	if build := firstOf("pom.xml", "build.gradle", "build.gradle.kts"); build != "" {
		if strings.Contains(build, "spring-boot") {
			s.Preset, s.Reason = "spring", "Spring Boot (spring-boot in the build file)."
			if !has("mvnw") && !has("gradlew") {
				s.Notes = []string{"Add the Maven (mvnw) or Gradle (gradlew) wrapper to the repository: the build uses it."}
			}
			return s, true
		}
	}
	var csproj []string
	for _, f := range files {
		if strings.HasSuffix(f, ".csproj") {
			csproj = append(csproj, f)
		}
	}
	if len(csproj) > 0 {
		s.Preset, s.Reason = "dotnet", ".NET project ("+csproj[0]+")."
		if len(csproj) > 1 {
			s.Options = map[string]string{"main": csproj[0]}
		}
		return s, true
	}
	if has("index.html") {
		s.Preset, s.Reason = "html", "Plain HTML site (index.html, no build tool)."
		return s, true
	}
	return Suggestion{}, false
}

// node picks a preset from package.json (plus framework config files).
func node(p pkg, firstOf func(...string) string) (id string, opts map[string]string, reason string, notes []string) {
	build := p.Scripts["build"]
	switch {
	case p.dep("next"):
		cfg := firstOf("next.config.js", "next.config.mjs", "next.config.ts")
		switch {
		case regexp.MustCompile(`output\s*:\s*["']export["']`).MatchString(cfg):
			return "nextjs-export", nil, `Next.js with output: "export" (static).`, nil
		case regexp.MustCompile(`output\s*:\s*["']standalone["']`).MatchString(cfg):
			return "nextjs-standalone", nil, `Next.js with output: "standalone".`, nil
		}
		return "nextjs-ssr", nil, "Next.js (server-rendered: no static export in next.config).",
			[]string{`If every page can be static, set output: "export" in next.config for a lighter nginx deploy.`}
	case p.dep("nuxt"):
		cfg := firstOf("nuxt.config.ts", "nuxt.config.js")
		if strings.Contains(build, "generate") || regexp.MustCompile(`ssr\s*:\s*false`).MatchString(cfg) {
			return "nuxt-static", nil, "Nuxt generated as a static site.", nil
		}
		return "nuxt-ssr", nil, "Nuxt with server-side rendering.", nil
	case p.dep("@sveltejs/kit"):
		switch {
		case p.dep("@sveltejs/adapter-static"):
			cfg := firstOf("svelte.config.js")
			if strings.Contains(cfg, "fallback") {
				return "sveltekit-static", map[string]string{"spa": "true"}, "SvelteKit with adapter-static (SPA fallback).", nil
			}
			return "sveltekit-static", nil, "SvelteKit with adapter-static.", nil
		case p.dep("@sveltejs/adapter-node"):
			return "sveltekit-node", nil, "SvelteKit with adapter-node.", nil
		}
		return "sveltekit-node", nil, "SvelteKit.", []string{"It uses adapter-auto: install @sveltejs/adapter-node (server) or adapter-static (static site) and set it in svelte.config.js."}
	case p.dep("astro"):
		if p.dep("@astrojs/node") {
			return "astro-ssr", nil, "Astro with the Node adapter.", nil
		}
		return "astro-static", nil, "Astro (static output).", nil
	case p.dep("@angular/core"):
		if p.dep("@angular/ssr") {
			return "angular-ssr", nil, "Angular with SSR (@angular/ssr).", nil
		}
		return "angular", nil, "Angular (client-side).", nil
	case p.dep("@react-router/dev") || p.depPrefix("@remix-run/"):
		return "remix", nil, "React Router framework mode / Remix.", nil
	case p.dep("gatsby"):
		return "gatsby", nil, "Gatsby.", nil
	case p.dep("@docusaurus/core"):
		return "docusaurus", nil, "Docusaurus.", nil
	case p.dep("vitepress"):
		for _, k := range []string{"docs:build", "build"} {
			if v := p.Scripts[k]; strings.Contains(v, "vitepress build") {
				return "vitepress", map[string]string{"build": "npm run " + k}, "VitePress.", nil
			}
		}
		return "vitepress", nil, "VitePress.", nil
	case p.dep("@nestjs/core"):
		return "nestjs", nil, "NestJS (@nestjs/core).", nil
	case p.dep("vite"):
		switch {
		case p.dep("react"):
			return "react-vite", nil, "React with Vite.", nil
		case p.dep("vue"):
			return "vue-vite", nil, "Vue with Vite.", nil
		case p.dep("svelte"):
			return "svelte-vite", nil, "Svelte with Vite.", nil
		case p.dep("solid-js"):
			return "solid-vite", nil, "Solid with Vite.", nil
		}
		return "react-vite", nil, "Vite app.", []string{"Check the framework: the Vite presets differ only in their name."}
	case p.dep("react-scripts"):
		return "react-cra", nil, "Create React App (react-scripts).", nil
	}
	for _, d := range []string{"express", "fastify", "koa", "hono", "@hapi/hapi", "@adonisjs/core"} {
		if p.dep(d) {
			opts = map[string]string{}
			if build != "" {
				opts["build"] = "npm run build"
			}
			if p.Scripts["start"] == "" {
				notes = []string{`package.json has no "start" script: set the start command (e.g. node server.js).`}
			}
			return "node", opts, "Node.js server (" + d + ").", notes
		}
	}
	if p.Scripts["start"] != "" && len(p.Workspaces) == 0 {
		return "node", nil, `Node.js app with a "start" script.`, nil
	}
	return "", nil, "", nil
}

func pyDep(text, name string) bool {
	return regexp.MustCompile(`(?m)(^|["'\s,\[])` + regexp.QuoteMeta(name) + `([\s=<>~!\[;"',]|$)`).MatchString(text)
}

// wsgiModule finds <project>/wsgi.py, giving "<project>.wsgi".
func wsgiModule(r Repo, dir string) string {
	for _, f := range r.Files {
		rel, ok := strings.CutPrefix(f, join(dir, ""))
		if dir == "" {
			rel, ok = f, true
		}
		if !ok {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) == 2 && parts[1] == "wsgi.py" && !skip(parts[:1]) {
			return parts[0] + ".wsgi"
		}
	}
	return ""
}

// pyApp guesses module:app from where the entry file is.
func pyApp(r Repo, dir, def string) string {
	for _, c := range []struct{ file, app string }{
		{"main.py", "main:app"}, {"app.py", "app:app"}, {"app/main.py", "app.main:app"}, {"src/main.py", "src.main:app"},
		{"wsgi.py", "wsgi:app"}, {"server.py", "server:app"},
	} {
		if r.has(join(dir, c.file)) {
			return c.app
		}
	}
	return def
}

// goMain is "." when main.go is at the folder root, else ./cmd/<name>.
func goMain(r Repo, dir string) string {
	if r.has(join(dir, "main.go")) {
		return "."
	}
	var cmds []string
	for _, f := range r.Files {
		rel := f
		if dir != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(f, dir+"/"); !ok {
				continue
			}
		}
		parts := strings.Split(rel, "/")
		if len(parts) == 3 && parts[0] == "cmd" && parts[2] == "main.go" {
			cmds = append(cmds, "./cmd/"+parts[1])
		}
	}
	sort.Strings(cmds)
	for _, c := range cmds {
		if strings.Contains(c, "server") || strings.Contains(c, "api") || strings.Contains(c, "web") {
			return c
		}
	}
	if len(cmds) > 0 {
		return cmds[0]
	}
	return "."
}

// KeyNames lists the file names whose contents detection reads.
func KeyNames() []string {
	out := make([]string, 0, len(keyNames))
	for n := range keyNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ParseProbe reads deploy.ProbeScript's output.
func ParseProbe(lines []string) Repo {
	r := Repo{Contents: map[string]string{}}
	for _, l := range lines {
		f := strings.Split(strings.TrimRight(l, "\r"), "\t")
		switch {
		case f[0] == "F" && len(f) >= 2:
			r.Files = append(r.Files, f[1])
		case f[0] == "C" && len(f) >= 3:
			if b, err := base64.StdEncoding.DecodeString(f[2]); err == nil {
				r.Contents[f[1]] = string(b)
			}
		}
	}
	return r
}
