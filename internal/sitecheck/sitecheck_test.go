package sitecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	status := 200
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "TrueDeploy-SiteCheck/1" {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	client := srv.Client()
	ctx := context.Background()

	r := Check(ctx, client, srv.URL+"/")
	if !r.Up() || r.Status != 200 || r.CertExpires.IsZero() || r.Latency < 0 {
		t.Errorf("ok site: %+v", r)
	}
	if d := r.CertDaysLeft(time.Now()); d < 1 {
		t.Errorf("days left %d", d)
	}
	if r := Check(ctx, client, srv.URL+"/old"); !r.Up() || r.CertExpires.IsZero() {
		t.Errorf("redirect: %+v", r)
	}
	if r := Check(ctx, client, srv.URL+"/loop"); r.Up() || !strings.Contains(r.Err, "loop") {
		t.Errorf("loop: %+v", r)
	}

	status = 401
	if r := Check(ctx, client, srv.URL+"/"); !r.Up() {
		t.Errorf("401 means the app works: %+v", r)
	}
	status = 502
	if r := Check(ctx, client, srv.URL+"/"); r.Up() || r.Status != 502 {
		t.Errorf("502: %+v", r)
	}

	// The default client doesn't trust the test CA: certificate error.
	if r := Check(ctx, Client(5*time.Second), srv.URL+"/"); r.Up() || !strings.Contains(r.Err, "certificate") {
		t.Errorf("untrusted cert: %+v", r)
	}
	if r := Check(ctx, Client(2*time.Second), "https://127.0.0.1:1/"); r.Up() || r.Err == "" {
		t.Errorf("closed port: %+v", r)
	}

	if (Result{}).CertDaysLeft(time.Now()) != -1 {
		t.Error("unknown expiry should be -1")
	}
	exp := Result{CertExpires: time.Now().Add(-time.Hour)}
	if exp.CertDaysLeft(time.Now()) != 0 {
		t.Error("expired should be 0")
	}
}
