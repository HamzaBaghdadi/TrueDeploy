package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"truedeploy/internal/github"
	"truedeploy/internal/store"
)

type GitHubStatus struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

type GitHubRepo struct {
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

type NewGitHubRepo struct {
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
}

type GitHubService struct {
	store *store.Store
	// newClient is swapped in tests.
	newClient func(token string) *github.Client
}

func NewGitHubService(st *store.Store) *GitHubService {
	return &GitHubService{store: st, newClient: github.New}
}

func ghCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *GitHubService) client() (*github.Client, *store.GitHubAccount, error) {
	acct, err := s.store.GitHub()
	if err != nil {
		return nil, nil, err
	}
	if acct == nil {
		return nil, nil, errors.New("GitHub isn't connected. Connect it in Settings.")
	}
	return s.newClient(acct.Token), acct, nil
}

func (s *GitHubService) Status() (*GitHubStatus, error) {
	acct, err := s.store.GitHub()
	if err != nil || acct == nil {
		return &GitHubStatus{}, err
	}
	return &GitHubStatus{Connected: true, Login: acct.Login, Name: acct.Name, AvatarURL: acct.AvatarURL}, nil
}

// Connect checks the token with GitHub and saves it.
func (s *GitHubService) Connect(token string) (*GitHubStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("paste a GitHub token first")
	}
	ctx, cancel := ghCtx()
	defer cancel()
	u, err := s.newClient(token).Me(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetGitHub(&store.GitHubAccount{Token: token, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}); err != nil {
		return nil, err
	}
	return s.Status()
}

// Disconnect forgets the token on this computer (it stays valid on GitHub
// until revoked there).
func (s *GitHubService) Disconnect() error {
	return s.store.SetGitHub(nil)
}

func (s *GitHubService) Repos() ([]GitHubRepo, error) {
	c, _, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	repos, err := c.Repos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GitHubRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, repoView(r))
	}
	return out, nil
}

func repoView(r github.Repo) GitHubRepo {
	return GitHubRepo{
		FullName: r.FullName, Private: r.Private, SSHURL: r.SSHURL, CloneURL: r.CloneURL, HTMLURL: r.HTMLURL,
		DefaultBranch: r.DefaultBranch, Description: r.Description, PushedAt: r.PushedAt, CanAdmin: r.Permissions.Admin,
	}
}

// Owners lists where new repositories can be created: the user, then orgs.
func (s *GitHubService) Owners() ([]string, error) {
	c, acct, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	orgs, err := c.Orgs(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{acct.Login}
	for _, o := range orgs {
		out = append(out, o.Login)
	}
	return out, nil
}

func (s *GitHubService) CreateRepo(in NewGitHubRepo) (*GitHubRepo, error) {
	c, acct, err := s.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ghCtx()
	defer cancel()
	r, err := c.CreateRepo(ctx, github.NewRepo{
		Owner: strings.TrimSpace(in.Owner), Name: strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description), Private: in.Private,
	}, acct.Login)
	if err != nil {
		return nil, err
	}
	v := repoView(*r)
	v.CanAdmin = true
	return &v, nil
}
