package services

import (
	"math"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/history"
	"truedeploy/internal/store"
)

// record saves one monitoring round to the local history.
func (m *MonitorService) record(now time.Time, servers []store.Server, projects []store.Project, obs []alerts.ServerObs, pobs []alerts.ProjectObs) {
	srv := map[string]history.ServerPoint{}
	keepSrv := map[string]bool{}
	for _, s := range servers {
		keepSrv[s.ID] = true
	}
	for _, o := range obs {
		if o.ID == "" {
			continue
		}
		p := history.ServerPoint{Up: o.Err == ""}
		if mt := o.Metrics; mt != nil && p.Up {
			p.CPU = pct(mt.CPUPercent, 100)
			if mt.MemTotal > 0 {
				p.Mem = pct(float64(mt.MemUsed), float64(mt.MemTotal))
			}
			if mt.DiskTotal > 0 {
				p.Disk = pct(float64(mt.DiskUsed), float64(mt.DiskTotal))
			}
			p.Load = float32(math.Round(mt.Load[0]*100) / 100)
		}
		srv[o.ID] = p
	}
	sites := map[string]history.SitePoint{}
	keepSites := map[string]bool{}
	for _, p := range projects {
		for _, d := range p.Domains {
			keepSites[history.SiteKey(p.ID, d)] = true
		}
	}
	for _, po := range pobs {
		for _, s := range po.Sites {
			sites[history.SiteKey(po.ID, s.Domain)] = history.SitePoint{Up: s.Up, Ms: int32(s.Ms), Status: int16(s.Status)}
		}
	}
	_ = m.hist.Add(now, srv, sites, keepSrv, keepSites)
}

// pct is v/total as a percentage with one decimal.
func pct(v, total float64) float32 {
	if total <= 0 {
		return 0
	}
	return float32(math.Round(1000*v/total) / 10)
}

// ServerHistory returns a server's load over the last hours (at most 168),
// in at most 144 points.
func (m *MonitorService) ServerHistory(serverID string, hours int) []history.ServerPoint {
	return m.hist.ServerSeries(serverID, time.Now().Add(-window(hours)), 144)
}

type SiteHistory struct {
	Domain string `json:"domain"`
	// Uptime24h / Uptime7d are percentages (-1: no checks yet).
	Uptime24h float64 `json:"uptime24h"`
	Uptime7d  float64 `json:"uptime7d"`
	AvgMs24h  int     `json:"avgMs24h"`
	Checks7d  int     `json:"checks7d"`
	// Points cover the requested window.
	Points []history.SitePoint `json:"points"`
}

// SiteHistory returns uptime and response times of a project's domains.
func (m *MonitorService) SiteHistory(projectID string, hours int) ([]SiteHistory, error) {
	p, _, err := m.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := []SiteHistory{}
	for _, d := range p.Domains {
		key := history.SiteKey(p.ID, d)
		day := m.hist.SiteStats(key, now.Add(-24*time.Hour), 0)
		week := m.hist.SiteStats(key, now.Add(-7*24*time.Hour), 0)
		win := m.hist.SiteStats(key, now.Add(-window(hours)), 96)
		out = append(out, SiteHistory{Domain: d, Uptime24h: day.Uptime, Uptime7d: week.Uptime, AvgMs24h: day.AvgMs, Checks7d: week.Checks, Points: win.Points})
	}
	return out, nil
}

func window(hours int) time.Duration {
	if hours < 1 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}
