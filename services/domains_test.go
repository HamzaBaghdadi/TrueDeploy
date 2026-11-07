package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"truedeploy/internal/proxy"
	"truedeploy/internal/sitecheck"
	"truedeploy/internal/store"
)

func TestSaveProjectDomains(t *testing.T) {
	st := newTestStore(t)
	if err := st.Put(store.Server{ID: "s2", Name: "other", Host: "h2", Port: 22, User: "u", Auth: "password", CreatedAt: time.Now()}, store.Secrets{}); err != nil {
		t.Fatal(err)
	}
	svc := NewProjectService(st, nil)
	base := ProjectInput{ServerID: "s1", RepoURL: "git@github.com:me/a.git", Runtime: "compose"}

	in := base
	in.Name, in.Domains = "api", []string{"https://API.example.com/", "api.example.com"}
	if _, err := svc.SaveProject(in); err == nil || !strings.Contains(err.Error(), "app port") {
		t.Fatalf("compose + domains without an app port must be rejected, got %v", err)
	}
	in.AppPort = 3000
	p, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Domains) != 1 || p.Domains[0] != "api.example.com" || p.ProxyPort != 3000 {
		t.Fatalf("domains should be normalised and de-duplicated: %+v", p.Domains)
	}

	// Dockerfile projects forward to their public port.
	web := base
	web.Name, web.Runtime, web.HostPort, web.ContainerPort = "web", "dockerfile", 8080, 80
	web.Domains, web.AppPort = []string{"example.com"}, 9999
	w, err := svc.SaveProject(web)
	if err != nil || w.ProxyPort != 8080 || w.AppPort != 0 {
		t.Fatalf("dockerfile project should proxy to its host port: %+v, %v", w, err)
	}

	// One domain can't serve two projects on the same server...
	dup := base
	dup.Name, dup.AppPort, dup.Domains = "dup", 4000, []string{"api.example.com"}
	if _, err := svc.SaveProject(dup); err == nil || !strings.Contains(err.Error(), "used by both") {
		t.Fatalf("want duplicate-domain error, got %v", err)
	}
	// ...but other servers have their own proxy.
	dup.ServerID = "s2"
	if _, err := svc.SaveProject(dup); err != nil {
		t.Fatalf("same domain on another server should be allowed: %v", err)
	}

	// Editing a project doesn't conflict with its own domains.
	in.ID = p.ID
	if _, err := svc.SaveProject(in); err != nil {
		t.Fatalf("re-saving should not conflict with itself: %v", err)
	}

	bad := base
	bad.Name, bad.AppPort, bad.Domains = "bad", 1, []string{"api.example.com {\nrespond x"}
	if _, err := svc.SaveProject(bad); err == nil {
		t.Fatal("malformed domains must be rejected")
	}
}

func TestCheckDomain(t *testing.T) {
	origLookup, origGet := lookup, httpsGet
	t.Cleanup(func() { lookup, httpsGet = origLookup, origGet })

	dns := map[string][]string{"good.io": {"1.2.3.4"}, "elsewhere.io": {"9.9.9.9"}, "noapp.io": {"1.2.3.4"}, "nocert.io": {"1.2.3.4"}}
	lookup = func(_ context.Context, h string) ([]string, error) {
		if ips, ok := dns[h]; ok {
			return ips, nil
		}
		return nil, errors.New("no such host")
	}
	httpsGet = func(_ context.Context, url string) sitecheck.Result {
		switch url {
		case "https://good.io/":
			return sitecheck.Result{URL: url, Status: 200, Latency: 42, CertExpires: time.Now().Add(60 * 24 * time.Hour)}
		case "https://noapp.io/":
			return sitecheck.Result{URL: url, Status: 502}
		}
		return sitecheck.Failed(url, errors.New("tls: failed to verify certificate: x509: certificate is valid for other.io"))
	}

	server := []string{"1.2.3.4"}
	ctx := context.Background()
	cases := map[string]struct{ dns, https, detail string }{
		"good.io":      {"ok", "ok", ""},
		"missing.io":   {"missing", "skipped", "A record for missing.io pointing to 1.2.3.4"},
		"elsewhere.io": {"wrong", "skipped", "points to 9.9.9.9"},
		"noapp.io":     {"ok", "ok", "502"},
		"nocert.io":    {"ok", "error", "certificate"},
	}
	for d, want := range cases {
		c := checkDomain(ctx, d, server)
		if d == "good.io" && (c.LatencyMs != 42 || c.CertDays < 59) {
			t.Errorf("good.io should carry latency and certificate expiry: %+v", c)
		}
		if c.DNS != want.dns || c.HTTPS != want.https || !strings.Contains(c.Detail, want.detail) {
			t.Errorf("%s: got dns=%s https=%s detail=%q, want %+v", d, c.DNS, c.HTTPS, c.Detail, want)
		}
	}
}

func TestFrontendAndBackendShareADomain(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	web, err := svc.SaveProject(ProjectInput{Name: "web", ServerID: "s1", RepoURL: "git@github.com:me/web.git", Runtime: "dockerfile", HostPort: 8080, ContainerPort: 80, Domains: []string{"https://Shop.io/"}})
	if err != nil {
		t.Fatal(err)
	}
	api, err := svc.SaveProject(ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "dockerfile", HostPort: 3000, ContainerPort: 3000, Domains: []string{"shop.io/api/"}, StripPath: true})
	if err != nil {
		t.Fatal(err)
	}
	if web.Domains[0] != "shop.io" || api.Domains[0] != "shop.io/api" || !api.StripPath {
		t.Fatalf("routes: %v %v", web.Domains, api.Domains)
	}
	if _, err := svc.SaveProject(ProjectInput{Name: "api2", ServerID: "s1", RepoURL: "git@github.com:me/api2.git", Runtime: "dockerfile", HostPort: 3001, ContainerPort: 3000, Domains: []string{"shop.io/api"}}); err == nil {
		t.Error("the same route can't belong to two projects")
	}
	all, _ := svc.store.ListProjects()
	cfg := proxy.Caddyfile(svc.sites("s1", nil, all))
	if !strings.Contains(cfg, "uri strip_prefix /api") || !strings.Contains(cfg, "reverse_proxy localhost:8080") {
		t.Errorf("caddyfile:\n%s", cfg)
	}
}

func TestSaveKeepsRedirectHosts(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	v, err := svc.SaveProject(ProjectInput{ServerID: "s1", Name: "web", RepoURL: "git@github.com:me/w.git", Runtime: "compose", AppPort: 3000,
		Domains: []string{"shop.io"}, Redirects: []string{"https://WWW.Shop.io/", "www.shop.io", "shop.io", "old.io/path", " "}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(v.Redirects, ","); got != "www.shop.io,old.io" {
		t.Errorf("redirects = %q", got)
	}
	// Without a domain there's nowhere to redirect to.
	v2, err := svc.SaveProject(ProjectInput{ServerID: "s1", Name: "b", RepoURL: "git@github.com:me/b.git", Runtime: "compose", Redirects: []string{"www.b.io"}})
	if err != nil || len(v2.Redirects) != 0 {
		t.Errorf("redirects without domains: %v %v", v2.Redirects, err)
	}
	// Another project can't take a redirect host as its domain.
	if _, err := svc.SaveProject(ProjectInput{ServerID: "s1", Name: "c", RepoURL: "git@github.com:me/c.git", Runtime: "compose", AppPort: 3001,
		Domains: []string{"www.shop.io"}}); err == nil {
		t.Error("expected a conflict with web's redirect")
	}
}
