package services

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"truedeploy/internal/gitprov"
	"truedeploy/internal/store"
)

// GitService connects Git hosts (GitHub, GitLab, Bitbucket, Gitea/Forgejo)
// and gives the rest of the app one way to reach a repository's host.
// GitHub keeps its own service and storage; this one delegates to it.
type GitService struct {
	store  *store.Store
	github *GitHubService
	// apiBase overrides an account's API root (tests).
	apiBase func(a gitprov.Account) string
}

func NewGitService(st *store.Store, gh *GitHubService) *GitService {
	return &GitService{store: st, github: gh, apiBase: func(gitprov.Account) string { return "" }}
}

// GitAccountView is a connected account as the UI shows it (no token).
type GitAccountView struct {
	Host      string `json:"host"` // identifies the account
	Kind      string `json:"kind"`
	Label     string `json:"label"` // GitHub, GitLab, Bitbucket, Gitea
	Web       string `json:"web"`
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	CIName    string `json:"ciName"`
}

type GitConnectInput struct {
	Kind     string `json:"kind"`
	BaseURL  string `json:"baseUrl"`  // self-hosted GitLab, Gitea/Forgejo
	Username string `json:"username"` // Bitbucket
	Token    string `json:"token"`
}

func accountView(a gitprov.Account) GitAccountView {
	return GitAccountView{Host: a.Host(), Kind: string(a.Kind), Label: a.Kind.Label(), Web: a.Web(), Login: a.Login,
		Name: a.Name, AvatarURL: a.AvatarURL, CIName: a.Kind.CIName()}
}

// Accounts lists connected accounts, GitHub first.
func (s *GitService) Accounts() ([]GitAccountView, error) {
	out := []GitAccountView{}
	if s.github != nil {
		if acct, err := s.store.GitHub(); err != nil {
			return nil, err
		} else if acct != nil {
			out = append(out, accountView(gitprov.Account{Kind: gitprov.GitHub, Login: acct.Login, Name: acct.Name, AvatarURL: acct.AvatarURL}))
		}
	}
	others, err := s.store.GitAccounts()
	if err != nil {
		return nil, err
	}
	for _, a := range others {
		out = append(out, accountView(a))
	}
	return out, nil
}

// normalizeBase turns "git.example.com" or "https://git.example.com/" into
// "https://git.example.com", and the cloud address into "".
func normalizeBase(kind gitprov.Kind, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" {
		return "", fmt.Errorf("%q isn't an address like https://git.example.com", raw)
	}
	base := u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/")
	if def := gitprov.DefaultHost(kind); def != "" && strings.EqualFold(u.Host, def) && u.Path == "" {
		return "", nil
	}
	return base, nil
}

// Connect checks the token with the provider and saves the account.
func (s *GitService) Connect(in GitConnectInput) (*GitAccountView, error) {
	kind := gitprov.Kind(in.Kind)
	token := strings.TrimSpace(in.Token)
	if kind == gitprov.GitHub {
		if s.github == nil {
			return nil, errors.New("GitHub isn't available")
		}
		st, err := s.github.Connect(token)
		if err != nil {
			return nil, err
		}
		v := accountView(gitprov.Account{Kind: gitprov.GitHub, Login: st.Login, Name: st.Name, AvatarURL: st.AvatarURL})
		return &v, nil
	}
	if token == "" {
		return nil, fmt.Errorf("paste a %s token first", kind.Label())
	}
	base, err := normalizeBase(kind, in.BaseURL)
	if err != nil {
		return nil, err
	}
	switch kind {
	case gitprov.Bitbucket:
		base = ""
		if strings.TrimSpace(in.Username) == "" {
			return nil, errors.New("enter your Atlassian account email (for an API token) or Bitbucket username (for an app password)")
		}
	case gitprov.Gitea:
		if base == "" {
			return nil, errors.New("enter your Gitea or Forgejo address, e.g. https://codeberg.org")
		}
	case gitprov.GitLab:
	default:
		return nil, fmt.Errorf("unknown Git provider %q", in.Kind)
	}
	a := gitprov.Account{Kind: kind, BaseURL: base, Username: strings.TrimSpace(in.Username), Token: token}
	if a.Host() == "github.com" {
		return nil, errors.New("connect github.com as GitHub")
	}
	p, err := gitprov.New(a, s.apiBase(a))
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	u, err := p.Me(ctx)
	if err != nil {
		return nil, err
	}
	a.Login, a.Name, a.AvatarURL = u.Login, u.Name, u.AvatarURL
	if err := s.store.PutGitAccount(a); err != nil {
		return nil, err
	}
	v := accountView(a)
	return &v, nil
}

