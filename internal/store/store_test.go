package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestPutGetDeleteEncryptsSecrets(t *testing.T) {
	keyring.MockInit() // don't touch the real OS keychain
	s := &Store{dir: t.TempDir()}

	srv := Server{ID: NewID(), Name: "prod", Host: "10.0.0.1", Port: 22, User: "root", Auth: "password", CreatedAt: time.Now()}
	if err := s.Put(srv, Secrets{Password: "hunter2"}); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(filepath.Join(s.dir, "secrets.json"))
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("secrets.json contains the plaintext password")
	}
	meta, _ := os.ReadFile(filepath.Join(s.dir, "servers.json"))
	if strings.Contains(string(meta), "hunter2") {
		t.Fatal("servers.json contains the password")
	}

	got, sec, err := s.Get(srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "10.0.0.1" || sec.Password != "hunter2" {
		t.Fatalf("Get = %+v / %+v", got, sec)
	}

	list, _ := s.List()
	if len(list) != 1 {
		t.Fatalf("List len = %d", len(list))
	}

	if err := s.Delete(srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(srv.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestProjectsEnvEncryptedAndHistoryCapped(t *testing.T) {
	keyring.MockInit()
	s := &Store{dir: t.TempDir()}

	p := Project{ID: NewID(), Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Branch: "main", Runtime: "compose", CreatedAt: time.Now()}
	if err := s.PutProject(p, map[string]string{"DB_PASSWORD": "s3cret-value"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"envs.json", "projects.json"} {
		raw, _ := os.ReadFile(filepath.Join(s.dir, name))
		if strings.Contains(string(raw), "s3cret-value") {
			t.Fatalf("%s contains a plaintext env value", name)
		}
	}
	_, env, err := s.GetProject(p.ID)
	if err != nil || env["DB_PASSWORD"] != "s3cret-value" {
		t.Fatalf("GetProject env = %v, %v", env, err)
	}

	for i := 0; i < maxDeploymentsPerProject+5; i++ {
		d := Deployment{ID: NewID(), ProjectID: p.ID, Status: "success", Log: make([]string, MaxDeploymentLogLines+10)}
		if err := s.SaveDeployment(d); err != nil {
			t.Fatal(err)
		}
	}
	deps, _ := s.ListDeployments(p.ID)
	if len(deps) != maxDeploymentsPerProject || len(deps[0].Log) != MaxDeploymentLogLines {
		t.Fatalf("history not capped: %d deployments, %d log lines", len(deps), len(deps[0].Log))
	}

	// Updating keeps position instead of adding a new entry.
	deps[0].Status = "failed"
	_ = s.SaveDeployment(deps[0])
	again, _ := s.ListDeployments(p.ID)
	if len(again) != maxDeploymentsPerProject || again[0].Status != "failed" {
		t.Fatal("SaveDeployment should update an existing deployment in place")
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if deps, _ := s.ListDeployments(p.ID); len(deps) != 0 {
		t.Fatal("deleting a project should delete its history")
	}
}
