package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// fakeActions stores secrets (decrypting them like GitHub would) and files.
type fakeActions struct {
	mu      sync.Mutex
	pub     *[32]byte
	priv    *[32]byte
	secrets map[string]string
	files   map[string]string // path -> content
	commits int
	// workflowScope=false makes workflow writes fail the way GitHub does.
	workflowScope bool
}

func newFakeActions(t *testing.T, workflowScope bool) (*fakeActions, *Client) {
	pub, priv, _ := box.GenerateKey(rand.Reader)
	f := &fakeActions{pub: pub, priv: priv, secrets: map[string]string{}, files: map[string]string{}, workflowScope: workflowScope}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, New("tok").WithBaseURL(srv.URL)
}

func (f *fakeActions) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	p := strings.TrimPrefix(r.URL.Path, "/repos/me/app")

	switch {
	case p == "/actions/secrets/public-key":
		json.NewEncoder(w).Encode(map[string]string{"key_id": "k1", "key": base64.StdEncoding.EncodeToString(f.pub[:])})
	case strings.HasPrefix(p, "/actions/secrets/"):
		name := strings.TrimPrefix(p, "/actions/secrets/")
		if r.Method == http.MethodDelete {
			if _, ok := f.secrets[name]; !ok {
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			delete(f.secrets, name)
			w.WriteHeader(204)
			return
		}
		sealed, _ := base64.StdEncoding.DecodeString(body["encrypted_value"])
		plain, ok := box.OpenAnonymous(nil, sealed, f.pub, f.priv)
		if !ok || body["key_id"] != "k1" {
			w.WriteHeader(422)
			return
		}
		f.secrets[name] = string(plain)
		w.WriteHeader(201)
	case strings.HasPrefix(p, "/contents/"):
		path := strings.TrimPrefix(p, "/contents/")
		content, exists := f.files[path]
		switch r.Method {
		case http.MethodGet:
			if !exists {
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"sha": "sha-" + path, "content": base64.StdEncoding.EncodeToString([]byte(content))})
		case http.MethodPut:
			if strings.HasPrefix(path, ".github/workflows/") && !f.workflowScope {
				w.WriteHeader(404)
				w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			if exists && body["sha"] != "sha-"+path {
				w.WriteHeader(409)
				return
			}
			b, _ := base64.StdEncoding.DecodeString(body["content"])
			f.files[path] = string(b)
			f.commits++
			w.WriteHeader(201)
			w.Write([]byte(`{}`))
		case http.MethodDelete:
			delete(f.files, path)
			f.commits++
			w.Write([]byte(`{}`))
		}
	default:
		w.WriteHeader(404)
	}
}

func TestSecretsAreSealedAndDeletable(t *testing.T) {
	f, c := newFakeActions(t, true)
	ctx := context.Background()
	if err := c.SetSecrets(ctx, "me", "app", map[string]string{"A": "one", "B": "-----BEGIN KEY-----\nx\n"}); err != nil {
		t.Fatal(err)
	}
	if f.secrets["A"] != "one" || f.secrets["B"] != "-----BEGIN KEY-----\nx\n" {
		t.Fatalf("secrets = %v", f.secrets)
	}
	if err := c.DeleteSecrets(ctx, "me", "app", []string{"A", "B", "MISSING"}); err != nil {
		t.Fatal(err)
	}
	if len(f.secrets) != 0 {
		t.Fatalf("secrets left: %v", f.secrets)
	}
}

func TestPutAndDeleteFile(t *testing.T) {
	f, c := newFakeActions(t, true)
	ctx := context.Background()
	path := ".github/workflows/truedeploy-app.yml"

	changed, err := c.PutFile(ctx, "me", "app", path, "main", "v1", "add")
	if err != nil || !changed || f.files[path] != "v1" {
		t.Fatalf("create: %v %v %v", changed, err, f.files)
	}
	changed, err = c.PutFile(ctx, "me", "app", path, "main", "v1", "same")
	if err != nil || changed || f.commits != 1 {
		t.Fatalf("unchanged content must not commit: %v %v commits=%d", changed, err, f.commits)
	}
	if _, err := c.PutFile(ctx, "me", "app", path, "main", "v2", "update"); err != nil || f.files[path] != "v2" {
		t.Fatalf("update: %v %v", err, f.files)
	}
	if err := c.DeleteFile(ctx, "me", "app", path, "main", "remove"); err != nil || len(f.files) != 0 {
		t.Fatalf("delete: %v %v", err, f.files)
	}
	if err := c.DeleteFile(ctx, "me", "app", path, "main", "remove"); err != nil {
		t.Fatalf("deleting a missing file should be fine: %v", err)
	}
}

func TestWorkflowNeedsScope(t *testing.T) {
	_, c := newFakeActions(t, false)
	_, err := c.PutFile(context.Background(), "me", "app", ".github/workflows/x.yml", "main", "x", "add")
	if err == nil || !strings.Contains(err.Error(), `"workflow" scope`) {
		t.Fatalf("want workflow-scope hint, got %v", err)
	}
}
