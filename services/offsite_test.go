package services

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"truedeploy/internal/offsite"
	"truedeploy/internal/store"
)

// memBucket is a path-style S3 bucket named "bkt" in memory.
type memBucket struct {
	mu   sync.Mutex
	objs map[string]string
}

func (m *memBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/bkt/")
	if strings.Contains(r.Header.Get("Authorization"), "Credential=wrong/") {
		w.WriteHeader(403)
		fmt.Fprint(w, `<Error><Code>InvalidAccessKeyId</Code></Error>`)
		return
	}
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		m.objs[key] = string(b)
	case http.MethodDelete:
		delete(m.objs, key)
	case http.MethodGet:
		if r.URL.Path == "/bkt/" {
			fmt.Fprint(w, `<ListBucketResult>`)
			for k, v := range m.objs {
				if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
					fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>%d</Size><LastModified>2026-11-06T03:15:00Z</LastModified></Contents>`, k, len(v))
				}
			}
			fmt.Fprint(w, `</ListBucketResult>`)
			return
		}
		v, ok := m.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, v)
	}
}

func TestOffsiteSaveListAndDownload(t *testing.T) {
	st := newTestStore(t)
	bucket := &memBucket{objs: map[string]string{}}
	srv := httptest.NewServer(bucket)
	defer srv.Close()
	svc := NewOffsiteService(st, NewProjectService(st, nil))

	if v, err := svc.Offsite(); err != nil || v.Configured {
		t.Fatalf("not set up yet: %+v %v", v, err)
	}
	in := offsite.Target{Provider: "other", Endpoint: srv.URL, Bucket: "bkt", AccessKey: "AK", SecretKey: "SK"}
	if _, err := svc.SaveOffsite(offsite.Target{Endpoint: srv.URL, Bucket: "bkt", AccessKey: "wrong", SecretKey: "SK"}); err == nil || !strings.Contains(err.Error(), "access key") {
		t.Fatalf("bad key should fail the test: %v", err)
	}
	msg, err := svc.SaveOffsite(in)
	if err != nil || !strings.Contains(msg, "could write, list and delete") {
		t.Fatalf("%q %v", msg, err)
	}
	if len(bucket.objs) != 0 {
		t.Errorf("the check file should be removed: %v", bucket.objs)
	}
	v, _ := svc.Offsite()
	if !v.Configured || !v.HasSecret || v.Target.SecretKey != "" || v.Target.Prefix != "truedeploy" {
		t.Errorf("view must hide the secret: %+v", v)
	}
	// Saving again without the secret keeps it.
	in.SecretKey = ""
	in.Prefix = "backups/td"
	if _, err := svc.SaveOffsite(in); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Offsite(); got.SecretKey != "SK" || got.Prefix != "backups/td" {
		t.Errorf("saved: %+v", got)
	}

	// Copies of one database, newest first; other databases and junk are ignored.
	d := store.Database{ID: "d1", Name: "shop", Slug: "shop", ServerID: "s1", Engine: "postgres", CreatedAt: time.Now()}
	if err := st.PutDatabase(d, "pw"); err != nil {
		t.Fatal(err)
	}
	base := "backups/td/s1/databases/"
	bucket.objs[base+"shop-20261106-031500.sql.gz"] = "dump"
	bucket.objs[base+"shop-api-20261106-031500.sql.gz"] = "other db"
	bucket.objs[base+"shop-x/evil.sql.gz"] = "nested"
	bucket.objs["backups/td/s2/databases/shop-20261106-031500.sql.gz"] = "other server"
	copies, err := svc.OffsiteCopies("database", "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 || copies[0].Name != "shop-20261106-031500.sql.gz" || copies[0].Bytes != 4 {
		t.Errorf("copies: %+v", copies)
	}

	// Download straight from the bucket.
	tg, _ := st.Offsite()
	dst := filepath.Join(t.TempDir(), "x.sql.gz")
	if err := downloadObject(tg, base+"shop-20261106-031500.sql.gz", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "dump" {
		t.Errorf("downloaded %q", b)
	}
	if err := downloadObject(tg, base+"missing.sql.gz", dst+"2"); err == nil {
		t.Error("missing object should fail")
	} else if _, statErr := os.Stat(dst + "2.part"); statErr == nil {
		t.Error("no partial file should be left")
	}

	if _, err := svc.FetchOffsite("database", "d1", "../../etc/passwd"); err == nil {
		t.Error("bad name accepted")
	}

	if _, err := svc.RemoveOffsite(); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Offsite(); got != nil {
		t.Error("should be removed")
	}
	if _, err := svc.OffsiteCopies("database", "d1"); err == nil {
		t.Error("listing without storage should fail")
	}
}

func TestBackupMessage(t *testing.T) {
	ok := backupMessage([]string{"::td::backup=/x/shop-1.sql.gz", "Backup saved: shop-1.sql.gz (4K)", "==> Copying", "::td::offsite=ok", "Offsite copy saved."})
	if ok != "Backup saved: shop-1.sql.gz (4K) Copied to offsite storage." {
		t.Error(ok)
	}
	if m := backupMessage([]string{"Backup saved: a (1K)", "::td::offsite=failed"}); !strings.Contains(m, "offsite copy failed") {
		t.Error(m)
	}
	if m := backupMessage([]string{"Backup saved: a (1K)", "::td::offsite=notset"}); m != "Backup saved: a (1K)" {
		t.Error(m)
	}
}
