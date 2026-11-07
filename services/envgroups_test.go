package services

import (
	"strings"
	"testing"
)

func TestEnvGroups(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	g, err := svc.SaveEnvGroup(EnvGroupInput{Name: "Shared", Vars: map[string]string{"JWT_SECRET": "s3cret", "API_URL": "https://shop.io/api", " ": "ignored"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Vars) != 2 {
		t.Errorf("vars: %v", g.Vars)
	}
	for _, bad := range []EnvGroupInput{
		{Name: ""},
		{Name: "shared"}, // same name, other case
		{Name: "x", Vars: map[string]string{"1BAD": "v"}},
		{Name: "y", Vars: map[string]string{"K": "a\nb"}},
	} {
		if _, err := svc.SaveEnvGroup(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}

	p, err := svc.SaveProject(ProjectInput{Name: "web", ServerID: "s1", RepoURL: "git@github.com:me/web.git", Runtime: "compose",
		EnvGroups: []string{g.ID, g.ID}, Env: map[string]string{"API_URL": "/api"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.EnvGroups) != 1 {
		t.Errorf("groups should be deduplicated: %v", p.EnvGroups)
	}
	got, env, _ := svc.store.GetProject(p.ID)
	sp, err := svc.deploySpec(got, env)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Env["JWT_SECRET"] != "s3cret" || sp.Env["API_URL"] != "/api" {
		t.Errorf("merged env (project overrides group): %v", sp.Env)
	}
	if _, err := svc.SaveProject(ProjectInput{Name: "x", ServerID: "s1", RepoURL: "git@github.com:me/x.git", Runtime: "compose", EnvGroups: []string{"missing"}}); err == nil {
		t.Error("unknown group accepted")
	}

	list, _ := svc.ListEnvGroups()
	if len(list) != 1 || len(list[0].UsedBy) != 1 || list[0].UsedBy[0].Name != "web" {
		t.Errorf("used by: %+v", list)
	}
	if err := svc.DeleteEnvGroup(g.ID); err == nil || !strings.Contains(err.Error(), "used by web") {
		t.Errorf("delete in use: %v", err)
	}
	// Updating keeps the ID, and the next deploy sees the new value.
	if _, err := svc.SaveEnvGroup(EnvGroupInput{ID: g.ID, Name: "Shared", Vars: map[string]string{"JWT_SECRET": "rotated"}}); err != nil {
		t.Fatal(err)
	}
	sp, _ = svc.deploySpec(got, env)
	if sp.Env["JWT_SECRET"] != "rotated" {
		t.Errorf("after update: %v", sp.Env)
	}
	sn, _ := svc.store.Snapshot()
	if _, ok := sn.Files["envgroups.json"]; !ok {
		t.Error("groups must be part of TrueDeploy's backup")
	}
}
