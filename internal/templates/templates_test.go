package templates

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCatalogIsConsistent(t *testing.T) {
	ids := map[string]bool{}
	ports := map[int]string{}
	for _, tp := range All() {
		t.Run(tp.ID, func(t *testing.T) {
			if ids[tp.ID] {
				t.Fatal("duplicate id")
			}
			ids[tp.ID] = true
			if other, ok := ports[tp.DefaultPort]; ok {
				t.Errorf("default port %d also used by %s", tp.DefaultPort, other)
			}
			ports[tp.DefaultPort] = tp.ID
			if tp.Name == "" || tp.Tagline == "" || tp.Category == "" || !strings.HasPrefix(tp.Website, "https://") || len(tp.Notes) == 0 {
				t.Error("missing display fields")
			}

			var doc struct {
				Services map[string]struct {
					Image string   `yaml:"image"`
					Ports []string `yaml:"ports"`
				} `yaml:"services"`
			}
			if err := yaml.Unmarshal([]byte(tp.Compose), &doc); err != nil {
				t.Fatalf("compose is not valid YAML: %v", err)
			}
			published := 0
			images := map[string]bool{}
			for name, svc := range doc.Services {
				images[svc.Image] = true
				// Floating tags would make a redeploy jump to a breaking release,
				// except the few vendors that only publish latest/stable/lts.
				if !regexp.MustCompile(`:(\d+(\.\d+)?(-apache)?|lts|stable|latest)$`).MatchString(svc.Image) {
					t.Errorf("%s: image %q isn't pinned", name, svc.Image)
				}
				for _, p := range svc.Ports {
					published++
					if !strings.HasPrefix(p, "${BIND}:${PORT}:") {
						t.Errorf("%s: port %q must be published as ${BIND}:${PORT}:<container port>", name, p)
					}
				}
			}
			if published != 1 {
				t.Errorf("exactly one port must be published, got %d", published)
			}
			if !images[tp.Image] {
				t.Errorf("gallery image %q isn't used in the compose file", tp.Image)
			}

			provided := map[string]bool{}
			for _, k := range Managed {
				provided[k] = true
			}
			for _, v := range tp.Vars {
				if provided[v.Key] {
					t.Errorf("variable %s declared twice (or clashes with a managed one)", v.Key)
				}
				provided[v.Key] = true
				if !v.Secret && v.Default == "" {
					t.Errorf("%s needs a default or must be generated", v.Key)
				}
			}
			for _, ref := range References(tp.Compose) {
				if !provided[ref] {
					t.Errorf("compose uses ${%s} but nothing sets it", ref)
				}
			}
			for _, v := range tp.Vars {
				if !strings.Contains(tp.Compose, "${"+v.Key+"}") {
					t.Errorf("variable %s is never used", v.Key)
				}
			}
		})
	}
}

func TestEnvGeneratesSecretsAndKeepsOverrides(t *testing.T) {
	tp, _ := Get("vaultwarden")
	a, b := tp.Env(nil), tp.Env(map[string]string{"SIGNUPS_ALLOWED": " false ", "ADMIN_TOKEN": "  "})
	if len(a["ADMIN_TOKEN"]) != 48 || a["ADMIN_TOKEN"] == b["ADMIN_TOKEN"] {
		t.Errorf("tokens should be random and 48 long: %q %q", a["ADMIN_TOKEN"], b["ADMIN_TOKEN"])
	}
	if a["SIGNUPS_ALLOWED"] != "true" || b["SIGNUPS_ALLOWED"] != "false" {
		t.Errorf("defaults/overrides: %v %v", a, b)
	}
	if strings.Trim(Secret(64), alphabet) != "" {
		t.Error("secret uses characters outside the safe alphabet")
	}
}

func TestManagedEnv(t *testing.T) {
	d := ManagedEnv(8100, []string{"blog.example.com", "www.example.com"}, "1.2.3.4")
	if d["BIND"] != "127.0.0.1" || d["PUBLIC_URL"] != "https://blog.example.com" || d["PUBLIC_HOST"] != "blog.example.com" || d["HTTPS"] != "true" || d["PORT"] != "8100" {
		t.Errorf("with domain: %v", d)
	}
	n := ManagedEnv(8100, nil, "1.2.3.4")
	if n["BIND"] != "0.0.0.0" || n["PUBLIC_URL"] != "http://1.2.3.4:8100" || n["HTTPS"] != "false" {
		t.Errorf("without domain: %v", n)
	}
	if got := References("a: ${X}\nb: ${Y:-1}\nc: ${X}\nd: ${Z-2}"); strings.Join(got, ",") != "X" {
		t.Errorf("References = %v", got)
	}
}
