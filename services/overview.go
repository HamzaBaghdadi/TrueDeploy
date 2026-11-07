package services

import (
	"sort"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/history"
)

type OverviewServer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	// Last is the most recent check (nil before the first one).
	Last *history.ServerPoint `json:"last,omitempty"`
	// CPU is the last 24 h of CPU readings, for a sparkline (-1 = unreachable).
	CPU      []float32 `json:"cpu"`
	Alerts   int       `json:"alerts"`
	Critical bool      `json:"critical"`
}

type OverviewProject struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ServerName string `json:"serverName"`
	Preview    bool   `json:"preview"`
	App        bool   `json:"app"`
	// LastStatus of the newest deploy ("" if never deployed).
	LastStatus string    `json:"lastStatus"`
	LastAt     time.Time `json:"lastAt,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	Domain     string    `json:"domain,omitempty"`
	// Uptime24h of the first domain (-1: no domain or no checks yet).
	Uptime24h float64 `json:"uptime24h"`
	Alerts    int     `json:"alerts"`
	Critical  bool    `json:"critical"`
}

type Overview struct {
	Servers   []OverviewServer  `json:"servers"`
	Projects  []OverviewProject `json:"projects"`
	Alerts    []alerts.Alert    `json:"alerts"` // most urgent first
	Deploys   []DeploymentEntry `json:"deploys"`
	LastCheck time.Time         `json:"lastCheck"`
	Enabled   bool              `json:"enabled"` // monitoring on
}

// Overview summarizes everything for the home page from data already
// collected (no SSH connections).
func (m *MonitorService) Overview() (*Overview, error) {
	servers, err := m.store.List()
	if err != nil {
		return nil, err
	}
	projects, err := m.store.ListProjects()
	if err != nil {
		return nil, err
	}
	st := m.Status()
	out := &Overview{Servers: []OverviewServer{}, Projects: []OverviewProject{}, Alerts: st.Alerts, LastCheck: st.LastCheck, Enabled: st.Enabled}
	if out.Alerts == nil {
		out.Alerts = []alerts.Alert{}
	}

	srvAlerts := map[string]int{}
	srvCrit := map[string]bool{}
	projAlerts := map[string]int{}
	projCrit := map[string]bool{}
	for _, a := range out.Alerts {
		if a.ProjectID != "" {
			projAlerts[a.ProjectID]++
			projCrit[a.ProjectID] = projCrit[a.ProjectID] || a.Severity == alerts.Critical
		} else {
			srvAlerts[a.ServerID]++
			srvCrit[a.ServerID] = srvCrit[a.ServerID] || a.Severity == alerts.Critical
		}
	}

	now := time.Now()
	names := map[string]string{}
	for _, s := range servers {
		names[s.ID] = s.Name
		os := OverviewServer{ID: s.ID, Name: s.Name, Host: s.Host, CPU: []float32{}, Alerts: srvAlerts[s.ID], Critical: srvCrit[s.ID]}
		pts := m.hist.ServerSeries(s.ID, now.Add(-24*time.Hour), 48)
		for _, p := range pts {
			if p.Up {
				os.CPU = append(os.CPU, p.CPU)
			} else {
				os.CPU = append(os.CPU, -1)
			}
		}
		if len(pts) > 0 {
			last := pts[len(pts)-1]
			os.Last = &last
		}
		out.Servers = append(out.Servers, os)
	}

	for _, p := range projects {
		op := OverviewProject{ID: p.ID, Name: p.Name, ServerName: names[p.ServerID], Preview: p.PreviewOf != "", App: p.IsApp(),
			Uptime24h: -1, Alerts: projAlerts[p.ID], Critical: projCrit[p.ID]}
		if deps := m.projects.deployments(p.ID); len(deps) > 0 {
			op.LastStatus, op.LastAt, op.Commit = deps[0].Status, deps[0].StartedAt, deps[0].Commit
		}
		if len(p.Domains) > 0 {
			op.Domain = p.Domains[0]
			op.Uptime24h = m.hist.SiteStats(history.SiteKey(p.ID, p.Domains[0]), now.Add(-24*time.Hour), 0).Uptime
		}
		out.Projects = append(out.Projects, op)
	}
	// Problems first, then by name.
	sort.SliceStable(out.Projects, func(i, j int) bool {
		a, b := out.Projects[i], out.Projects[j]
		ra, rb := rank(a), rank(b)
		if ra != rb {
			return ra < rb
		}
		return a.Name < b.Name
	})

	if deps, err := m.projects.RecentDeployments(6); err == nil {
		out.Deploys = deps
	}
	if out.Deploys == nil {
		out.Deploys = []DeploymentEntry{}
	}
	return out, nil
}

func rank(p OverviewProject) int {
	switch {
	case p.Critical:
		return 0
	case p.Alerts > 0 || p.LastStatus == "failed":
		return 1
	case p.Preview:
		return 3
	}
	return 2
}
