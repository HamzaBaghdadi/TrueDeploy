package services

import (
	"testing"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/monitor"
	"truedeploy/internal/store"
)

func TestMonitorRecordsHistory(t *testing.T) {
	st := newTestStore(t)
	m := NewMonitorService(st, NewProjectService(st, nil), nil)
	_ = st.PutProject(store.Project{ID: "p1", Name: "shop", ServerID: "s1", Domains: []string{"shop.io"}}, nil)
	projects, _ := st.ListProjects()
	servers, _ := st.List()
	now := time.Now()
	m.record(now.Add(-10*time.Minute), servers, projects,
		[]alerts.ServerObs{{ID: "s1", Metrics: &monitor.Metrics{CPUPercent: 37.5, MemTotal: 4 << 30, MemUsed: 1 << 30, DiskTotal: 100, DiskUsed: 81, Load: [3]float64{1.234, 0, 0}}}},
		[]alerts.ProjectObs{{ID: "p1", Sites: []alerts.SiteObs{{Domain: "shop.io", Up: true, Status: 200, Ms: 140}}}})
	m.record(now, servers, projects,
		[]alerts.ServerObs{{ID: "s1", Err: "timeout"}},
		[]alerts.ProjectObs{{ID: "p1", Sites: []alerts.SiteObs{{Domain: "shop.io", Up: false, Status: 502}}}})

	pts := m.ServerHistory("s1", 24)
	if len(pts) != 2 || pts[0].CPU != 37.5 || pts[0].Mem != 25 || pts[0].Disk != 81 || pts[0].Load != 1.23 || pts[1].Up {
		t.Errorf("server history: %+v", pts)
	}
	sh, err := m.SiteHistory("p1", 24)
	if err != nil || len(sh) != 1 || sh[0].Uptime24h != 50 || sh[0].AvgMs24h != 140 || len(sh[0].Points) != 2 {
		t.Errorf("site history: %+v %v", sh, err)
	}
}
