package gitprov

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// bitbucket talks to Bitbucket Cloud's API 2.0 with Basic auth: an
// Atlassian API token (or app password) and the matching email/username.
type bitbucket struct{ api }

func newBitbucket(a Account, apiBase string) *bitbucket {
	if apiBase == "" {
		apiBase = "https://api.bitbucket.org/2.0"
	}
	user, tok := a.Username, a.Token
	return &bitbucket{api{kind: Bitbucket, base: strings.TrimRight(apiBase, "/"), http: newHTTP(),
		auth:    func(r *http.Request) { r.SetBasicAuth(user, tok) },
		message: bitbucketMessage}}
}

func bitbucketMessage(_ int, raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Detail  any    `json:"detail"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	if d, ok := e.Error.Detail.(string); ok && d != "" {
		return e.Error.Message + ": " + d
	}
	return e.Error.Message
}

func bbRepo(repo string) string {
	o, n := SplitOwner(repo)
	return "/repositories/" + url.PathEscape(o) + "/" + url.PathEscape(n)
}

func (b *bitbucket) Kind() Kind { return Bitbucket }

func (b *bitbucket) Me(ctx context.Context) (*User, error) {
	var u struct {
		Username    string `json:"username"`
		Nickname    string `json:"nickname"`
		DisplayName string `json:"display_name"`
		Links       struct {
			Avatar struct {
				Href string `json:"href"`
			} `json:"avatar"`
		} `json:"links"`
	}
	if err := b.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, err
	}
	login := u.Username
	if login == "" {
		login = u.Nickname
	}
	return &User{Login: login, Name: u.DisplayName, AvatarURL: u.Links.Avatar.Href}, nil
}

type bbRepository struct {
	FullName    string    `json:"full_name"`
	IsPrivate   bool      `json:"is_private"`
	Description string    `json:"description"`
	UpdatedOn   time.Time `json:"updated_on"`
	MainBranch  *struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	Links struct {
		Clone []struct {
			Name string `json:"name"`
			Href string `json:"href"`
		} `json:"clone"`
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

func (r bbRepository) repo() Repo {
	v := Repo{FullName: r.FullName, Private: r.IsPrivate, Description: r.Description, PushedAt: r.UpdatedOn,
		HTMLURL: r.Links.HTML.Href, CanAdmin: true, DefaultBranch: "main"}
	if r.MainBranch != nil && r.MainBranch.Name != "" {
		v.DefaultBranch = r.MainBranch.Name
	}
	for _, c := range r.Links.Clone {
		switch c.Name {
		case "ssh":
			v.SSHURL = c.Href
		case "https":
			// Drop the "user@" Bitbucket puts in clone links.
			if u, err := url.Parse(c.Href); err == nil {
				u.User = nil
				v.CloneURL = u.String()
			}
		}
	}
	return v
}

// pages follows Bitbucket's "next" links (up to max pages).
func (b *bitbucket) pages(ctx context.Context, path string, max int, each func(json.RawMessage) error) error {
	for i := 0; i < max && path != ""; i++ {
		var page struct {
			Values []json.RawMessage `json:"values"`
			Next   string            `json:"next"`
		}
		if err := b.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return err
		}
		for _, v := range page.Values {
			if err := each(v); err != nil {
				return err
			}
		}
		path = strings.TrimPrefix(page.Next, b.base)
		if page.Next != "" && path == page.Next {
			return nil // a next link on another host: stop rather than follow it
		}
	}
	return nil
}

func (b *bitbucket) Repos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	err := b.pages(ctx, "/repositories?role=member&sort=-updated_on&pagelen=100", 3, func(raw json.RawMessage) error {
		var r bbRepository
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		out = append(out, r.repo())
		return nil
	})
	return out, err
}

func (b *bitbucket) Owners(ctx context.Context, me string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	err := b.pages(ctx, "/user/permissions/workspaces?pagelen=100", 2, func(raw json.RawMessage) error {
		var p struct {
			Workspace struct {
				Slug string `json:"slug"`
			} `json:"workspace"`
		}
		if json.Unmarshal(raw, &p) == nil && p.Workspace.Slug != "" && !seen[p.Workspace.Slug] {
			seen[p.Workspace.Slug] = true
			out = append(out, p.Workspace.Slug)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The personal workspace (usually named like the user) goes first.
	for i, w := range out {
		if w == me {
			out[0], out[i] = out[i], out[0]
		}
	}
	if len(out) == 0 {
		out = []string{me}
	}
	return out, nil
}

func (b *bitbucket) CreateRepo(ctx context.Context, r NewRepo, me string) (*Repo, error) {
	ws := r.Owner
	if ws == "" {
		ws = me
	}
	slug := strings.ToLower(r.Name)
	var created bbRepository
	if err := b.do(ctx, http.MethodPost, "/repositories/"+url.PathEscape(ws)+"/"+url.PathEscape(slug),
		map[string]any{"scm": "git", "is_private": r.Private, "description": r.Description}, &created); err != nil {
		return nil, err
	}
	v := created.repo()
	return &v, nil
}

func (b *bitbucket) AddDeployKey(ctx context.Context, repo, title, key string) (bool, error) {
	exists := false
	if err := b.pages(ctx, bbRepo(repo)+"/deploy-keys?pagelen=100", 3, func(raw json.RawMessage) error {
		var k struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &k) == nil && keyBody(k.Key) == keyBody(key) {
			exists = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	err := b.do(ctx, http.MethodPost, bbRepo(repo)+"/deploy-keys", map[string]string{"key": keyBody(key), "label": title}, nil)
	return err == nil, err
}

func (b *bitbucket) DeployKeyPage(repo string) string {
	return "https://bitbucket.org/" + repo + "/admin/access-keys/"
}

func (b *bitbucket) Branches(ctx context.Context, repo string) ([]string, error) {
	var out []string
	err := b.pages(ctx, bbRepo(repo)+"/refs/branches?pagelen=100", 2, func(raw json.RawMessage) error {
		var br struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &br) == nil {
			out = append(out, br.Name)
		}
		return nil
	})
	return out, err
}

func (b *bitbucket) Tree(ctx context.Context, repo, ref string) ([]string, bool, error) {
	var paths []string
	n := 0
	err := b.pages(ctx, bbRepo(repo)+"/src/"+url.PathEscape(ref)+"/?max_depth=4&pagelen=100", 10, func(raw json.RawMessage) error {
		n++
		var e struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Type == "commit_file" {
			paths = append(paths, e.Path)
		}
		return nil
	})
	return paths, n >= 1000, err
}

func (b *bitbucket) File(ctx context.Context, repo, path, ref string) (string, error) {
	c, _, err := b.GetFile(ctx, repo, path, ref)
	return c, err
}

func (b *bitbucket) GetFile(ctx context.Context, repo, path, ref string) (string, bool, error) {
	var s string
	err := b.do(ctx, http.MethodGet, bbRepo(repo)+"/src/"+url.PathEscape(ref)+"/"+escapePath(path), nil, &s)
	if IsNotFound(err) {
		return "", false, nil
	}
	return s, err == nil, err
}

// PutFile commits through the src endpoint (a form field named by the path).
func (b *bitbucket) PutFile(ctx context.Context, repo, path, branch, content, msg string) (bool, error) {
	cur, exists, err := b.GetFile(ctx, repo, path, branch)
	if err != nil {
		return false, err
	}
	if exists && cur == content {
		return false, nil
	}
	form := url.Values{"message": {msg}, "branch": {branch}, path: {content}}
	err = b.do(ctx, http.MethodPost, bbRepo(repo)+"/src", formBody{form}, nil)
	return err == nil, err
}

func (b *bitbucket) DeleteFile(ctx context.Context, repo, path, branch, msg string) error {
	_, exists, err := b.GetFile(ctx, repo, path, branch)
	if err != nil || !exists {
		return err
	}
	return b.do(ctx, http.MethodPost, bbRepo(repo)+"/src", formBody{url.Values{"message": {msg}, "branch": {branch}, "files": {path}}}, nil)
}

// variables maps repository pipeline variable names to their uuids.
func (b *bitbucket) variables(ctx context.Context, repo string) (map[string]string, error) {
	m := map[string]string{}
	err := b.pages(ctx, bbRepo(repo)+"/pipelines_config/variables?pagelen=100", 5, func(raw json.RawMessage) error {
		var v struct {
			UUID string `json:"uuid"`
			Key  string `json:"key"`
		}
		if json.Unmarshal(raw, &v) == nil {
			m[v.Key] = v.UUID
		}
		return nil
	})
	return m, err
}

// SetSecrets stores secured repository variables for Pipelines.
func (b *bitbucket) SetSecrets(ctx context.Context, repo string, secrets map[string]string) error {
	existing, err := b.variables(ctx, repo)
	if err != nil {
		return err
	}
	for k, v := range secrets {
		body := map[string]any{"key": k, "value": v, "secured": true}
		if id, ok := existing[k]; ok {
			err = b.do(ctx, http.MethodPut, bbRepo(repo)+"/pipelines_config/variables/"+url.PathEscape(id), body, nil)
		} else {
			err = b.do(ctx, http.MethodPost, bbRepo(repo)+"/pipelines_config/variables", body, nil)
		}
		if err != nil {
			return fmt.Errorf("couldn't save the repository variable %s: %w", k, err)
		}
	}
	return nil
}

func (b *bitbucket) DeleteSecrets(ctx context.Context, repo string, names []string) error {
	existing, err := b.variables(ctx, repo)
	if err != nil {
		return err
	}
	for _, k := range names {
		if id, ok := existing[k]; ok {
			if err := b.do(ctx, http.MethodDelete, bbRepo(repo)+"/pipelines_config/variables/"+url.PathEscape(id), nil, nil); err != nil && !IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// PrepareCI turns Pipelines on for the repository (it's off by default).
func (b *bitbucket) PrepareCI(ctx context.Context, repo string) error {
	if err := b.do(ctx, http.MethodPut, bbRepo(repo)+"/pipelines_config", map[string]bool{"enabled": true}, nil); err != nil {
		return fmt.Errorf("couldn't turn on Bitbucket Pipelines for %s: %w", repo, err)
	}
	return nil
}
