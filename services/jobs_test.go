package services

import (
	"strings"
	"testing"

	"truedeploy/internal/jobs"
)

func TestJobGuardsAndPersistence(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	p, err := svc.SaveProject(ProjectInput{Name: "shop", ServerID: "s1", RepoURL: "git@github.com:me/shop.git", Runtime: "compose"})
	if err != nil {
		t.Fatal(err)
	}
	// Validation happens before anything touches the server.
	for _, bad := range []jobs.Job{
		{Name: "x", Schedule: "every day", Service: "web", Command: "echo"},
		{Name: "x", Schedule: "@daily", Service: "", Command: "echo"},
		{Name: "", Schedule: "@daily", Service: "web", Command: "echo"},
	} {
		if _, err := svc.SaveJob(p.ID, bad); err == nil || strings.Contains(err.Error(), "dial") {
			t.Errorf("%+v: expected a validation error, got %v", bad, err)
		}
	}
	if _, err := svc.SaveJob(p.ID, jobs.Job{ID: "nope1234", Name: "x", Schedule: "@daily", Service: "web", Command: "echo"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown job: %v", err)
	}
	if _, err := svc.RunJob(p.ID, "nope1234"); err == nil {
		t.Error("running an unknown job")
	}

	// Jobs survive project edits.
	got, env, _ := svc.store.GetProject(p.ID)
	got.Jobs = []jobs.Job{{ID: "abcd1234", Name: "x", Schedule: "@daily", Service: "web", Command: "echo", Enabled: true}}
	if err := svc.store.PutProject(got, env); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveProject(ProjectInput{ID: p.ID, Name: "shop renamed", ServerID: "s1", RepoURL: "git@github.com:me/shop.git", Runtime: "compose"}); err != nil {
		t.Fatal(err)
	}
	after, _, _ := svc.store.GetProject(p.ID)
	if len(after.Jobs) != 1 || after.Jobs[0].ID != "abcd1234" {
		t.Errorf("jobs lost on edit: %+v", after.Jobs)
	}
}
