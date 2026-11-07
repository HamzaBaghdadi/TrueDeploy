package gitprov

import (
	"context"
	"fmt"

	"truedeploy/internal/github"
)

// githubProvider adapts the existing GitHub client.
type githubProvider struct{ c *github.Client }

func newGitHub(a Account, apiBase string) *githubProvider {
	c := github.New(a.Token)
	if apiBase != "" {
		c = c.WithBaseURL(apiBase)
	}
	return &githubProvider{c: c}
}

// FromGitHubClient wraps an existing client (the GitHub service's).
func FromGitHubClient(c *github.Client) Provider { return &githubProvider{c: c} }

func (g *githubProvider) Kind() Kind { return GitHub }

func (g *githubProvider) Me(ctx context.Context) (*User, error) {
	u, err := g.c.Me(ctx)
	if err != nil {
		return nil, err
	}
	return &User{Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}, nil
}

func ghRepo(r github.Repo) Repo {
	return Repo{FullName: r.FullName, Private: r.Private, SSHURL: r.SSHURL, CloneURL: r.CloneURL, HTMLURL: r.HTMLURL,
		DefaultBranch: r.DefaultBranch, Description: r.Description, PushedAt: r.PushedAt, CanAdmin: r.Permissions.Admin}
}

func (g *githubProvider) Repos(ctx context.Context) ([]Repo, error) {
	rs, err := g.c.Repos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(rs))
	for _, r := range rs {
		out = append(out, ghRepo(r))
	}
	return out, nil
}

func (g *githubProvider) Owners(ctx context.Context, me string) ([]string, error) {
	orgs, err := g.c.Orgs(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{me}
	for _, o := range orgs {
		out = append(out, o.Login)
	}
	return out, nil
}

func (g *githubProvider) CreateRepo(ctx context.Context, r NewRepo, me string) (*Repo, error) {
	created, err := g.c.CreateRepo(ctx, github.NewRepo{Owner: r.Owner, Name: r.Name, Description: r.Description, Private: r.Private}, me)
	if err != nil {
		return nil, err
	}
	v := ghRepo(*created)
	v.CanAdmin = true
	return &v, nil
}

func (g *githubProvider) AddDeployKey(ctx context.Context, repo, title, key string) (bool, error) {
	o, n := SplitOwner(repo)
	return g.c.AddDeployKey(ctx, o, n, title, key)
}

func (g *githubProvider) DeployKeyPage(repo string) string {
	return fmt.Sprintf("https://github.com/%s/settings/keys/new", repo)
}

func (g *githubProvider) Branches(ctx context.Context, repo string) ([]string, error) {
	o, n := SplitOwner(repo)
	return g.c.Branches(ctx, o, n)
}

func (g *githubProvider) Tree(ctx context.Context, repo, ref string) ([]string, bool, error) {
	o, n := SplitOwner(repo)
	return g.c.Tree(ctx, o, n, ref)
}

func (g *githubProvider) File(ctx context.Context, repo, path, ref string) (string, error) {
	o, n := SplitOwner(repo)
	return g.c.File(ctx, o, n, path, ref)
}

func (g *githubProvider) SetSecrets(ctx context.Context, repo string, s map[string]string) error {
	o, n := SplitOwner(repo)
	return g.c.SetSecrets(ctx, o, n, s)
}

func (g *githubProvider) DeleteSecrets(ctx context.Context, repo string, names []string) error {
	o, n := SplitOwner(repo)
	return g.c.DeleteSecrets(ctx, o, n, names)
}

func (g *githubProvider) GetFile(ctx context.Context, repo, path, branch string) (string, bool, error) {
	o, n := SplitOwner(repo)
	return g.c.GetFile(ctx, o, n, path, branch)
}

func (g *githubProvider) PutFile(ctx context.Context, repo, path, branch, content, msg string) (bool, error) {
	o, n := SplitOwner(repo)
	return g.c.PutFile(ctx, o, n, path, branch, content, msg)
}

func (g *githubProvider) DeleteFile(ctx context.Context, repo, path, branch, msg string) error {
	o, n := SplitOwner(repo)
	return g.c.DeleteFile(ctx, o, n, path, branch, msg)
}

func (g *githubProvider) PrepareCI(context.Context, string) error { return nil }
