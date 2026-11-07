// Package gitprov gives TrueDeploy one interface over Git hosts: GitHub,
// GitLab (gitlab.com or self-hosted), Bitbucket Cloud and Gitea/Forgejo
// (Codeberg or self-hosted). It covers what the app needs: who the token
// belongs to, listing and creating repositories, read-only deploy keys,
// branches, reading files (framework detection), and the CI pieces behind
// auto-deploy (secrets/variables and committing pipeline files).
package gitprov

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

type Kind string

const (
	GitHub    Kind = "github"
	GitLab    Kind = "gitlab"
	Bitbucket Kind = "bitbucket"
	Gitea     Kind = "gitea" // also Forgejo and Codeberg
)

// Label is the provider's name for messages.
func (k Kind) Label() string {
	switch k {
	case GitHub:
		return "GitHub"
	case GitLab:
		return "GitLab"
	case Bitbucket:
		return "Bitbucket"
	case Gitea:
		return "Gitea"
	}
	return string(k)
}

// CIName is what runs auto-deploys on the provider.
func (k Kind) CIName() string {
	switch k {
	case GitHub:
		return "GitHub Actions"
	case GitLab:
		return "GitLab CI/CD"
	case Bitbucket:
		return "Bitbucket Pipelines"
	case Gitea:
		return "Gitea Actions"
	}
	return "CI"
}

// Account is a connected Git host.
type Account struct {
	Kind Kind `json:"kind"`
	// BaseURL is the web address for self-hosted GitLab and Gitea
	// (https://git.example.com); empty for the cloud default.
	BaseURL string `json:"baseUrl,omitempty"`
	// Username is needed by Bitbucket (Atlassian email or username, used
	// with an API token or app password).
	Username  string `json:"username,omitempty"`
	Token     string `json:"token"`
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// DefaultHost is the cloud host for a kind ("" for Gitea, which is self-hosted or Codeberg).
func DefaultHost(k Kind) string {
	switch k {
	case GitHub:
		return "github.com"
	case GitLab:
		return "gitlab.com"
	case Bitbucket:
		return "bitbucket.org"
	}
	return ""
}

// Host identifies the account: the Git host its repositories live on.
func (a Account) Host() string {
	if a.BaseURL != "" {
		if u, err := url.Parse(a.BaseURL); err == nil && u.Host != "" {
			return strings.ToLower(u.Hostname())
		}
	}
	return DefaultHost(a.Kind)
}

// Web is the account's web root, e.g. https://gitlab.com.
func (a Account) Web() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return "https://" + DefaultHost(a.Kind)
}

type User struct {
	Login     string
	Name      string
	AvatarURL string
}

type Repo struct {
	FullName      string    `json:"fullName"`
	Private       bool      `json:"private"`
	SSHURL        string    `json:"sshUrl"`
	CloneURL      string    `json:"cloneUrl"`
	HTMLURL       string    `json:"htmlUrl"`
	DefaultBranch string    `json:"defaultBranch"`
	Description   string    `json:"description,omitempty"`
	PushedAt      time.Time `json:"pushedAt"`
	CanAdmin      bool      `json:"canAdmin"` // can add deploy keys
}

type NewRepo struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

// Provider is one connected account's API. Repositories are named by their
// full path (owner/repo, or group/subgroup/repo on GitLab).
type Provider interface {
	Kind() Kind
	Me(ctx context.Context) (*User, error)
	Repos(ctx context.Context) ([]Repo, error)
	// Owners lists where new repositories can be created: the user first.
	Owners(ctx context.Context, me string) ([]string, error)
	CreateRepo(ctx context.Context, r NewRepo, me string) (*Repo, error)
	AddDeployKey(ctx context.Context, repo, title, publicKey string) (added bool, err error)
	// DeployKeyPage is where a person adds a deploy key by hand.
	DeployKeyPage(repo string) string
	Branches(ctx context.Context, repo string) ([]string, error)
	Tree(ctx context.Context, repo, ref string) (paths []string, truncated bool, err error)
	// File returns a file's contents ("" and no error when it doesn't exist).
	File(ctx context.Context, repo, path, ref string) (string, error)

	// Auto-deploy: CI secrets (variables) and committing pipeline files.
	SetSecrets(ctx context.Context, repo string, secrets map[string]string) error
	DeleteSecrets(ctx context.Context, repo string, names []string) error
	// GetFile reports whether a file exists on a branch, and its contents.
	GetFile(ctx context.Context, repo, path, branch string) (content string, exists bool, err error)
	// PutFile commits a file; changed=false when it already has this content.
	PutFile(ctx context.Context, repo, path, branch, content, message string) (changed bool, err error)
	DeleteFile(ctx context.Context, repo, path, branch, message string) error
	// PrepareCI turns the provider's CI on for the repository where that's a
	// setting (Bitbucket Pipelines); others do nothing.
	PrepareCI(ctx context.Context, repo string) error
}

