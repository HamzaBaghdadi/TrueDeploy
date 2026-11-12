package proxy

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestCaddyfile(t *testing.T) {
	got := Caddyfile([]Site{
		{Project: "web", Domains: []string{"www.example.com", "example.com"}, Port: 8080},
		{Project: "api", Domains: []string{"api.example.com"}, Port: 3000},
		{Project: "worker", Port: 9000}, // no domains: skipped
	})
	want := `# Managed by TrueDeploy. Changes here are overwritten on the next apply.

# api
api.example.com {
	encode zstd gzip
	reverse_proxy localhost:3000
}

# web
example.com, www.example.com {
	encode zstd gzip
	reverse_proxy localhost:8080
}
`
	if got != want {
		t.Fatalf("Caddyfile:\n%s\nwant:\n%s", got, want)
	}
}

func TestValidate(t *testing.T) {
	ok := []Site{{Project: "a", Domains: []string{"a.example.com"}, Port: 80}}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string][]Site{
		"duplicate":  {{Project: "a", Domains: []string{"x.io"}, Port: 1}, {Project: "b", Domains: []string{"x.io"}, Port: 2}},
		"bad domain": {{Project: "a", Domains: []string{"not a domain"}, Port: 1}},
		"injection":  {{Project: "a", Domains: []string{"x.io {\n respond hacked"}, Port: 1}},
		"no tld":     {{Project: "a", Domains: []string{"localhost"}, Port: 1}},
		"port":       {{Project: "a", Domains: []string{"x.io"}, Port: 0}},
	}
	for name, sites := range bad {
		if Validate(sites) == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	for in, want := range map[string]string{
		" HTTPS://Api.Example.com/path?x=1 ": "api.example.com/path",
		"example.com/API/":                   "example.com/API",
		"https://example.com/":               "example.com",
		"example.com.":                       "example.com",
		"http://a.b.io":                      "a.b.io",
	} {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScriptsParse(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	if exec.Command(bash, "-c", "true").Run() != nil {
		t.Skip("no bash")
	}
	for name, s := range map[string]string{"apply": ApplyScript(Caddyfile(nil)), "status": StatusScript()} {
		if out, err := exec.Command(bash, "-n", "-c", s).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
	// The config must travel encoded, never as raw text in the command.
	if strings.Contains(ApplyScript("evil'; rm -rf / #"), "rm -rf") {
		t.Error("Caddyfile content leaked into the script unencoded")
	}
}

func TestCaddyfileSharedHost(t *testing.T) {
	got := Caddyfile([]Site{
		{Project: "frontend", Domains: []string{"shop.io", "www.shop.io"}, Port: 8080},
		{Project: "backend", Domains: []string{"shop.io/api"}, Port: 3000},
		{Project: "admin", Domains: []string{"shop.io/api/admin"}, Port: 4000, StripPath: true},
		{Project: "docs", Domains: []string{"docs.io/v1"}, Port: 5000},
	})
	want := `# Managed by TrueDeploy. Changes here are overwritten on the next apply.

# frontend
www.shop.io {
	encode zstd gzip
	reverse_proxy localhost:8080
}

docs.io {
	encode zstd gzip

	# docs
	@r0 path /v1 /v1/*
	handle @r0 {
		reverse_proxy localhost:5000
	}

	handle {
		respond "Not found" 404
	}
}

shop.io {
	encode zstd gzip

	# admin
	@r0 path /api/admin /api/admin/*
	handle @r0 {
		uri strip_prefix /api/admin
		reverse_proxy localhost:4000
	}

	# backend
	@r1 path /api /api/*
	handle @r1 {
		reverse_proxy localhost:3000
	}

	# frontend
	handle {
		reverse_proxy localhost:8080
	}
}
`
	if got != want {
		t.Fatalf("Caddyfile:\n%s\nwant:\n%s", got, want)
	}

	if err := Validate([]Site{{Project: "a", Domains: []string{"shop.io"}, Port: 1}, {Project: "b", Domains: []string{"shop.io/api"}, Port: 2}}); err != nil {
		t.Errorf("same host, different paths: %v", err)
	}
	for _, bad := range []string{"shop.io/a b", "shop.io/api/../x", "shop.io/{x}", "shop.io//x"} {
		if Validate([]Site{{Project: "a", Domains: []string{bad}, Port: 1}}) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if h, p := SplitRoute("shop.io/api/v2"); h != "shop.io" || p != "/api/v2" {
		t.Errorf("SplitRoute = %q %q", h, p)
	}
}

func TestPasswordAndMaintenance(t *testing.T) {
	hash := "$2a$12$" + strings.Repeat("a", 53)
	got := Caddyfile([]Site{
		{Project: "staging", Domains: []string{"staging.shop.io"}, Port: 8081, AuthUser: "team", AuthHash: hash},
		{Project: "web", Domains: []string{"shop.io"}, Port: 8080, Maintenance: true, MaintenanceMessage: "Upgrading <db> & back at 10:00\nThanks!"},
		{Project: "admin", Domains: []string{"shop.io/admin"}, Port: 9000, AuthUser: "boss", AuthHash: hash, StripPath: true},
	})
	for _, want := range []string{
		"staging.shop.io {\n\tencode zstd gzip\n\tbasic_auth {\n\t\tteam " + hash + "\n\t}\n\treverse_proxy localhost:8081\n}",
		"\t@r0 path /admin /admin/*\n\thandle @r0 {\n\t\turi strip_prefix /admin\n\t\tbasic_auth {\n\t\t\tboss " + hash + "\n\t\t}\n\t\treverse_proxy localhost:9000\n\t}",
		"\thandle {\n\t\theader Content-Type \"text/html; charset=utf-8\"\n\t\theader Retry-After 600\n\t\trespond <<HTML\n<!doctype html>",
		"Upgrading &lt;db&gt; &amp; back at 10:00<br>Thanks!",
		"</html>\nHTML 503\n\t}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing:\n%s\n--- in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "reverse_proxy localhost:8080") {
		t.Error("maintenance must not proxy to the app")
	}
	// The page is a single line, so nothing in it can end the heredoc.
	if strings.Contains(MaintenancePage("x\nHTML\ny"), "\n") {
		t.Error("maintenance page must be one line")
	}

	for name, s := range map[string]Site{
		"bad user":    {Project: "a", Domains: []string{"a.io"}, Port: 1, AuthUser: "a b", AuthHash: hash},
		"bad hash":    {Project: "a", Domains: []string{"a.io"}, Port: 1, AuthUser: "a", AuthHash: "plaintext"},
		"hash only":   {Project: "a", Domains: []string{"a.io"}, Port: 1, AuthHash: hash},
		"long notice": {Project: "a", Domains: []string{"a.io"}, Port: 1, Maintenance: true, MaintenanceMessage: strings.Repeat("x", 501)},
	} {
		if Validate([]Site{s}) == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
