package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"v0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.2.0", "0.2.0-beta.1", true},
		{"0.2.0-beta.1", "0.2.0", false},
		{"0.2.0-beta.1", "0.1.0", true},
		{"garbage", "0.1.0", false},
		{"0.2", "0.1.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLatest(t *testing.T) {
	status, body := 200, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+Repo+"/releases/latest" || r.Header.Get("User-Agent") == "" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	API = srv.URL
	ctx := context.Background()

	body = `{"tag_name":"v0.3.1","name":"TrueDeploy 0.3.1","body":"Fixes","html_url":"https://github.com/x/y/releases/tag/v0.3.1",
	  "published_at":"2026-11-01T10:00:00Z","assets":[
	  {"name":"truedeploy.exe","browser_download_url":"https://github.com/x/y/releases/download/v0.3.1/truedeploy.exe"},
	  {"name":"truedeploy-amd64-installer.exe","browser_download_url":"https://github.com/x/y/releases/download/v0.3.1/truedeploy-amd64-installer.exe"}]}`
	r, err := Latest(ctx, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.3.1" || r.Notes != "Fixes" || r.Installer != "https://github.com/x/y/releases/download/v0.3.1/truedeploy-amd64-installer.exe" || r.Published.IsZero() {
		t.Errorf("release = %+v", r)
	}

	status = 404
	if _, err := Latest(ctx, srv.Client()); err != ErrNoReleases {
		t.Errorf("404: %v", err)
	}
	status, body = 200, `{"tag_name":"nightly"}`
	if _, err := Latest(ctx, srv.Client()); err == nil {
		t.Error("non-version tags must be rejected")
	}
	status = 403
	if _, err := Latest(ctx, srv.Client()); err == nil {
		t.Error("rate limit must be an error")
	}
}
