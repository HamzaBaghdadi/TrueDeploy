package gitprov

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestParseRepoURL(t *testing.T) {
	cases := map[string][2]string{
		"git@github.com:me/api.git":                     {"github.com", "me/api"},
		"https://gitlab.com/group/sub/app":              {"gitlab.com", "group/sub/app"},
		"https://gitlab.com/group/sub/app.git/":         {"gitlab.com", "group/sub/app"},
		"ssh://git@git.example.com:2222/team/web.git":   {"git.example.com", "team/web"},
		"git@bitbucket.org:workspace/shop.git":          {"bitbucket.org", "workspace/shop"},
		"https://user@bitbucket.org/workspace/shop.git": {"bitbucket.org", "workspace/shop"},
		"https://Codeberg.org/me/site":                  {"codeberg.org", "me/site"},
	}
	for in, want := range cases {
		h, p, ok := ParseRepoURL(in)
		if !ok || h != want[0] || p != want[1] {
			t.Errorf("%s: %q %q %v", in, h, p, ok)
		}
	}
	for _, bad := range []string{"", "not a url", "https://github.com/onlyowner", "git@host:../../etc", "https://h/a/../b"} {
		if _, _, ok := ParseRepoURL(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
	if o, n := SplitOwner("group/sub/app"); o != "group/sub" || n != "app" {
		t.Error(o, n)
	}
}

func TestAccountHost(t *testing.T) {
	if h := (Account{Kind: GitLab}).Host(); h != "gitlab.com" {
		t.Error(h)
	}
	if h := (Account{Kind: GitLab, BaseURL: "https://Git.Example.com/"}).Host(); h != "git.example.com" {
		t.Error(h)
	}
	if _, err := New(Account{Kind: Gitea}, ""); err == nil {
		t.Error("Gitea needs an address")
	}
}

// fake is a tiny scripted API: handlers by "METHOD path" (path without query).
type fake struct {
	t        *testing.T
	mu       sync.Mutex
	routes   map[string]func(w http.ResponseWriter, r *http.Request, body []byte)
	calls    []string
	wantAuth func(*http.Request) bool
}

func newFake(t *testing.T, auth func(*http.Request) bool) (*fake, *httptest.Server) {
	f := &fake{t: t, routes: map[string]func(http.ResponseWriter, *http.Request, []byte){}, wantAuth: auth}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) on(route string, h func(w http.ResponseWriter, r *http.Request, body []byte)) {
	f.routes[route] = h
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	key := r.Method + " " + r.URL.EscapedPath()
	f.calls = append(f.calls, key)
	if !f.wantAuth(r) {
		w.WriteHeader(401)
		return
	}
	if h, ok := f.routes[key]; ok {
		h(w, r, body)
		return
	}
	w.WriteHeader(404)
	io.WriteString(w, `{"message":"404 Not found"}`)
}

func reply(v any) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, _ *http.Request, _ []byte) { json.NewEncoder(w).Encode(v) }
}

func jsonBody(t *testing.T, b []byte) map[string]any {
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q: %v", b, err)
	}
	return m
}

