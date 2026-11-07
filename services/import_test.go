package services

import (
	"strings"
	"testing"

	"truedeploy/internal/importer"
)

func TestImportGuards(t *testing.T) {
	st := newTestStore(t)
	projects := NewProjectService(st, nil)
	imp := NewImportService(st, projects, NewDatabaseService(st))

	if _, err := imp.AdoptCompose(AdoptInput{ServerID: "s1", Name: "shop"}); err == nil || !strings.Contains(err.Error(), "scan the server first") {
		t.Errorf("no scan: %v", err)
	}
	imp.scans["s1"] = importer.Scan{Compose: []importer.ComposeProject{
		{Name: "loose", WorkDir: "/srv/loose", Writable: true}, // no git remote
	}}
	if _, err := imp.AdoptCompose(AdoptInput{ServerID: "s1", Name: "loose"}); err == nil || !strings.Contains(err.Error(), "Git clone") {
		t.Errorf("no repo: %v", err)
	}
	if _, err := imp.AdoptCompose(AdoptInput{ServerID: "s1", Name: "ghost"}); err == nil || !strings.Contains(err.Error(), "scan again") {
		t.Errorf("unknown: %v", err)
	}
	if _, err := imp.ImportDatabase(ImportDBInput{ServerID: "s1", Container: "pg"}); err == nil {
		t.Error("unknown database accepted")
	}

	for _, c := range []struct {
		db   importer.Database
		want string
		warn bool
	}{
		{importer.Database{Engine: "postgres", Version: "16"}, "16", false},
		{importer.Database{Engine: "postgres", Version: "12"}, "17", true},
		{importer.Database{Engine: "mysql", Version: "8.0"}, "8.0", false},
		{importer.Database{Engine: "mysql", Version: "10.11", MariaDB: true}, "8.4", true},
		{importer.Database{Engine: "mongo", Version: "7"}, "7", false},
	} {
		got, warn := targetVersion(c.db)
		if got != c.want || (len(warn) > 0) != c.warn {
			t.Errorf("%+v: got %s %v", c.db, got, warn)
		}
	}
	if !validProjectName("my_app") || validProjectName("My App") || validProjectName("") {
		t.Error("project name rules")
	}
}