// Disconnect forgets an account on this computer (the token stays valid on
// the provider until revoked there).
func (s *GitService) Disconnect(host string) error {
	if host == "github.com" {
		if s.github == nil {
			return nil
		}
		return s.github.Disconnect()
	}
	return s.store.DeleteGitAccount(host)
}

// errNotConnected means no account is connected for a repository's host.
type errNotConnected struct{ host string }

func (e errNotConnected) Error() string {
	return fmt.Sprintf("%s isn't connected. Connect it in Settings → Git providers.", e.host)
}

func isNotConnected(err error) bool {
	var e errNotConnected
	return errors.As(err, &e)
}

// provider returns the API for a connected host.
func (s *GitService) provider(host string) (gitprov.Provider, gitprov.Account, error) {
	host = strings.ToLower(host)
	if host == "github.com" {
		if s.github == nil {
			return nil, gitprov.Account{}, errNotConnected{host}
		}
		c, acct, err := s.github.client()
		if err != nil {
			if acct == nil {
				return nil, gitprov.Account{}, errNotConnected{host}
			}
			return nil, gitprov.Account{}, err
		}
		return gitprov.FromGitHubClient(c), gitprov.Account{Kind: gitprov.GitHub, Login: acct.Login}, nil
	}
	accts, err := s.store.GitAccounts()
	if err != nil {
		return nil, gitprov.Account{}, err
	}
	for _, a := range accts {
		if a.Host() == host {
			p, err := gitprov.New(a, s.apiBase(a))
			return p, a, err
		}
	}
	return nil, gitprov.Account{}, errNotConnected{host}
}

// forRepo finds the API for a repository URL and the repository's path on it.
func (s *GitService) forRepo(repoURL string) (gitprov.Provider, string, error) {
	host, repo, ok := gitprov.ParseRepoURL(repoURL)
	if !ok {
		return nil, "", errors.New("this isn't a repository URL TrueDeploy understands")
	}
	p, _, err := s.provider(host)
	return p, repo, err
}

// hostKind reports which provider a repository URL belongs to, when its host
// is connected or a known cloud host ("" otherwise).
func (s *GitService) hostKind(repoURL string) gitprov.Kind {
	host, _, ok := gitprov.ParseRepoURL(repoURL)
	if !ok {
		return ""
	}
	for _, k := range []gitprov.Kind{gitprov.GitHub, gitprov.GitLab, gitprov.Bitbucket} {
		if gitprov.DefaultHost(k) == host {
			return k
		}
	}
	if accts, err := s.store.GitAccounts(); err == nil {
		for _, a := range accts {
			if a.Host() == host {
				return a.Kind
			}
		}
	}
	return ""
}

func (s *GitService) Repos(host string) ([]gitprov.Repo, error) {
	p, _, err := s.provider(host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	rs, err := p.Repos(ctx)
	if rs == nil {
		rs = []gitprov.Repo{}
	}
	return rs, err
}

func (s *GitService) Owners(host string) ([]string, error) {
	p, a, err := s.provider(host)
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	return p.Owners(ctx, a.Login)
}

func (s *GitService) CreateRepo(host string, in gitprov.NewRepo) (*gitprov.Repo, error) {
	p, a, err := s.provider(host)
	if err != nil {
		return nil, err
	}
	in.Owner, in.Name, in.Description = strings.TrimSpace(in.Owner), strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.Name == "" {
		return nil, errors.New("name the repository first")
	}
	ctx, cancel := ghCtx()
	defer cancel()
	return p.CreateRepo(ctx, in, a.Login)
}
