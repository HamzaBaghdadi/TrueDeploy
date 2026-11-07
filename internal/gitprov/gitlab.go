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

// gitlab talks to GitLab's REST API v4 (gitlab.com or self-hosted) with a
// personal access token (scope "api").
type gitlab struct {
	api
	web string
}

func newGitLab(a Account, apiBase string) *gitlab {
	if apiBase == "" {
		apiBase = a.Web() + "/api/v4"
	}
	tok := a.Token
	return &gitlab{web: a.Web(), api: api{kind: GitLab, base: strings.TrimRight(apiBase, "/"), http: newHTTP(),
		auth:    func(r *http.Request) { r.Header.Set("PRIVATE-TOKEN", tok) },
		message: gitlabMessage}}
}

func gitlabMessage(_ int, raw []byte) string {
	var e struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	switch m := e.Message.(type) {
	case string:
		return m
	case map[string]any:
		var parts []string
		for k, v := range m {
			parts = append(parts, fmt.Sprintf("%s %v", k, v))
		}
		return strings.Join(parts, "; ")
	}
	return e.Error
}

// pid is the URL id of a project: its full path, slashes escaped.
func pid(repo string) string {
	return "/projects/" + strings.ReplaceAll(url.PathEscape(repo), "/", "%2F")
}

func fileID(path string) string { return strings.ReplaceAll(url.PathEscape(path), "/", "%2F") }

func (g *gitlab) Kind() Kind { return GitLab }

func (g *gitlab) Me(ctx context.Context) (*User, error) {
	var u struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := g.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &User{Login: u.Username, Name: u.Name, AvatarURL: u.AvatarURL}, nil
}