var (
	scpRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+@([A-Za-z0-9.-]+):(.+)$`)
	pathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+$`)
)

// ParseRepoURL returns the host and repository path of a Git URL:
// git@host:owner/repo.git, ssh://git@host:2222/owner/repo.git or
// https://host/group/sub/repo(.git).
func ParseRepoURL(raw string) (host, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	var p string
	if m := scpRe.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, "://") {
		host, p = m[1], m[2]
	} else if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host, p = u.Hostname(), u.Path
	} else {
		return "", "", false
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	if !pathRe.MatchString(p) || strings.Contains(p, "..") {
		return "", "", false
	}
	return strings.ToLower(host), p, true
}

// SplitOwner splits owner/repo (the owner may contain slashes on GitLab).
func SplitOwner(repo string) (owner, name string) {
	i := strings.LastIndex(repo, "/")
	if i < 0 {
		return "", repo
	}
	return repo[:i], repo[i+1:]
}

// Error is an API error with a message a person can act on.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// api is a JSON REST client shared by the GitLab, Bitbucket and Gitea clients.
type api struct {
	kind    Kind
	base    string // API root, no trailing slash
	auth    func(*http.Request)
	http    *http.Client
	message func(status int, raw []byte) string // provider-specific error text
}

func (c *api) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case formBody:
		rd, ctype = strings.NewReader(b.Encode()), "application/x-www-form-urlencoded"
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, ctype = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TrueDeploy")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", c.kind.Label(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return c.apiError(resp.StatusCode, raw)
	}
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(raw)
			return nil
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil
		}
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *api) apiError(status int, raw []byte) error {
	label := c.kind.Label()
	msg := ""
	if c.message != nil {
		msg = c.message(status, raw)
	}
	switch status {
	case http.StatusUnauthorized:
		msg = label + " rejected the token. It may be expired or revoked: create a new one."
	case http.StatusForbidden:
		if msg == "" {
			msg = "The token doesn't have permission for this."
		} else {
			msg = "The token doesn't have permission for this (" + msg + ")."
		}
	case http.StatusNotFound:
		if msg == "" || strings.Contains(strings.ToLower(msg), "not found") {
			msg = "Not found on " + label + ". Check the repository name, and that the token can access it."
		}
	case http.StatusTooManyRequests:
		msg = label + "'s API rate limit was reached. Try again in a few minutes."
	}
	if msg == "" {
		msg = fmt.Sprintf("%s returned HTTP %d", label, status)
	}
	return &Error{Status: status, Message: msg}
}

type formBody struct{ url.Values }

func newHTTP() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// escapePath escapes each segment of a file path for a URL.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// New returns the API for an account. apiBase overrides the API root (tests).
func New(a Account, apiBase string) (Provider, error) {
	switch a.Kind {
	case GitHub:
		return newGitHub(a, apiBase), nil
	case GitLab:
		return newGitLab(a, apiBase), nil
	case Bitbucket:
		return newBitbucket(a, apiBase), nil
	case Gitea:
		if a.BaseURL == "" && apiBase == "" {
			return nil, errors.New("enter your Gitea or Forgejo address (e.g. https://codeberg.org)")
		}
		return newGitea(a, apiBase), nil
	}
	return nil, fmt.Errorf("unknown Git provider %q", a.Kind)
}
