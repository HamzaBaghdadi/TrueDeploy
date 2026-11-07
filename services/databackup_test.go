package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"truedeploy/internal/store"
)

func TestExportImportRoundTrip(t *testing.T) {
	st := newTestStore(t) // has server s1 "vps"
	app := NewAppService(st, nil)
	if s, _ := app.ExportStatus(); !s.Changed || !s.HasData || !s.LastExport.IsZero() {
		t.Errorf("never exported: %+v", s)
	}
	file, sn, err := app.exportBytes("a long enough passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.exportBytes("short"); err == nil {
		t.Error("short passphrase accepted")
	}
	_ = st.SetLastExport(store.ExportRecord{Fingerprint: sn.Fingerprint()})
	if s, _ := app.ExportStatus(); s.Changed {
		t.Error("nothing changed since the export")
	}
	path := filepath.Join(t.TempDir(), "b.tdbackup")
	os.WriteFile(path, file, 0o600)

	if _, err := app.PreviewImport(path, "wrong passphrase!!"); err == nil {
		t.Error("wrong passphrase accepted")
	}
	sum, err := app.PreviewImport(path, "a long enough passphrase")
	if err != nil || len(sum.Servers) != 1 || sum.Servers[0] != "vps" {
		t.Fatalf("preview: %v %+v", err, sum)
	}

	// Change something, then replace from the backup: a safety copy appears.
	_ = st.Put(store.Server{ID: "s9", Name: "new"}, store.Secrets{})
	if s, _ := app.ExportStatus(); !s.Changed {
		t.Error("adding a server should mark the export stale")
	}
	msg, err := app.ImportData(path, "a long enough passphrase", true)
	if err != nil || !strings.Contains(msg, "before-import-") {
		t.Fatalf("import: %v %s", err, msg)
	}
	list, _ := st.List()
	if len(list) != 1 {
		t.Errorf("replace should leave only the backup's server: %+v", list)
	}
	copies, _ := filepath.Glob(filepath.Join(st.Dir(), "before-import-*.tdbackup"))
	if len(copies) != 1 {
		t.Fatalf("safety copy: %v", copies)
	}
	// The safety copy holds the server that was replaced away.
	if sum, err := app.PreviewImport(copies[0], "a long enough passphrase"); err != nil || len(sum.Servers) != 2 {
		t.Errorf("safety copy: %v %+v", err, sum)
	}
}
