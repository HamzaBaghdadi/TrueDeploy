package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/nacl/box"
)

func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

type secretsKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

// SetSecrets stores Actions secrets, encrypted with the repository's public
// key as GitHub requires (libsodium sealed box).
func (c *Client) SetSecrets(ctx context.Context, owner, repo string, secrets map[string]string) error {
	var pk secretsKey
	if err := c.do(ctx, http.MethodGet, repoPath(owner, repo)+"/actions/secrets/public-key", nil, &pk); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(pk.Key)
	if err != nil || len(raw) != 32 {
		return errors.New("GitHub returned an invalid secrets key")
	}
	var recipient [32]byte
	copy(recipient[:], raw)

	for name, value := range secrets {
		sealed, err := box.SealAnonymous(nil, []byte(value), &recipient, rand.Reader)
		if err != nil {
			return err
		}
		body := map[string]string{"encrypted_value": base64.StdEncoding.EncodeToString(sealed), "key_id": pk.KeyID}
		if err := c.do(ctx, http.MethodPut, repoPath(owner, repo)+"/actions/secrets/"+url.PathEscape(name), body, nil); err != nil {
			return fmt.Errorf("saving secret %s: %w", name, err)
		}
	}
	return nil
}

// DeleteSecrets removes Actions secrets; missing ones are ignored.
func (c *Client) DeleteSecrets(ctx context.Context, owner, repo string, names []string) error {
	for _, name := range names {
		err := c.do(ctx, http.MethodDelete, repoPath(owner, repo)+"/actions/secrets/"+url.PathEscape(name), nil, nil)
		if err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}

func isNotFound(err error) bool {
	var ge *Error
	return errors.As(err, &ge) && ge.Status == http.StatusNotFound
}

type contentInfo struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

func contentsPath(owner, repo, path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return repoPath(owner, repo) + "/contents/" + strings.Join(parts, "/")
}

func (c *Client) fileSHA(ctx context.Context, owner, repo, path, branch string) (sha, content string, err error) {
	var info contentInfo
	err = c.do(ctx, http.MethodGet, contentsPath(owner, repo, path)+"?ref="+url.QueryEscape(branch), nil, &info)
	if isNotFound(err) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	decoded, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(info.Content, "\n", ""))
	return info.SHA, string(decoded), nil
}

// PutFile creates or updates a file on a branch with one commit. It returns
// changed=false when the file already has exactly this content.
func (c *Client) PutFile(ctx context.Context, owner, repo, path, branch, content, message string) (changed bool, err error) {
	sha, current, err := c.fileSHA(ctx, owner, repo, path, branch)
	if err != nil {
		return false, err
	}
	if sha != "" && current == content {
		return false, nil
	}
	body := map[string]string{"message": message, "content": base64.StdEncoding.EncodeToString([]byte(content)), "branch": branch}
	if sha != "" {
		body["sha"] = sha
	}
	err = c.do(ctx, http.MethodPut, contentsPath(owner, repo, path), body, nil)
	if isNotFound(err) && strings.HasPrefix(path, ".github/workflows/") {
		return false, errors.New(`GitHub refused to add the workflow file. Classic tokens need the "workflow" scope: create a new token in Settings`)
	}
	return err == nil, err
}

// DeleteFile removes a file from a branch; a missing file is not an error.
func (c *Client) DeleteFile(ctx context.Context, owner, repo, path, branch, message string) error {
	sha, _, err := c.fileSHA(ctx, owner, repo, path, branch)
	if err != nil || sha == "" {
		return err
	}
	return c.do(ctx, http.MethodDelete, contentsPath(owner, repo, path),
		map[string]string{"message": message, "sha": sha, "branch": branch}, nil)
}

// GetFile reports whether a file exists on a branch, and its contents.
func (c *Client) GetFile(ctx context.Context, owner, repo, path, branch string) (content string, exists bool, err error) {
	sha, content, err := c.fileSHA(ctx, owner, repo, path, branch)
	return content, sha != "", err
}
