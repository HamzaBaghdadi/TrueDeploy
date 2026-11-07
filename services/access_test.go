package services

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestSetAccess(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	p, err := svc.SaveProject(ProjectInput{Name: "staging", ServerID: "s1", RepoURL: "git@github.com:me/s.git", Runtime: "dockerfile", HostPort: 8081, ContainerPort: 80})
	if err != nil {
		t.Fatal(err)
	}
	// Without a domain it's saved and applies later (no SSH needed).
	if _, err := svc.SetAccess(AccessInput{ProjectID: p.ID, Protect: true, User: "team"}); err == nil || !strings.Contains(err.Error(), "enter a password") {
		t.Errorf("no password: %v", err)
	}
	if _, err := svc.SetAccess(AccessInput{ProjectID: p.ID, Protect: true, User: "team", Password: "short"}); err == nil {
		t.Error("short password accepted")
	}
	msg, err := svc.SetAccess(AccessInput{ProjectID: p.ID, Protect: true, User: "team", Password: "correct horse", Maintenance: true, Message: "Back at 10"})
	if err != nil || !strings.Contains(msg, "once the project has a domain") {
		t.Fatalf("%v %s", err, msg)
	}
	got, _, _ := svc.store.GetProject(p.ID)
	if got.AuthUser != "team" || bcrypt.CompareHashAndPassword([]byte(got.AuthHash), []byte("correct horse")) != nil || !got.Maintenance || got.MaintenanceMessage != "Back at 10" {
		t.Fatalf("stored: %+v", got)
	}
	// An empty password keeps the current one; renaming the user is allowed.
	if _, err := svc.SetAccess(AccessInput{ProjectID: p.ID, Protect: true, User: "qa"}); err != nil {
		t.Fatal(err)
	}
	again, _, _ := svc.store.GetProject(p.ID)
	if again.AuthHash != got.AuthHash || again.AuthUser != "qa" || again.Maintenance {
		t.Errorf("keep password: %+v", again)
	}
	// The hash never reaches the UI, and edits keep the settings.
	v, _ := svc.GetProject(p.ID)
	if v.AuthHash != "" || v.AuthUser != "qa" {
		t.Errorf("view: %q %q", v.AuthHash, v.AuthUser)
	}
	if _, err := svc.SaveProject(ProjectInput{ID: p.ID, Name: "staging2", ServerID: "s1", RepoURL: "git@github.com:me/s.git", Runtime: "dockerfile", HostPort: 8081, ContainerPort: 80}); err != nil {
		t.Fatal(err)
	}
	if kept, _, _ := svc.store.GetProject(p.ID); kept.AuthHash == "" || kept.AuthUser != "qa" {
		t.Error("project edit dropped the password")
	}
	if _, err := svc.SetAccess(AccessInput{ProjectID: p.ID}); err != nil {
		t.Fatal(err)
	}
	if off, _, _ := svc.store.GetProject(p.ID); off.AuthHash != "" || off.AuthUser != "" {
		t.Error("turning protection off should forget the password")
	}
}
