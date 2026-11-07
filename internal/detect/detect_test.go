package detect

import (
	"strings"
	"testing"

	"truedeploy/internal/presets"
)

func repo(files map[string]string) Repo {
	r := Repo{Contents: map[string]string{}}
	for f, c := range files {
		r.Files = append(r.Files, f)
		if c != "" {
			r.Contents[f] = c
		}
	}
	return r
}

func pkgJSON(deps ...string) string {
	var b strings.Builder
	b.WriteString(`{"scripts":{"build":"x"},"dependencies":{`)
	for i, d := range deps {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + d + `":"1"`)
	}
	b.WriteString("}}")
	return b.String()
}

func TestSingleApps(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
		opts  map[string]string
	}{
		{"next export", map[string]string{"package.json": pkgJSON("next", "react"), "next.config.mjs": "export default { output: 'export' }"}, "nextjs-export", nil},
		{"next standalone", map[string]string{"package.json": pkgJSON("next"), "next.config.ts": `const c = { output: "standalone" }`}, "nextjs-standalone", nil},
		{"next ssr", map[string]string{"package.json": pkgJSON("next")}, "nextjs-ssr", nil},
		{"nuxt", map[string]string{"package.json": pkgJSON("nuxt")}, "nuxt-ssr", nil},
		{"nuxt static", map[string]string{"package.json": pkgJSON("nuxt"), "nuxt.config.ts": "export default defineNuxtConfig({ ssr: false })"}, "nuxt-static", nil},
		{"sveltekit spa", map[string]string{"package.json": pkgJSON("@sveltejs/kit", "@sveltejs/adapter-static"), "svelte.config.js": "adapter({ fallback: 'index.html' })"}, "sveltekit-static", map[string]string{"spa": "true"}},
		{"sveltekit node", map[string]string{"package.json": pkgJSON("@sveltejs/kit", "@sveltejs/adapter-node")}, "sveltekit-node", nil},
		{"astro", map[string]string{"package.json": pkgJSON("astro")}, "astro-static", nil},
		{"astro ssr", map[string]string{"package.json": pkgJSON("astro", "@astrojs/node")}, "astro-ssr", nil},
		{"angular", map[string]string{"package.json": pkgJSON("@angular/core"), "angular.json": ""}, "angular", nil},
		{"angular ssr", map[string]string{"package.json": pkgJSON("@angular/core", "@angular/ssr")}, "angular-ssr", nil},
		{"remix", map[string]string{"package.json": pkgJSON("@remix-run/node", "react")}, "remix", nil},
		{"nest", map[string]string{"package.json": pkgJSON("@nestjs/core", "express")}, "nestjs", nil},
		{"vite vue", map[string]string{"package.json": pkgJSON("vite", "vue")}, "vue-vite", nil},
		{"vite react", map[string]string{"package.json": pkgJSON("vite", "react")}, "react-vite", nil},
		{"cra", map[string]string{"package.json": pkgJSON("react-scripts", "react")}, "react-cra", nil},
		{"express", map[string]string{"package.json": `{"scripts":{"start":"node index.js"},"dependencies":{"express":"4"}}`}, "node", map[string]string{}},
		{"django", map[string]string{"requirements.txt": "Django==5.0\npsycopg[binary]\n", "manage.py": "", "shop/wsgi.py": "", "shop/settings.py": ""}, "django", map[string]string{"app": "shop.wsgi"}},
		{"fastapi", map[string]string{"pyproject.toml": "dependencies = [\"fastapi>=0.110\", \"uvicorn\"]", "app/main.py": ""}, "fastapi", map[string]string{"app": "app.main:app"}},
		{"flask", map[string]string{"requirements.txt": "flask\ngunicorn", "app.py": ""}, "flask", nil},
		{"not flask", map[string]string{"requirements.txt": "flask-cors-helper-lib\n", "index.html": ""}, "html", nil},
		{"laravel", map[string]string{"composer.json": `{"require":{"laravel/framework":"^11"}}`}, "laravel", nil},
		{"rails", map[string]string{"Gemfile": "source 'https://rubygems.org'\ngem \"rails\", \"~> 7.1\"\n"}, "rails", nil},
		{"go cmd", map[string]string{"go.mod": "module x", "cmd/worker/main.go": "", "cmd/server/main.go": ""}, "go", map[string]string{"main": "./cmd/server"}},
		{"go root", map[string]string{"go.mod": "module x", "main.go": ""}, "go", nil},
		{"rust", map[string]string{"Cargo.toml": "[package]\nname = \"api\""}, "rust", nil},
		{"spring", map[string]string{"pom.xml": "<artifactId>spring-boot-starter-web</artifactId>", "mvnw": ""}, "spring", nil},
		{"dotnet", map[string]string{"Api.csproj": "", "Program.cs": ""}, "dotnet", nil},
		{"html", map[string]string{"index.html": "", "style.css": ""}, "html", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Detect(repo(c.files))
			if len(got) == 0 {
				t.Fatalf("no suggestion")
			}
			s := got[0]
			if s.Kind != "preset" || s.Preset != c.want || s.Folder != "" || s.Reason == "" {
				t.Fatalf("got %+v, want preset %s", s, c.want)
			}
			if c.opts != nil {
				for k, v := range c.opts {
					if s.Options[k] != v {
						t.Errorf("option %s = %q, want %q", k, s.Options[k], v)
					}
				}
			}
			// Every suggestion must render with its options.
			if _, err := presets.Render(s.Preset, s.Options, 0); err != nil {
				t.Errorf("suggested options don't render: %v", err)
			}
		})
	}
}

func TestMonorepoAndExistingFiles(t *testing.T) {
	r := repo(map[string]string{
		"package.json":                        `{"private":true,"workspaces":["frontend","backend"],"devDependencies":{"prettier":"3"}}`,
		"frontend/package.json":               pkgJSON("next", "react"),
		"frontend/next.config.js":             "module.exports = { output: 'export' }",
		"backend/package.json":                pkgJSON("@nestjs/core"),
		"backend/node_modules/x/package.json": pkgJSON("express"),
		"docs/index.html":                     "",
		"apps/admin/Dockerfile":               "",
		"apps/admin/package.json":             pkgJSON("vite", "vue"),
		".github/workflows/x/package.json":    pkgJSON("express"),
	})
	got := Detect(r)
	var summary []string
	for _, s := range got {
		summary = append(summary, s.Folder+"="+s.Kind+":"+s.Preset)
	}
	want := "apps/admin=dockerfile:,backend=preset:nestjs,docs=preset:html,frontend=preset:nextjs-export"
	if strings.Join(summary, ",") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(summary, ","), want)
	}

	c := Detect(repo(map[string]string{"docker-compose.yml": "", "package.json": pkgJSON("express")}))
	if len(c) != 2 || c[0].Kind != "compose" || c[1].Preset != "node" {
		t.Errorf("compose first: %+v", c)
	}
	if len(Detect(repo(map[string]string{"README.md": ""}))) != 0 {
		t.Error("nothing to deploy")
	}
	if !KeyFile("frontend/next.config.mjs") || KeyFile("src/index.ts") {
		t.Error("KeyFile")
	}
}
