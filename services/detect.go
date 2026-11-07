package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"truedeploy/internal/deploy"
	"truedeploy/internal/detect"
	"truedeploy/internal/gitprov"
)

type DetectInput struct {
	ServerID string `json:"serverId"`
	RepoURL  string `json:"repoUrl"`
	Branch   string `json:"branch"`
	// Name is the project name (its deploy key is used for server-side reads).
	Name string `json:"name"`
}

type DetectResult struct {
	Source      string              `json:"source"` // the Git host ("GitHub", "GitLab"…) or "the server"
	Suggestions []detect.Suggestion `json:"suggestions"`
	// Partial: the repository is so large that not every file was seen.
	Partial bool `json:"partial"`
}

// DetectFramework looks at a repository and suggests how to deploy it.
// It reads through the Git host when connected (works before the project is saved),
// otherwise the server makes a shallow clone with the deploy credentials.
func (s *ProjectService) DetectFramework(in DetectInput) (*DetectResult, error) {
	repo := strings.TrimSpace(in.RepoURL)
	if err := deploy.ValidateRepoURL(repo); err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		branch = "main"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if prov, path, err := s.git.forRepo(repo); err == nil {
		if r, partial, err := readRepo(ctx, prov, path, branch); err == nil {
			return &DetectResult{Source: prov.Kind().Label(), Suggestions: orEmpty(detect.Detect(r)), Partial: partial}, nil
		}
		// No access through the token (or the branch is missing): try the server.
	}
	if _, _, err := s.store.Get(in.ServerID); err != nil {
		return nil, errors.New("choose a server first")
	}
	slug := deploy.Slug(in.Name)
	if slug == "" {
		slug = "probe"
	}
	c, err := s.dial(in.ServerID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	lines, err := collect(ctx, c, deploy.ProbeScript(slug, repo, branch, detect.KeyNames()))
	if err != nil {
		return nil, err
	}
	r := detect.ParseProbe(lines)
	return &DetectResult{Source: "the server", Suggestions: orEmpty(detect.Detect(r)), Partial: len(r.Files) >= 20000}, nil
}

func orEmpty(s []detect.Suggestion) []detect.Suggestion {
	if s == nil {
		return []detect.Suggestion{}
	}
	return s
}

// readRepo lists the tree through the Git host and fetches the key files near the top.
func readRepo(ctx context.Context, c gitprov.Provider, repoPath, branch string) (detect.Repo, bool, error) {
	paths, truncated, err := c.Tree(ctx, repoPath, branch)
	if err != nil {
		return detect.Repo{}, false, err
	}
	r := detect.Repo{Files: paths, Contents: map[string]string{}}
	var want []string
	for _, p := range paths {
		if detect.KeyFile(p) && strings.Count(p, "/") <= detect.MaxDepth && !strings.Contains(p, "node_modules/") {
			want = append(want, p)
		}
	}
	if len(want) > 40 {
		want = want[:40]
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, p := range want {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if body, err := c.File(ctx, repoPath, p, branch); err == nil && body != "" {
				mu.Lock()
				r.Contents[p] = body
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	return r, truncated, nil
}
