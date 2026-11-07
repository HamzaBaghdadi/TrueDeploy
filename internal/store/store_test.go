package store

import (
	"encoding/json"
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

func TestSnapshotRestore(t *testing.T) {
	src := openTestStore(t)
	if err := src.Put(Server{ID: "s1", Name: "prod", Host: "h", Port: 22, User: "u", Auth: "key"}, Secrets{PrivateKey: "-----BEGIN KEY-----"}); err != nil {
		t.Fatal(err)
	}
	if err := src.PutProject(Project{ID: "p1", Name: "shop", ServerID: "s1"}, map[string]string{"JWT": "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := src.SetGitHub(&GitHubAccount{Login: "me", Token: "ghp_x"}); err != nil {
		t.Fatal(err)
	}
	sn, err := src.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	sum := sn.Summary()
	if len(sum.Servers) != 1 || sum.Servers[0] != "prod" || len(sum.Projects) != 1 || sum.GitHub != "me" {
		t.Fatalf("summary: %+v", sum)
	}
	fp := sn.Fingerprint()

	// Round trip through JSON, as the file does.
	raw, _ := json.Marshal(sn)
	var back Snapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}

	// Merge into a computer that already has another server and a different project with the same ID.
	dst := openTestStore(t)
	_ = dst.Put(Server{ID: "s2", Name: "staging"}, Secrets{Password: "pw"})
	_ = dst.PutProject(Project{ID: "p1", Name: "local version", ServerID: "s2"}, map[string]string{"A": "1"})
	if err := dst.Restore(back, false); err != nil {
		t.Fatal(err)
	}
	servers, _ := dst.List()
	if len(servers) != 2 {
		t.Errorf("merge should add the missing server: %+v", servers)
	}
	if _, sec, err := dst.Get("s1"); err != nil || sec.PrivateKey != "-----BEGIN KEY-----" {
		t.Errorf("imported key: %v %+v", err, sec)
	}
	if p, env, _ := dst.GetProject("p1"); p.Name != "local version" || env["A"] != "1" {
		t.Errorf("merge must keep this computer's version: %+v %v", p, env)
	}
	if gh, _ := dst.GitHub(); gh == nil || gh.Token != "ghp_x" {
		t.Errorf("github: %+v", gh)
	}

	// Replace makes it identical (re-encrypted with this computer's key).
	if err := dst.Restore(back, true); err != nil {
		t.Fatal(err)
	}
	servers, _ = dst.List()
	if p, env, _ := dst.GetProject("p1"); len(servers) != 1 || p.Name != "shop" || env["JWT"] != "secret" {
		t.Errorf("replace: %+v %+v %v", servers, p, env)
	}
	again, _ := dst.Snapshot()
	if again.Fingerprint() != fp {
		t.Error("after replace the state should match the export")
	}
	if b, _ := os.ReadFile(filepath.Join(dst.Dir(), "envs.json")); strings.Contains(string(b), "secret") {
		t.Error("secrets must stay encrypted at rest after import")
	}
	if err := dst.Restore(Snapshot{App: "Other"}, true); err == nil {
		t.Error("foreign snapshot accepted")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	keyring.MockInit()
	return &Store{dir: t.TempDir()}
}
