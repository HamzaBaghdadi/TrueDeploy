package services

import (
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"truedeploy/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv("AppData", dir)         // Windows
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("HOME", dir)            // macOS
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(store.Server{ID: "s1", Name: "vps", Host: "h", Port: 22, User: "u", Auth: "password", CreatedAt: time.Now()}, store.Secrets{Password: "x"}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSaveProjectKeepsFolderOnRename(t *testing.T) {
	svc := NewProjectService(newTestStore(t))
	in := ProjectInput{Name: "My API", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "compose"}
	p, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "my-api" || p.AppDir != "~/apps/my-api" || p.Branch != "main" {
		t.Fatalf("unexpected project: %+v", p)
	}

	in.ID, in.Name = p.ID, "Renamed Service"
	r, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Slug != "my-api" || r.Name != "Renamed Service" || !r.CreatedAt.Equal(p.CreatedAt) {
		t.Fatalf("rename must keep the folder and creation time: %+v", r)
	}

	// A new project can't take a folder already used on the same server.
	_, err = svc.SaveProject(ProjectInput{Name: "my api", ServerID: "s1", RepoURL: "git@github.com:me/other.git", Runtime: "compose"})
	if err == nil || !strings.Contains(err.Error(), "~/apps/my-api") {
		t.Fatalf("want folder conflict, got %v", err)
	}
}

func TestSaveProjectValidates(t *testing.T) {
	svc := NewProjectService(newTestStore(t))
	cases := map[string]ProjectInput{
		"server":  {Name: "a", ServerID: "nope", RepoURL: "git@github.com:me/a.git"},
		"Git URL": {Name: "a", ServerID: "s1", RepoURL: "not-a-url"},
		"port":    {Name: "a", ServerID: "s1", RepoURL: "git@github.com:me/a.git", Runtime: "dockerfile"},
		"environment variable": {Name: "a", ServerID: "s1", RepoURL: "git@github.com:me/a.git",
			Env: map[string]string{"bad key": "v"}},
	}
	for want, in := range cases {
		if _, err := svc.SaveProject(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want error mentioning %q, got %v", want, err)
		}
	}
}
