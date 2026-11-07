package github

import (
	"context"
	"net/http"
	"net/url"
)

// Tree lists the file paths of a branch (or any ref). GitHub truncates
// very large trees; truncated reports that.
func (c *Client) Tree(ctx context.Context, owner, repo, ref string) (paths []string, truncated bool, err error) {
	var out struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := c.do(ctx, http.MethodGet, repoPath(owner, repo)+"/git/trees/"+url.PathEscape(ref)+"?recursive=1", nil, &out); err != nil {
		return nil, false, err
	}
	for _, e := range out.Tree {
		if e.Type == "blob" {
			paths = append(paths, e.Path)
		}
	}
	return paths, out.Truncated, nil
}

// File returns a file's contents on a ref ("" and no error when it doesn't exist).
func (c *Client) File(ctx context.Context, owner, repo, path, ref string) (string, error) {
	_, content, err := c.fileSHA(ctx, owner, repo, path, ref)
	return content, err
}

// Branches lists a repository's branch names (up to 100, GitHub's order).
func (c *Client) Branches(ctx context.Context, owner, repo string) ([]string, error) {
	var out []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, repoPath(owner, repo)+"/branches?per_page=100", nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, b := range out {
		names = append(names, b.Name)
	}
	return names, nil
}
