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
	svc := NewProjectService(newTestStore(t), nil)
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
	svc := NewProjectService(newTestStore(t), nil)
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

func TestEditKeepsBackupSchedule(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	in := ProjectInput{Name: "shop", ServerID: "s1", RepoURL: "git@github.com:me/shop.git", Runtime: "compose"}
	p, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	got, env, _ := svc.store.GetProject(p.ID)
	got.Backups = &store.BackupSchedule{Enabled: true, Frequency: "daily", Hour: 2, Keep: 5}
	if err := svc.store.PutProject(got, env); err != nil {
		t.Fatal(err)
	}
	in.ID, in.Name = p.ID, "shop renamed"
	if _, err := svc.SaveProject(in); err != nil {
		t.Fatal(err)
	}
	after, _, _ := svc.store.GetProject(p.ID)
	if after.Backups == nil || after.Backups.Keep != 5 {
		t.Errorf("schedule lost on edit: %+v", after.Backups)
	}
	if _, err := svc.RestoreProjectBackup(p.ID, "../../etc/passwd"); err == nil {
		t.Error("bad backup name accepted")
	}
}

func TestPresetProjects(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	in := ProjectInput{Name: "web", ServerID: "s1", RepoURL: "git@github.com:me/web.git", Runtime: "dockerfile", HostPort: 8080, ContainerPort: 3000,
		Folder: "/frontend/", Preset: "react-vite", PresetOptions: map[string]string{"spa": "true"}, Domains: []string{"shop.io"}}
	p, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	if p.ContainerPort != 80 || p.Folder != "frontend" || p.Preset != "react-vite" {
		t.Fatalf("static presets listen on 80 inside the container: %+v", p.Project)
	}
	got, env, _ := svc.store.GetProject(p.ID)
	sp, err := svc.deploySpec(got, env)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Files[".truedeploy/Dockerfile"] == "" || sp.Files[".truedeploy/nginx.conf"] == "" || !sp.LocalOnly || sp.Folder != "frontend" || sp.PresetName != "React (Vite)" {
		t.Errorf("spec: %+v", sp)
	}

	// Server presets keep the chosen container port (default: the preset's).
	api, err := svc.SaveProject(ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "dockerfile", HostPort: 3000, Preset: "nestjs"})
	if err != nil {
		t.Fatal(err)
	}
	if api.ContainerPort != 3000 {
		t.Errorf("nest default port: %d", api.ContainerPort)
	}
	if _, err := svc.SaveProject(ProjectInput{Name: "bad", ServerID: "s1", RepoURL: "git@github.com:me/b.git", Runtime: "dockerfile", HostPort: 1, Preset: "nestjs", PresetOptions: map[string]string{"start": "a\nb"}}); err == nil {
		t.Error("invalid preset options must be rejected on save")
	}
	if _, err := svc.SaveProject(ProjectInput{Name: "bad2", ServerID: "s1", RepoURL: "git@github.com:me/c.git", Runtime: "dockerfile", HostPort: 1, Preset: "cobol"}); err == nil {
		t.Error("unknown preset accepted")
	}
	// Switching to compose drops the preset.
	in.ID, in.Runtime, in.AppPort = p.ID, "compose", 8080
	c, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Preset != "" || c.Folder != "" {
		t.Errorf("compose project kept preset: %+v", c.Project)
	}
	if files, err := svc.PreviewPreset("fastapi", nil, 0); err != nil || files[".truedeploy/Dockerfile"] == "" {
		t.Errorf("preview: %v", err)
	}
}

func TestParseStatusWithUsage(t *testing.T) {
	got := parseStatus([]string{
		"shop-db-1\texited\tExited (0) 1 hour ago\t",
		"shop-web-1\trunning\tUp 2 hours\t127.0.0.1:3000->3000/tcp",
		"stat\tshop-web-1\t1.25%\t120MiB / 512MiB\t23.44%",
		"stat\tbroken",
	})
	if len(got) != 2 || got[0].CPU != "" || got[1].CPU != "1.25%" || got[1].Memory != "120MiB / 512MiB" || got[1].MemPerc != "23.44%" {
		t.Errorf("%+v", got)
	}
}
