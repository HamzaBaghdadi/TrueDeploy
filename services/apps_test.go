package services

import (
	"strings"
	"testing"

	"truedeploy/internal/deploy"
)

func TestCreateAppAndEditKeepsItAnApp(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	// Another project already uses 3001 on this server.
	if _, err := svc.SaveProject(ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "compose", Domains: []string{"api.example.com"}, AppPort: 3001}); err != nil {
		t.Fatal(err)
	}

	v, err := svc.CreateApp(AppInput{Template: "uptime-kuma", ServerID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Name != "Uptime Kuma" || v.Slug != "uptime-kuma" || v.Template != "uptime-kuma" || v.RepoURL != "" || v.Branch != "" || v.AppPort != 3002 {
		t.Fatalf("unexpected app: %+v", v.Project)
	}
	d, _ := svc.GetProject(v.ID)
	if d.Env["PORT"] != "3002" || d.Env["BIND"] != "0.0.0.0" || d.Env["PUBLIC_URL"] != "http://h:3002" {
		t.Errorf("managed env without a domain: %v", d.Env)
	}

	// The edit form sends no template or compose; the app keeps both, and
	// adding a domain moves it behind the proxy.
	in := ProjectInput{ID: v.ID, Name: "Status", ServerID: "s1", Runtime: "dockerfile", RepoURL: "x", Domains: []string{"status.example.com"}, AppPort: 3002, Env: d.Env}
	e, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	d, _ = svc.GetProject(e.ID)
	if !d.IsApp() || d.Runtime != deploy.RuntimeCompose || d.RepoURL != "" || d.Slug != "uptime-kuma" {
		t.Errorf("edit must keep it an app: %+v", d.Project)
	}
	if d.Env["BIND"] != "127.0.0.1" || d.Env["PUBLIC_URL"] != "https://status.example.com" || d.Env["HTTPS"] != "true" {
		t.Errorf("managed env with a domain: %v", d.Env)
	}

	// Ports can't clash with another project on the server.
	in.AppPort = 3001
	if _, err := svc.SaveProject(in); err == nil || !strings.Contains(err.Error(), "already used by api") {
		t.Errorf("expected port clash, got %v", err)
	}

	// Repository features are refused for apps.
	if _, err := svc.CheckRepoAccess(v.ID); err != errNoRepo {
		t.Errorf("CheckRepoAccess: %v", err)
	}
	if _, err := svc.EnableAutoDeploy(v.ID); err != errNoRepo {
		t.Errorf("EnableAutoDeploy: %v", err)
	}

	// Secrets are generated per app; overrides win for normal variables.
	w, err := svc.CreateApp(AppInput{Template: "vaultwarden", Name: "Vault", ServerID: "s1", Port: 9999, Vars: map[string]string{"SIGNUPS_ALLOWED": "false"}})
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := svc.GetProject(w.ID)
	if len(wd.Env["ADMIN_TOKEN"]) != 48 || wd.Env["SIGNUPS_ALLOWED"] != "false" || wd.AppPort != 9999 {
		t.Errorf("vaultwarden env: %v", wd.Env)
	}
	sp, _ := svc.deploySpec(wd.Project, wd.Env)
	if sp.Compose == "" || sp.Network != "" || sp.Validate() != nil {
		t.Errorf("deploy spec: %+v", sp)
	}
	a, _ := svc.CreateApp(AppInput{Template: "adminer", ServerID: "s1"})
	ad, _ := svc.GetProject(a.ID)
	if sp, _ := svc.deploySpec(ad.Project, ad.Env); sp.Network == "" {
		t.Error("adminer must join the database network")
	}
	if _, err := svc.CreateApp(AppInput{Template: "nope", ServerID: "s1"}); err == nil {
		t.Error("unknown template accepted")
	}
}

func TestParseListening(t *testing.T) {
	got := parseListening([]string{"0.0.0.0:22", "[::]:443", "*:8080", "127.0.0.53%lo:53", "Local", ""})
	for _, p := range []int{22, 443, 8080, 53} {
		if !got[p] {
			t.Errorf("missing %d in %v", p, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("got %v", got)
	}
}