func TestGitLab(t *testing.T) {
	f, srv := newFake(t, func(r *http.Request) bool { return r.Header.Get("PRIVATE-TOKEN") == "glpat" })
	p, _ := New(Account{Kind: GitLab, Token: "glpat"}, srv.URL)
	ctx := context.Background()
	const proj = "/projects/group%2Fsub%2Fapp"

	f.on("GET /user", reply(map[string]string{"username": "dev", "name": "Dev"}))
	if u, err := p.Me(ctx); err != nil || u.Login != "dev" {
		t.Fatalf("%+v %v", u, err)
	}
	f.on("GET /projects", reply([]map[string]any{{"path_with_namespace": "group/sub/app", "visibility": "private", "ssh_url_to_repo": "git@gitlab.com:group/sub/app.git",
		"default_branch": "main", "permissions": map[string]any{"group_access": map[string]int{"access_level": 40}}}}))
	rs, err := p.Repos(ctx)
	if err != nil || len(rs) != 1 || !rs[0].CanAdmin || !rs[0].Private || rs[0].SSHURL == "" {
		t.Fatalf("%+v %v", rs, err)
	}

	// Deploy key: added once, then recognised.
	var keys []map[string]string
	f.on("GET "+proj+"/deploy_keys", func(w http.ResponseWriter, _ *http.Request, _ []byte) { json.NewEncoder(w).Encode(keys) })
	f.on("POST "+proj+"/deploy_keys", func(w http.ResponseWriter, _ *http.Request, b []byte) {
		m := jsonBody(t, b)
		if m["can_push"] != false {
			t.Error("deploy keys must be read-only")
		}
		keys = append(keys, map[string]string{"key": m["key"].(string)})
		w.WriteHeader(201)
	})
	if added, err := p.AddDeployKey(ctx, "group/sub/app", "server", "ssh-ed25519 AAAA td"); err != nil || !added {
		t.Fatal(added, err)
	}
	if added, err := p.AddDeployKey(ctx, "group/sub/app", "server", "ssh-ed25519 AAAA other-comment"); err != nil || added {
		t.Fatal("same key should be recognised", added, err)
	}

	// Files: create, unchanged, update.
	files := map[string]string{}
	const fpath = proj + "/repository/files/.gitlab%2Ftruedeploy-app.yml"
	f.on("GET "+fpath+"/raw", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		c, ok := files["x"]
		if !ok {
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, c)
	})
	save := func(w http.ResponseWriter, r *http.Request, b []byte) {
		m := jsonBody(t, b)
		if m["branch"] != "main" || m["commit_message"] == "" {
			t.Errorf("file body %v", m)
		}
		files["x"] = m["content"].(string)
	}
	f.on("POST "+fpath, save)
	f.on("PUT "+fpath, save)
	for i, want := range []bool{true, false} {
		if ch, err := p.PutFile(ctx, "group/sub/app", ".gitlab/truedeploy-app.yml", "main", "v1", "msg"); err != nil || ch != want {
			t.Fatalf("put %d: %v %v", i, ch, err)
		}
	}
	if ch, _ := p.PutFile(ctx, "group/sub/app", ".gitlab/truedeploy-app.yml", "main", "v2", "msg"); !ch || files["x"] != "v2" {
		t.Fatal("update failed")
	}

	// Variables: PUT, falling back to POST when new; raw so $ isn't expanded.
	vars := map[string]bool{}
	f.on("PUT "+proj+"/variables/TD_KEY", func(w http.ResponseWriter, _ *http.Request, b []byte) {
		if !vars["TD_KEY"] {
			w.WriteHeader(404)
			return
		}
		if jsonBody(t, b)["raw"] != true {
			t.Error("variables must be raw")
		}
	})
	f.on("POST "+proj+"/variables", func(w http.ResponseWriter, _ *http.Request, b []byte) { vars[jsonBody(t, b)["key"].(string)] = true })
	for i := 0; i < 2; i++ {
		if err := p.SetSecrets(ctx, "group/sub/app", map[string]string{"TD_KEY": "a$b"}); err != nil {
			t.Fatal(err)
		}
	}
	if !vars["TD_KEY"] {
		t.Error("variable not created")
	}
	if !strings.Contains(p.DeployKeyPage("group/app"), "gitlab.com/group/app/-/settings/repository") {
		t.Error(p.DeployKeyPage("group/app"))
	}

	bad, _ := New(Account{Kind: GitLab, Token: "nope"}, srv.URL)
	if _, err := bad.Me(ctx); err == nil || !strings.Contains(err.Error(), "GitLab rejected the token") {
		t.Errorf("bad token: %v", err)
	}
}

