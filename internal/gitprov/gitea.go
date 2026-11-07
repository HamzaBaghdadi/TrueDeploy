package gitprov

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// gitea talks to Gitea and Forgejo (Codeberg or self-hosted), API v1, with
// an access token (repository and organization read/write, user read).
type gitea struct {
	api
	web string
}

func newGitea(a Account, apiBase string) *gitea {
	if apiBase == "" {
		apiBase = a.Web() + "/api/v1"
	}
	tok := a.Token
	return &gitea{web: a.Web(), api: api{kind: Gitea, base: strings.TrimRight(apiBase, "/"), http: newHTTP(),
		auth: func(r *http.Request) { r.Header.Set("Authorization", "token "+tok) },
		message: func(_ int, raw []byte) string {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &e)
			return e.Message
		}}}
}

func gtRepo(repo string) string {
	o, n := SplitOwner(repo)
	return "/repos/" + url.PathEscape(o) + "/" + url.PathEscape(n)
}

func (g *gitea) Kind() Kind { return Gitea }

func (g *gitea) Me(ctx context.Context) (*User, error) {
	var u struct {
		Login     string `json:"login"`
		FullName  string `json:"full_name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := g.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &User{Login: u.Login, Name: u.FullName, AvatarURL: u.AvatarURL}, nil
}

type gtRepository struct {
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	SSHURL        string    `json:"ssh_url"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	UpdatedAt     time.Time `json:"updated_at"`
	Permissions   struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

func (r gtRepository) repo() Repo {
	return Repo{FullName: r.FullName, Private: r.Private, SSHURL: r.SSHURL, CloneURL: r.CloneURL, HTMLURL: r.HTMLURL,
		DefaultBranch: r.DefaultBranch, Description: r.Description, PushedAt: r.UpdatedAt, CanAdmin: r.Permissions.Admin}
}

func (g *gitea) Repos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 6; page++ {
		var rs []gtRepository
		if err := g.do(ctx, http.MethodGet, fmt.Sprintf("/user/repos?limit=50&page=%d", page), nil, &rs); err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, r.repo())
		}
		if len(rs) < 50 {
			break
		}
	}
	return out, nil
}

func (g *gitea) Owners(ctx context.Context, me string) ([]string, error) {
	var orgs []struct {
		Username string `json:"username"`
	}
	if err := g.do(ctx, http.MethodGet, "/user/orgs?limit=50", nil, &orgs); err != nil {
		return nil, err
	}
	out := []string{me}
	for _, o := range orgs {
		out = append(out, o.Username)
	}
	return out, nil
}

func (g *gitea) CreateRepo(ctx context.Context, r NewRepo, me string) (*Repo, error) {
	path := "/user/repos"
	if r.Owner != "" && r.Owner != me {
		path = "/orgs/" + url.PathEscape(r.Owner) + "/repos"
	}
	var created gtRepository
	if err := g.do(ctx, http.MethodPost, path, map[string]any{"name": r.Name, "description": r.Description, "private": r.Private, "auto_init": true, "default_branch": "main"}, &created); err != nil {
		return nil, err
	}
	v := created.repo()
	v.CanAdmin = true
	return &v, nil
}

func (g *gitea) AddDeployKey(ctx context.Context, repo, title, key string) (bool, error) {
	var keys []struct {
		Key string `json:"key"`
	}
	if err := g.do(ctx, http.MethodGet, gtRepo(repo)+"/keys?limit=50", nil, &keys); err != nil {
		return false, err
	}
	for _, k := range keys {
		if keyBody(k.Key) == keyBody(key) {
			return false, nil
		}
	}
	err := g.do(ctx, http.MethodPost, gtRepo(repo)+"/keys", map[string]any{"title": title, "key": key, "read_only": true}, nil)
	return err == nil, err
}

func (g *gitea) DeployKeyPage(repo string) string { return g.web + "/" + repo + "/settings/keys" }

func (g *gitea) Branches(ctx context.Context, repo string) ([]string, error) {
	var bs []struct {
		Name string `json:"name"`
	}
	if err := g.do(ctx, http.MethodGet, gtRepo(repo)+"/branches?limit=50", nil, &bs); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out, nil
}

func (g *gitea) Tree(ctx context.Context, repo, ref string) ([]string, bool, error) {
	var t struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := g.do(ctx, http.MethodGet, gtRepo(repo)+"/git/trees/"+url.PathEscape(ref)+"?recursive=true&per_page=10000", nil, &t); err != nil {
		return nil, false, err
	}
	var paths []string
	for _, e := range t.Tree {
		if e.Type == "blob" {
			paths = append(paths, e.Path)
		}
	}
	return paths, t.Truncated, nil
}

func (g *gitea) File(ctx context.Context, repo, path, ref string) (string, error) {
	c, _, err := g.GetFile(ctx, repo, path, ref)
	return c, err
}

// contents returns a file's sha and decoded content ("" sha when missing).
func (g *gitea) contents(ctx context.Context, repo, path, ref string) (sha, content string, err error) {
	var c struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	err = g.do(ctx, http.MethodGet, gtRepo(repo)+"/contents/"+escapePath(path)+"?ref="+url.QueryEscape(ref), nil, &c)
	if IsNotFound(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	b, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(c.Content, "\n", ""))
	return c.SHA, string(b), nil
}

func (g *gitea) GetFile(ctx context.Context, repo, path, ref string) (string, bool, error) {
	sha, content, err := g.contents(ctx, repo, path, ref)
	return content, sha != "", err
}

func (g *gitea) PutFile(ctx context.Context, repo, path, branch, content, msg string) (bool, error) {
	sha, cur, err := g.contents(ctx, repo, path, branch)
	if err != nil {
		return false, err
	}
	if sha != "" && cur == content {
		return false, nil
	}
	body := map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(content)), "message": msg, "branch": branch}
	method := http.MethodPost
	if sha != "" {
		method, body["sha"] = http.MethodPut, sha
	}
	err = g.do(ctx, method, gtRepo(repo)+"/contents/"+escapePath(path), body, nil)
	return err == nil, err
}

func (g *gitea) DeleteFile(ctx context.Context, repo, path, branch, msg string) error {
	sha, _, err := g.contents(ctx, repo, path, branch)
	if err != nil || sha == "" {
		return err
	}
	return g.do(ctx, http.MethodDelete, gtRepo(repo)+"/contents/"+escapePath(path), map[string]string{"message": msg, "branch": branch, "sha": sha}, nil)
}

// SetSecrets stores Actions secrets (Gitea 1.21+, Forgejo 1.21+).
func (g *gitea) SetSecrets(ctx context.Context, repo string, secrets map[string]string) error {
	for k, v := range secrets {
		if err := g.do(ctx, http.MethodPut, gtRepo(repo)+"/actions/secrets/"+url.PathEscape(k), map[string]string{"data": v}, nil); err != nil {
			if IsNotFound(err) {
				return fmt.Errorf("couldn't save Actions secrets. Is Actions turned on for %s (Settings → Repository → Actions), and is the server Gitea 1.21 or newer?", repo)
			}
			return fmt.Errorf("couldn't save the Actions secret %s: %w", k, err)
		}
	}
	return nil
}

func (g *gitea) DeleteSecrets(ctx context.Context, repo string, names []string) error {
	for _, k := range names {
		if err := g.do(ctx, http.MethodDelete, gtRepo(repo)+"/actions/secrets/"+url.PathEscape(k), nil, nil); err != nil && !IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (g *gitea) PrepareCI(context.Context, string) error { return nil }
