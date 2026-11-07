package services

import (
	"strings"
	"testing"
	"time"

	"truedeploy/internal/store"
)

func TestRecentDeploymentsAcrossProjects(t *testing.T) {
	st := newTestStore(t)
	svc := NewProjectService(st, nil)
	a, _ := svc.SaveProject(ProjectInput{Name: "a", ServerID: "s1", RepoURL: "git@github.com:me/a.git", Runtime: "compose"})
	b, _ := svc.SaveProject(ProjectInput{Name: "b", ServerID: "s1", RepoURL: "git@github.com:me/b.git", Runtime: "compose"})
	now := time.Now()
	st.SaveDeployment(store.Deployment{ID: "a1", ProjectID: a.ID, StartedAt: now.Add(-3 * time.Hour), Status: "success", Log: []string{"x"}})
	st.SaveDeployment(store.Deployment{ID: "b1", ProjectID: b.ID, StartedAt: now.Add(-1 * time.Hour), Status: "failed"})
	st.SaveDeployment(store.Deployment{ID: "a2", ProjectID: a.ID, StartedAt: now.Add(-2 * time.Hour), Status: "success"})

	list, err := svc.RecentDeployments(10)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, e := range list {
		order = append(order, e.ID)
		if e.Log != nil || e.ServerName != "vps" {
			t.Errorf("%s: logs should be stripped and server named: %+v", e.ID, e)
		}
	}
	if strings.Join(order, ",") != "b1,a2,a1" || list[0].ProjectName != "b" {
		t.Fatalf("want newest first across projects, got %v", order)
	}
	if l, _ := svc.RecentDeployments(2); len(l) != 2 {
		t.Fatal("limit not applied")
	}
}

func TestRollbackNeedsRecordedSHA(t *testing.T) {
	st := newTestStore(t)
	svc := NewProjectService(st, nil)
	p, _ := svc.SaveProject(ProjectInput{Name: "a", ServerID: "s1", RepoURL: "git@github.com:me/a.git", Runtime: "compose"})
	st.SaveDeployment(store.Deployment{ID: "old", ProjectID: p.ID, StartedAt: time.Now(), Status: "success", Commit: "abc1234"})

	if _, err := svc.Rollback(p.ID, "old"); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("want too-old error, got %v", err)
	}
	if _, err := svc.Rollback(p.ID, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got %v", err)
	}
}

func TestLogRangesAreValidated(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	if _, err := svc.StreamLogs("p1", 200, "2y"); err == nil {
		t.Error("unknown range accepted")
	}
	if _, err := svc.DownloadLogs("p1", "forever"); err == nil {
		t.Error("unknown range accepted for download")
	}
}