func TestBitbucket(t *testing.T) {
	f, srv := newFake(t, func(r *http.Request) bool {
		u, p, ok := r.BasicAuth()
		return ok && u == "me@example.com" && p == "ATBB"
	})
	p, _ := New(Account{Kind: Bitbucket, Username: "me@example.com", Token: "ATBB"}, srv.URL)
	ctx := context.Background()

	f.on("GET /repositories", func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Query().Get("page") == "" {
			json.NewEncoder(w).Encode(map[string]any{"values": []any{map[string]any{"full_name": "ws/one", "is_private": true,
				"links":      map[string]any{"clone": []any{map[string]string{"name": "ssh", "href": "git@bitbucket.org:ws/one.git"}, map[string]string{"name": "https", "href": "https://me@bitbucket.org/ws/one.git"}}},
				"mainbranch": map[string]string{"name": "develop"}}}, "next": srv.URL + "/repositories?role=member&page=2"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"values": []any{map[string]any{"full_name": "ws/two"}}})
	})
	rs, err := p.Repos(ctx)
	if err != nil || len(rs) != 2 || rs[0].DefaultBranch != "develop" || rs[0].CloneURL != "https://bitbucket.org/ws/one.git" || rs[1].DefaultBranch != "main" {
		t.Fatalf("%+v %v", rs, err)
	}

	// Files are committed with a form field named by the path.
	var committed url.Values
	f.on("GET /repositories/ws/one/src/main/bitbucket-pipelines.yml", func(w http.ResponseWriter, _ *http.Request, _ []byte) { w.WriteHeader(404) })
	f.on("POST /repositories/ws/one/src", func(w http.ResponseWriter, r *http.Request, b []byte) {
		committed, _ = url.ParseQuery(string(b))
		w.WriteHeader(201)
	})
	if ch, err := p.PutFile(ctx, "ws/one", "bitbucket-pipelines.yml", "main", "pipelines: {}\n", "Add"); err != nil || !ch {
		t.Fatal(ch, err)
	}
	if committed.Get("bitbucket-pipelines.yml") != "pipelines: {}\n" || committed.Get("branch") != "main" {
		t.Errorf("commit form: %v", committed)
	}

	// Variables: created secured, then updated by uuid.
	vars := []map[string]any{}
	f.on("GET /repositories/ws/one/pipelines_config/variables", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		json.NewEncoder(w).Encode(map[string]any{"values": vars})
	})
	f.on("POST /repositories/ws/one/pipelines_config/variables", func(w http.ResponseWriter, _ *http.Request, b []byte) {
		m := jsonBody(t, b)
		if m["secured"] != true {
			t.Error("variables must be secured")
		}
		vars = append(vars, map[string]any{"uuid": "{u1}", "key": m["key"]})
	})
	updated := false
	f.on("PUT /repositories/ws/one/pipelines_config/variables/%7Bu1%7D", func(http.ResponseWriter, *http.Request, []byte) { updated = true })
	for i := 0; i < 2; i++ {
		if err := p.SetSecrets(ctx, "ws/one", map[string]string{"TD_KEY": "v"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(vars) != 1 || !updated {
		t.Errorf("vars %v updated %v", vars, updated)
	}
	enabled := false
	f.on("PUT /repositories/ws/one/pipelines_config", func(_ http.ResponseWriter, _ *http.Request, b []byte) { enabled = jsonBody(t, b)["enabled"] == true })
	if err := p.PrepareCI(ctx, "ws/one"); err != nil || !enabled {
		t.Error("pipelines should be turned on", err)
	}
}

func TestGitea(t *testing.T) {
	f, srv := newFake(t, func(r *http.Request) bool { return r.Header.Get("Authorization") == "token gt" })
	p, _ := New(Account{Kind: Gitea, BaseURL: "https://codeberg.org", Token: "gt"}, srv.URL)
	ctx := context.Background()

	var content, sha string
	f.on("GET /repos/me/site/contents/.gitea/workflows/truedeploy-site.yml", func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		if sha == "" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"sha": sha, "content": base64.StdEncoding.EncodeToString([]byte(content))})
	})
	write := func(w http.ResponseWriter, r *http.Request, b []byte) {
		m := jsonBody(t, b)
		if r.Method == http.MethodPut && m["sha"] != sha {
			t.Error("update needs the current sha")
		}
		raw, _ := base64.StdEncoding.DecodeString(m["content"].(string))
		content, sha = string(raw), "s"+string(raw)
	}
	f.on("POST /repos/me/site/contents/.gitea/workflows/truedeploy-site.yml", write)
	f.on("PUT /repos/me/site/contents/.gitea/workflows/truedeploy-site.yml", write)
	for _, c := range []string{"one", "one", "two"} {
		if _, err := p.PutFile(ctx, "me/site", ".gitea/workflows/truedeploy-site.yml", "main", c, "m"); err != nil {
			t.Fatal(err)
		}
	}
	if content != "two" {
		t.Error(content)
	}
	f.on("PUT /repos/me/site/actions/secrets/TD_KEY", func(_ http.ResponseWriter, _ *http.Request, b []byte) {
		if jsonBody(t, b)["data"] != "secret" {
			t.Error("secret body")
		}
	})
	if err := p.SetSecrets(ctx, "me/site", map[string]string{"TD_KEY": "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSecrets(ctx, "me/site", map[string]string{"OTHER": "x"}); err == nil || !strings.Contains(err.Error(), "Actions turned on") {
		t.Errorf("missing Actions should explain: %v", err)
	}
	if got := p.DeployKeyPage("me/site"); got != "https://codeberg.org/me/site/settings/keys" {
		t.Error(got)
	}
}
