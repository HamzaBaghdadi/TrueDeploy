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
