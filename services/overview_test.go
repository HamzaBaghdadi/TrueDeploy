package services

import (
	"testing"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/monitor"
	"truedeploy/internal/store"
)

func TestOverview(t *testing.T) {
	st := newTestStore(t)
	projects := NewProjectService(st, nil)
	m := NewMonitorService(st, projects, nil)
	_ = st.PutProject(store.Project{ID: "p1", Name: "web", ServerID: "s1", Domains: []string{"shop.io"}, CreatedAt: time.Now()}, nil)
	_ = st.PutProject(store.Project{ID: "p2", Name: "api", ServerID: "s1", CreatedAt: time.Now()}, nil)
	_ = st.SaveDeployment(store.Deployment{ID: "d1", ProjectID: "p2", Status: "failed", StartedAt: time.Now(), Commit: "abc"})
	ps, _ := st.ListProjects()
	srvs, _ := st.List()
	m.record(time.Now(), srvs, ps,
		[]alerts.ServerObs{{ID: "s1", Metrics: &monitor.Metrics{CPUPercent: 40, MemTotal: 100, MemUsed: 50, DiskTotal: 100, DiskUsed: 30}}},
		[]alerts.ProjectObs{{ID: "p1", Sites: []alerts.SiteObs{{Domain: "shop.io", Up: true, Status: 200, Ms: 90}}}})
	m.mu.Lock()
	m.tracker.Update(time.Now(), []alerts.Alert{{Key: "app-down:p1", Severity: alerts.Critical, ServerID: "s1", ProjectID: "p1", Title: "web is down"}})
	m.tracker.Update(time.Now(), []alerts.Alert{{Key: "app-down:p1", Severity: alerts.Critical, ServerID: "s1", ProjectID: "p1", Title: "web is down"}})
	m.mu.Unlock()

	o, err := m.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Servers) != 1 || o.Servers[0].Last == nil || o.Servers[0].Last.CPU != 40 || len(o.Servers[0].CPU) != 1 {
		t.Errorf("servers: %+v", o.Servers)
	}
	if len(o.Projects) != 2 || o.Projects[0].ID != "p1" || !o.Projects[0].Critical || o.Projects[0].Uptime24h != 100 {
		t.Errorf("projects (critical first): %+v", o.Projects)
	}
	if o.Projects[1].LastStatus != "failed" || o.Projects[1].Uptime24h != -1 {
		t.Errorf("api: %+v", o.Projects[1])
	}
	if len(o.Alerts) != 1 || len(o.Deploys) != 1 {
		t.Errorf("alerts/deploys: %d %d", len(o.Alerts), len(o.Deploys))
	}
}
