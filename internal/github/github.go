// Package github is a small GitHub REST client covering what TrueDeploy
// needs: who the token belongs to, listing and creating repositories, and
// adding read-only deploy keys.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func New(token string) *Client {
	return &Client{token: token, baseURL: defaultBaseURL, http: &http.Client{Timeout: 20 * time.Second}}
}

// WithBaseURL points the client at another API root (tests, GitHub Enterprise).
func (c *Client) WithBaseURL(u string) *Client {
	c.baseURL = strings.TrimRight(u, "/")
	return c
}

type User struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type Org struct {
	Login string `json:"login"`
}

type Repo struct {
	FullName      string    `json:"full_name"`
	Name          string    `json:"name"`
	Owner         User      `json:"owner"`
	Private       bool      `json:"private"`
	SSHURL        string    `json:"ssh_url"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	PushedAt      time.Time `json:"pushed_at"`
	Permissions   struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

// Error is a GitHub API error with a message a user can act on.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "TrueDeploy")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode >= 300 {
		return apiError(resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func apiError(status int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
			Code    string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &e)
	msg := e.Message
	for _, d := range e.Errors {
		if d.Message != "" {
			msg += ": " + d.Message
		} else if d.Field != "" {
			msg += fmt.Sprintf(": %s %s", d.Field, d.Code)
		}
	}
	switch status {
	case http.StatusUnauthorized:
		msg = "GitHub rejected the token. It may be expired or revoked: create a new one."
	case http.StatusForbidden:
		if strings.Contains(strings.ToLower(msg), "rate limit") {
			msg = "GitHub's API rate limit was reached. Try again in a few minutes."
		} else {
			msg = "The token doesn't have permission for this (" + msg + "). A classic token needs the \"repo\" scope."
		}
	case http.StatusNotFound:
		msg = "Not found on GitHub. Check the repository name, and that the token can access it."
	}
	if msg == "" {
		msg = fmt.Sprintf("GitHub returned HTTP %d", status)
	}
	return &Error{Status: status, Message: msg}
}

func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	return &u, c.do(ctx, http.MethodGet, "/user", nil, &u)
}

func (c *Client) Orgs(ctx context.Context) ([]Org, error) {
	var orgs []Org
	return orgs, c.do(ctx, http.MethodGet, "/user/orgs?per_page=100", nil, &orgs)
}

// Repos returns repositories the token can access, most recently pushed first.
func (c *Client) Repos(ctx context.Context) ([]Repo, error) {
	var all []Repo
	for page := 1; page <= 5; page++ {
		var batch []Repo
		path := fmt.Sprintf("/user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member&page=%d", page)
		if err := c.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

type NewRepo struct {
	Owner       string // empty = the token's user
	Name        string
	Description string
	Private     bool
}

var repoNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// CreateRepo creates a repository with an initial commit, so the default
// branch exists and can be cloned right away.
func (c *Client) CreateRepo(ctx context.Context, r NewRepo, viewer string) (*Repo, error) {
	if !repoNameRe.MatchString(r.Name) || r.Name == "." || r.Name == ".." {
		return nil, errors.New("repository names can use letters, digits, '.', '-' and '_'")
	}
	body := map[string]any{"name": r.Name, "description": r.Description, "private": r.Private, "auto_init": true}
	path := "/user/repos"
	if r.Owner != "" && !strings.EqualFold(r.Owner, viewer) {
		path = "/orgs/" + url.PathEscape(r.Owner) + "/repos"
	}
	var out Repo
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		var ge *Error
		if errors.As(err, &ge) && ge.Status == http.StatusUnprocessableEntity && strings.Contains(ge.Message, "already exists") {
			return nil, fmt.Errorf("a repository named %q already exists there", r.Name)
		}
		return nil, err
	}
	return &out, nil
}

type deployKey struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	ReadOnly bool   `json:"read_only"`
}

// AddDeployKey adds a read-only deploy key. It succeeds without changes if
// the key is already on this repository.
func (c *Client) AddDeployKey(ctx context.Context, owner, repo, title, publicKey string) (added bool, err error) {
	base := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/keys"
	want := keyBody(publicKey)

	var keys []deployKey
	if err := c.do(ctx, http.MethodGet, base+"?per_page=100", nil, &keys); err != nil {
		return false, err
	}
	for _, k := range keys {
		if keyBody(k.Key) == want {
			return false, nil
		}
	}

	err = c.do(ctx, http.MethodPost, base, map[string]any{"title": title, "key": publicKey, "read_only": true}, nil)
	var ge *Error
	if errors.As(err, &ge) && ge.Status == http.StatusUnprocessableEntity && strings.Contains(ge.Message, "already in use") {
		return false, errors.New("this server key is already a deploy key on another repository; GitHub allows each key on one repository only")
	}
	return err == nil, err
}

// keyBody returns "type base64" without the comment, for comparing keys.
func keyBody(k string) string {
	f := strings.Fields(k)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return strings.TrimSpace(k)
}

var (
	scpRe  = regexp.MustCompile(`^[^@\s]+@github\.com:([^/\s]+)/([^/\s]+?)(\.git)?/?$`)
	httpRe = regexp.MustCompile(`^(?:https|ssh)://(?:[^@/]+@)?github\.com(?::\d+)?/([^/\s]+)/([^/\s]+?)(\.git)?/?$`)
)

// ParseRepoURL returns owner and repo for a github.com URL.
func ParseRepoURL(u string) (owner, repo string, ok bool) {
	for _, re := range []*regexp.Regexp{scpRe, httpRe} {
		if m := re.FindStringSubmatch(strings.TrimSpace(u)); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}
