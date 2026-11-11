package services

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"truedeploy/internal/github"
	"truedeploy/internal/store"
)

func TestGitHubConnectStoresTokenEncrypted(t *testing.T) {
	st := newTestStore(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_good" {
			w.WriteHeader(401)
			w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		w.Write([]byte(`{"login":"deployer","name":"Deployer"}`))
	}))
	defer api.Close()

	svc := NewGitHubService(st)
	svc.newClient = func(tok string) *github.Client { return github.New(tok).WithBaseURL(api.URL) }

	if _, err := svc.Connect("ghp_bad"); err == nil {
		t.Fatal("a rejected token must not connect")
	}
	if s, _ := svc.Status(); s.Connected {
		t.Fatal("still disconnected after a failed connect")
	}

	s, err := svc.Connect("  ghp_good \n")
	if err != nil || !s.Connected || s.Login != "deployer" {
		t.Fatalf("Connect = %+v, %v", s, err)
	}
	dir := os.Getenv("AppData")
	filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "ghp_good") {
				t.Errorf("%s contains the plaintext token", p)
			}
		}
		return nil
	})

	if err := svc.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if s, _ := svc.Status(); s.Connected {
		t.Fatal("Disconnect should forget the account")
	}
	if _, err := svc.Repos(); err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("calls without a connection should point to Settings, got %v", err)
	}
}

func TestDetectThroughGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/me/shop/git/trees/main":
			w.Write([]byte(`{"tree":[{"path":"package.json","type":"blob"},{"path":"next.config.mjs","type":"blob"},{"path":"app/page.tsx","type":"blob"}]}`))
		case "/repos/me/shop/contents/package.json":
			w.Write([]byte(`{"sha":"1","content":"` + base64.StdEncoding.EncodeToString([]byte(`{"dependencies":{"next":"15"}}`)) + `"}`))
		case "/repos/me/shop/contents/next.config.mjs":
			w.Write([]byte(`{"sha":"2","content":"` + base64.StdEncoding.EncodeToString([]byte(`export default { output: "export" }`)) + `"}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	st := newTestStore(t)
	gh := NewGitHubService(st)
	gh.newClient = func(token string) *github.Client { return github.New(token).WithBaseURL(srv.URL) }
	if err := st.SetGitHub(&store.GitHubAccount{Login: "me", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	svc := NewProjectService(st, gh)
	res, err := svc.DetectFramework(DetectInput{ServerID: "s1", RepoURL: "git@github.com:me/shop.git"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "GitHub" || len(res.Suggestions) != 1 || res.Suggestions[0].Preset != "nextjs-export" {
		t.Errorf("result: %+v", res)
	}
	if _, err := svc.DetectFramework(DetectInput{ServerID: "s1", RepoURL: "not a repo"}); err == nil {
		t.Error("invalid URL accepted")
	}
}
