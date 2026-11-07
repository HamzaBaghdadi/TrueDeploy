package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"truedeploy/internal/gitprov"
)

func TestConnectSelfHostedGitLab(t *testing.T) {
	st := newTestStore(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-good" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.EscapedPath() {
		case "/user":
			json.NewEncoder(w).Encode(map[string]string{"username": "dev", "name": "Dev"})
		case "/projects":
			json.NewEncoder(w).Encode([]map[string]any{{"path_with_namespace": "team/api", "ssh_url_to_repo": "git@git.example.com:team/api.git", "default_branch": "main"}})
		case "/projects/team%2Fapi/repository/branches":
			json.NewEncoder(w).Encode([]map[string]string{{"name": "main"}, {"name": "feature/x"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer api.Close()

	gh := NewGitHubService(st)
	git := NewGitService(st, gh)
	git.apiBase = func(gitprov.Account) string { return api.URL }

	if _, err := git.Connect(GitConnectInput{Kind: "gitlab", BaseURL: "git.example.com", Token: "bad"}); err == nil {
		t.Fatal("a rejected token must not connect")
	}
	if _, err := git.Connect(GitConnectInput{Kind: "gitea", Token: "x"}); err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("Gitea needs an address: %v", err)
	}
	if _, err := git.Connect(GitConnectInput{Kind: "bitbucket", Token: "x"}); err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("Bitbucket needs a username: %v", err)
	}
	v, err := git.Connect(GitConnectInput{Kind: "gitlab", BaseURL: "https://git.example.com/", Token: " glpat-good "})
	if err != nil || v.Host != "git.example.com" || v.Login != "dev" || v.CIName != "GitLab CI/CD" {
		t.Fatalf("%+v %v", v, err)
	}
	accts, _ := git.Accounts()
	if len(accts) != 1 || accts[0].Web != "https://git.example.com" {
		t.Fatalf("accounts: %+v", accts)
	}
	repos, err := git.Repos("git.example.com")
	if err != nil || len(repos) != 1 || repos[0].FullName != "team/api" {
		t.Fatalf("repos: %+v %v", repos, err)
	}

	// A project on that host: provider known, deploy key page and branches through the API.
	svc := NewProjectService(st, gh)
	svc.git = git
	p, err := svc.SaveProject(ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@git.example.com:team/api.git", Runtime: "compose"})
	if err != nil {
		t.Fatal(err)
	}
	if p.GitKind != "gitlab" || !p.GitConnected || p.DeployKeyURL != "https://git.example.com/team/api/-/settings/repository#js-deploy-keys-settings" {
		t.Errorf("view: kind=%q connected=%v key=%q", p.GitKind, p.GitConnected, p.DeployKeyURL)
	}
	if bs, err := svc.Branches(p.ID); err != nil || strings.Join(bs, ",") != "main,feature/x" {
		t.Errorf("branches: %v %v", bs, err)
	}

	// Unknown hosts aren't connected; cloud hosts are still recognised.
	other, _ := svc.SaveProject(ProjectInput{Name: "bb", ServerID: "s1", RepoURL: "git@bitbucket.org:ws/bb.git", Runtime: "compose"})
	if other.GitKind != "bitbucket" || other.GitConnected {
		t.Errorf("bitbucket view: %+v", other)
	}
	if _, err := svc.Branches(other.ID); err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Errorf("branches without a connection: %v", err)
	}

	if err := git.Disconnect("git.example.com"); err != nil {
		t.Fatal(err)
	}
	if accts, _ := git.Accounts(); len(accts) != 0 {
		t.Errorf("after disconnect: %+v", accts)
	}
}
