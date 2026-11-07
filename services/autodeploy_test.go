package services

import (
	"strings"
	"testing"
)

func TestAutoDeployMarkedStaleOnRelevantEdits(t *testing.T) {
	st := newTestStore(t)
	svc := NewProjectService(st, nil)
	in := ProjectInput{Name: "shop", ServerID: "s1", RepoURL: "git@github.com:me/shop.git", Runtime: "compose"}
	v, err := svc.SaveProject(in)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate auto-deploy having been enabled.
	p, env, _ := st.GetProject(v.ID)
	p.AutoDeploy = true
	if err := st.PutProject(p, env); err != nil {
		t.Fatal(err)
	}

	in.ID, in.Name = v.ID, "Shop (renamed)"
	r, err := svc.SaveProject(in)
	if err != nil || !r.AutoDeploy || r.AutoDeployStale {
		t.Fatalf("a rename keeps auto-deploy as is: %+v %v", r, err)
	}

	in.Branch = "production"
	r, err = svc.SaveProject(in)
	if err != nil || !r.AutoDeploy || !r.AutoDeployStale {
		t.Fatalf("changing the branch must flag auto-deploy for a refresh: %+v %v", r, err)
	}
}

func TestEnableAutoDeployRequiresAConnectedHost(t *testing.T) {
	st := newTestStore(t)
	svc := NewProjectService(st, NewGitHubService(st))

	gl, _ := svc.SaveProject(ProjectInput{Name: "gl", ServerID: "s1", RepoURL: "git@gitlab.com:me/gl.git", Runtime: "compose"})
	if _, err := svc.EnableAutoDeploy(gl.ID); err == nil || !strings.Contains(err.Error(), "gitlab.com isn't connected") {
		t.Fatalf("GitLab without a connection: got %v", err)
	}

	gh, _ := svc.SaveProject(ProjectInput{Name: "gh", ServerID: "s1", RepoURL: "git@github.com:me/gh.git", Runtime: "compose"})
	if _, err := svc.EnableAutoDeploy(gh.ID); err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("without a GitHub connection: got %v", err)
	}
}
