package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub is an in-memory stand-in for the parts of the API we call.
type fakeGitHub struct {
	mu    sync.Mutex
	keys  map[string][]deployKey // "owner/repo" -> keys
	used  map[string]string      // key body -> repo it is on (GitHub-wide uniqueness)
	repos map[string]bool
	calls []string
}

func newFake(t *testing.T) (*fakeGitHub, *Client) {
	f := &fakeGitHub{keys: map[string][]deployKey{}, used: map[string]string{}, repos: map[string]bool{"me/existing": true}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, New("tok").WithBaseURL(srv.URL)
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(401)
		w.Write([]byte(`{"message":"Bad credentials"}`))
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch {
	case r.Method == "GET" && r.URL.Path == "/user":
		w.Write([]byte(`{"login":"me","name":"Me"}`))
	case r.Method == "POST" && (r.URL.Path == "/user/repos" || strings.HasPrefix(r.URL.Path, "/orgs/")):
		owner := "me"
		if strings.HasPrefix(r.URL.Path, "/orgs/") {
			owner = strings.Split(r.URL.Path, "/")[2]
		}
		full := owner + "/" + body["name"].(string)
		if f.repos[full] {
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"Repository creation failed.","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"}]}`))
			return
		}
		f.repos[full] = true
		if body["auto_init"] != true {
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"full_name": full, "name": body["name"], "private": body["private"],
			"ssh_url": "git@github.com:" + full + ".git", "default_branch": "main"})
	case strings.HasSuffix(r.URL.Path, "/keys"):
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/keys")
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(f.keys[repo])
			return
		}
		k := keyBody(body["key"].(string))
		if other, ok := f.used[k]; ok && other != repo {
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"PublicKey","code":"custom","field":"key","message":"key is already in use"}]}`))
			return
		}
		f.used[k] = repo
		f.keys[repo] = append(f.keys[repo], deployKey{Key: body["key"].(string), Title: body["title"].(string), ReadOnly: body["read_only"] == true})
		w.WriteHeader(201)
		w.Write([]byte(`{}`))
	default:
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"Not Found"}`))
	}
}

func TestMeAndBadToken(t *testing.T) {
	_, c := newFake(t)
	u, err := c.Me(context.Background())
	if err != nil || u.Login != "me" {
		t.Fatalf("Me = %+v, %v", u, err)
	}
	bad := New("wrong").WithBaseURL(c.baseURL)
	if _, err := bad.Me(context.Background()); err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("want friendly bad-token error, got %v", err)
	}
}

func TestCreateRepo(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	r, err := c.CreateRepo(ctx, NewRepo{Name: "api", Private: true}, "me")
	if err != nil || r.SSHURL != "git@github.com:me/api.git" || !r.Private {
		t.Fatalf("CreateRepo = %+v, %v", r, err)
	}
	if _, err := c.CreateRepo(ctx, NewRepo{Owner: "acme", Name: "web"}, "me"); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "POST /orgs/acme/repos" {
		t.Fatalf("org repo should POST to /orgs/acme/repos, calls: %v", f.calls)
	}
	if _, err := c.CreateRepo(ctx, NewRepo{Name: "existing"}, "me"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists error, got %v", err)
	}
	if _, err := c.CreateRepo(ctx, NewRepo{Name: "bad name!"}, "me"); err == nil {
		t.Fatal("invalid names must be rejected before calling GitHub")
	}
}

func TestAddDeployKey(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	key := "ssh-ed25519 AAAAC3NzaKEY truedeploy@vps"

	added, err := c.AddDeployKey(ctx, "me", "api", "TrueDeploy (vps)", key)
	if err != nil || !added {
		t.Fatalf("first add = %v, %v", added, err)
	}
	if k := f.keys["me/api"][0]; !k.ReadOnly {
		t.Fatal("deploy keys must be read-only")
	}
	// Same key again (even with a different comment) is a no-op.
	added, err = c.AddDeployKey(ctx, "me", "api", "x", "ssh-ed25519 AAAAC3NzaKEY other-comment")
	if err != nil || added {
		t.Fatalf("re-add should be a no-op, got %v, %v", added, err)
	}
	// GitHub refuses one key on two repos; explain it.
	_, err = c.AddDeployKey(ctx, "me", "web", "x", key)
	if err == nil || !strings.Contains(err.Error(), "one repository only") {
		t.Fatalf("want key-in-use explanation, got %v", err)
	}
}

func TestParseRepoURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/api.git":          "acme/api",
		"git@github.com:acme/api":              "acme/api",
		"https://github.com/acme/api":          "acme/api",
		"https://github.com/acme/api.git":      "acme/api",
		"ssh://git@github.com/acme/my.app.git": "acme/my.app",
	}
	for in, want := range cases {
		o, r, ok := ParseRepoURL(in)
		if !ok || o+"/"+r != want {
			t.Errorf("ParseRepoURL(%q) = %s/%s %v, want %s", in, o, r, ok, want)
		}
	}
	for _, in := range []string{"git@gitlab.com:a/b.git", "https://example.com/a/b", "nope"} {
		if _, _, ok := ParseRepoURL(in); ok {
			t.Errorf("%q is not a GitHub URL", in)
		}
	}
}

func TestTreeAndFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/me/shop/git/trees/main":
			if r.URL.Query().Get("recursive") != "1" {
				t.Error("tree must be recursive")
			}
			w.Write([]byte(`{"tree":[{"path":"web","type":"tree"},{"path":"web/package.json","type":"blob"}],"truncated":false}`))
		case "/repos/me/shop/branches":
			w.Write([]byte(`[{"name":"main"},{"name":"feature/login"}]`))
		case "/repos/me/shop/contents/web/package.json":
			w.Write([]byte(`{"sha":"1","content":"eyJuYW1lIjoid2ViIn0=\n"}`))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	defer srv.Close()
	c := New("t").WithBaseURL(srv.URL)
	paths, trunc, err := c.Tree(context.Background(), "me", "shop", "main")
	if err != nil || trunc || len(paths) != 1 || paths[0] != "web/package.json" {
		t.Fatalf("tree: %v %v %v", paths, trunc, err)
	}
	if s, err := c.File(context.Background(), "me", "shop", "web/package.json", "main"); err != nil || s != `{"name":"web"}` {
		t.Errorf("file: %q %v", s, err)
	}
	if s, err := c.File(context.Background(), "me", "shop", "missing", "main"); err != nil || s != "" {
		t.Errorf("missing file: %q %v", s, err)
	}
}

func TestBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/me/shop/branches" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("unexpected %s", r.URL)
		}
		w.Write([]byte(`[{"name":"main"},{"name":"feature/login"}]`))
	}))
	defer srv.Close()
	got, err := New("t").WithBaseURL(srv.URL).Branches(context.Background(), "me", "shop")
	if err != nil || strings.Join(got, ",") != "main,feature/login" {
		t.Errorf("%v %v", got, err)
	}
}
