// Package updates checks GitHub Releases for a newer TrueDeploy.
// It never downloads or installs anything: it only reports what's available.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases are published.
const Repo = "HamzaBaghdadi/TrueDeploy"

// API is the GitHub API base (overridden in tests).
var API = "https://api.github.com"

type Release struct {
	Version   string    `json:"version"` // without the leading "v"
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	PageURL   string    `json:"pageUrl"`
	Installer string    `json:"installer,omitempty"` // direct download of the Windows installer, if attached
	Published time.Time `json:"published"`
}

// ErrNoReleases means the repository has no published release yet (or isn't public).
var ErrNoReleases = errors.New("no releases published yet")

// Latest returns the newest published (non-draft, non-prerelease) release.
func Latest(ctx context.Context, client *http.Client) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, API+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "TrueDeploy")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoReleases
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return Release{}, errors.New("GitHub is rate-limiting update checks; try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	var r struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return Release{}, err
	}
	v := strings.TrimPrefix(r.TagName, "v")
	if _, ok := parse(v); !ok {
		return Release{}, fmt.Errorf("the latest release tag %q isn't a version", r.TagName)
	}
	out := Release{Version: v, Name: r.Name, Notes: r.Body, PageURL: r.HTMLURL, Published: r.PublishedAt}
	for _, a := range r.Assets {
		n := strings.ToLower(a.Name)
		if strings.HasSuffix(n, ".exe") && strings.Contains(n, "installer") && strings.HasPrefix(a.URL, "https://github.com/") {
			out.Installer = a.URL
		}
	}
	return out, nil
}

// parse reads "1.2.3" (a "-suffix" marks a pre-release).
func parse(v string) ([3]int, bool) {
	var out [3]int
	core, _, _ := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether version a is newer than b. A pre-release is older
// than the release it leads up to (0.2.0-beta < 0.2.0).
func Newer(a, b string) bool {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	pa, okA := parse(a)
	pb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return !strings.Contains(a, "-") && strings.Contains(b, "-")
}