type glProject struct {
	ID            int       `json:"id"`
	Path          string    `json:"path_with_namespace"`
	Visibility    string    `json:"visibility"`
	SSHURL        string    `json:"ssh_url_to_repo"`
	HTTPURL       string    `json:"http_url_to_repo"`
	WebURL        string    `json:"web_url"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	LastActivity  time.Time `json:"last_activity_at"`
	Permissions   struct {
		Project *struct {
			Level int `json:"access_level"`
		} `json:"project_access"`
		Group *struct {
			Level int `json:"access_level"`
		} `json:"group_access"`
	} `json:"permissions"`
}

func (p glProject) repo() Repo {
	level := 0
	if p.Permissions.Project != nil {
		level = p.Permissions.Project.Level
	}
	if p.Permissions.Group != nil && p.Permissions.Group.Level > level {
		level = p.Permissions.Group.Level
	}
	return Repo{FullName: p.Path, Private: p.Visibility != "public", SSHURL: p.SSHURL, CloneURL: p.HTTPURL, HTMLURL: p.WebURL,
		DefaultBranch: p.DefaultBranch, Description: p.Description, PushedAt: p.LastActivity, CanAdmin: level >= 40}
}

func (g *gitlab) Repos(ctx context.Context) ([]Repo, error) {
	var out []Repo
	for page := 1; page <= 3; page++ {
		var ps []glProject
		if err := g.do(ctx, http.MethodGet, fmt.Sprintf("/projects?membership=true&order_by=last_activity_at&per_page=100&page=%d", page), nil, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, p.repo())
		}
		if len(ps) < 100 {
			break
		}
	}
	return out, nil
}

func (g *gitlab) Owners(ctx context.Context, me string) ([]string, error) {
	var gs []struct {
		FullPath string `json:"full_path"`
	}
	if err := g.do(ctx, http.MethodGet, "/groups?min_access_level=30&per_page=100", nil, &gs); err != nil {
		return nil, err
	}
	out := []string{me}
	for _, x := range gs {
		out = append(out, x.FullPath)
	}
	return out, nil
}

func (g *gitlab) CreateRepo(ctx context.Context, r NewRepo, me string) (*Repo, error) {
	body := map[string]any{"name": r.Name, "path": r.Name, "description": r.Description, "visibility": "public", "initialize_with_readme": true}
	if r.Private {
		body["visibility"] = "private"
	}
	if r.Owner != "" && r.Owner != me {
		var grp struct {
			ID int `json:"id"`
		}
		if err := g.do(ctx, http.MethodGet, "/groups/"+fileID(r.Owner), nil, &grp); err != nil {
			return nil, err
		}
		body["namespace_id"] = grp.ID
	}
	var p glProject
	if err := g.do(ctx, http.MethodPost, "/projects", body, &p); err != nil {
		return nil, err
	}
	v := p.repo()
	v.CanAdmin = true
	return &v, nil
}

func (g *gitlab) AddDeployKey(ctx context.Context, repo, title, key string) (bool, error) {
	var keys []struct {
		Key string `json:"key"`
	}
	if err := g.do(ctx, http.MethodGet, pid(repo)+"/deploy_keys?per_page=100", nil, &keys); err != nil {
		return false, err
	}
	for _, k := range keys {
		if keyBody(k.Key) == keyBody(key) {
			return false, nil
		}
	}
	err := g.do(ctx, http.MethodPost, pid(repo)+"/deploy_keys", map[string]any{"title": title, "key": key, "can_push": false}, nil)
	return err == nil, err
}

func (g *gitlab) DeployKeyPage(repo string) string {
	return g.web + "/" + repo + "/-/settings/repository#js-deploy-keys-settings"
}

func (g *gitlab) Branches(ctx context.Context, repo string) ([]string, error) {
	var bs []struct {
		Name string `json:"name"`
	}
	if err := g.do(ctx, http.MethodGet, pid(repo)+"/repository/branches?per_page=100", nil, &bs); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Name)
	}
	return out, nil
}

func (g *gitlab) Tree(ctx context.Context, repo, ref string) ([]string, bool, error) {
	var paths []string
	for page := 1; page <= 10; page++ {
		var es []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		}
		q := fmt.Sprintf("/repository/tree?recursive=true&per_page=100&page=%d&ref=%s", page, url.QueryEscape(ref))
		if err := g.do(ctx, http.MethodGet, pid(repo)+q, nil, &es); err != nil {
			return nil, false, err
		}
		for _, e := range es {
			if e.Type == "blob" {
				paths = append(paths, e.Path)
			}
		}
		if len(es) < 100 {
			return paths, false, nil
		}
	}
	return paths, true, nil
}

func (g *gitlab) File(ctx context.Context, repo, path, ref string) (string, error) {
	c, _, err := g.GetFile(ctx, repo, path, ref)
	return c, err
}

func (g *gitlab) GetFile(ctx context.Context, repo, path, ref string) (string, bool, error) {
	var s string
	err := g.do(ctx, http.MethodGet, pid(repo)+"/repository/files/"+fileID(path)+"/raw?ref="+url.QueryEscape(ref), nil, &s)
	if IsNotFound(err) {
		return "", false, nil
	}
	return s, err == nil, err
}

func (g *gitlab) PutFile(ctx context.Context, repo, path, branch, content, msg string) (bool, error) {
	cur, exists, err := g.GetFile(ctx, repo, path, branch)
	if err != nil {
		return false, err
	}
	if exists && cur == content {
		return false, nil
	}
	method := http.MethodPost
	if exists {
		method = http.MethodPut
	}
	err = g.do(ctx, method, pid(repo)+"/repository/files/"+fileID(path), map[string]string{"branch": branch, "content": content, "commit_message": msg}, nil)
	return err == nil, err
}

func (g *gitlab) DeleteFile(ctx context.Context, repo, path, branch, msg string) error {
	_, exists, err := g.GetFile(ctx, repo, path, branch)
	if err != nil || !exists {
		return err
	}
	return g.do(ctx, http.MethodDelete, pid(repo)+"/repository/files/"+fileID(path), map[string]string{"branch": branch, "commit_message": msg}, nil)
}

// SetSecrets stores CI/CD variables (raw: values aren't expanded).
func (g *gitlab) SetSecrets(ctx context.Context, repo string, secrets map[string]string) error {
	for k, v := range secrets {
		body := map[string]any{"value": v, "protected": false, "masked": false, "raw": true}
		err := g.do(ctx, http.MethodPut, pid(repo)+"/variables/"+url.PathEscape(k), body, nil)
		if IsNotFound(err) {
			body["key"] = k
			err = g.do(ctx, http.MethodPost, pid(repo)+"/variables", body, nil)
		}
		if err != nil {
			return fmt.Errorf("couldn't save the CI/CD variable %s: %w", k, err)
		}
	}
	return nil
}

func (g *gitlab) DeleteSecrets(ctx context.Context, repo string, names []string) error {
	for _, k := range names {
		if err := g.do(ctx, http.MethodDelete, pid(repo)+"/variables/"+url.PathEscape(k), nil, nil); err != nil && !IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (g *gitlab) PrepareCI(context.Context, string) error { return nil }

// keyBody is the "type base64" part of an authorized key, for comparisons.
func keyBody(k string) string {
	f := strings.Fields(k)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return strings.TrimSpace(k)
}
