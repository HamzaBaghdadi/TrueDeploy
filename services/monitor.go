package services

import (
	"context"
	"net"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"truedeploy/internal/alerts"
	"truedeploy/internal/autodeploy"
	"truedeploy/internal/history"
	"truedeploy/internal/jobs"
	"truedeploy/internal/monitor"
	"truedeploy/internal/proxy"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

const EventAlertsChanged = "alerts:changed"

type MonitorStatus struct {
	Alerts    []alerts.Alert `json:"alerts"`
	LastCheck time.Time      `json:"lastCheck"`
	Checking  bool           `json:"checking"`
	Enabled   bool           `json:"enabled"`
}

func init() {
	application.RegisterEvent[MonitorStatus](EventAlertsChanged)
}

// MonitorService checks servers and projects in the background while the app
// runs (including when it's hidden in the tray) and raises alerts.
type MonitorService struct {
	store    *store.Store
	projects *ProjectService
	notifier *notifications.NotificationService
	hub      *channelHub
	hist     *history.Store

	mu        sync.Mutex
	tracker   *alerts.Tracker
	lastCheck time.Time
	checking  bool
	trigger   chan struct{}

	// OnChange lets the tray show the alert count.
	OnChange func(active []alerts.Alert)
}

func NewMonitorService(st *store.Store, projects *ProjectService, notifier *notifications.NotificationService) *MonitorService {
	return &MonitorService{store: st, projects: projects, notifier: notifier, tracker: alerts.NewTracker(), hub: newChannelHub(), hist: history.Open(filepath.Join(st.Dir(), "history.json")), trigger: make(chan struct{}, 1)}
}

func (m *MonitorService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	go m.loop(ctx)
	return nil
}

func (m *MonitorService) loop(ctx context.Context) {
	// Let the window come up before the first round of SSH connections.
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.trigger:
		}
		set, _ := m.store.Settings()
		if set.MonitorEnabled {
			m.check(ctx, set)
		}
		interval := time.Duration(set.IntervalMinutes) * time.Minute
		if interval < time.Minute {
			interval = 5 * time.Minute
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)
	}
}

// CheckNow starts a check right away (it runs in the background).
func (m *MonitorService) CheckNow() {
	select {
	case m.trigger <- struct{}{}:
	default: // one is already queued
	}
}

func (m *MonitorService) Status() MonitorStatus {
	set, _ := m.store.Settings()
	m.mu.Lock()
	defer m.mu.Unlock()
	return MonitorStatus{Alerts: m.tracker.Active(), LastCheck: m.lastCheck, Checking: m.checking, Enabled: set.MonitorEnabled}
}

// Dismiss silences an alert until the problem goes away.
func (m *MonitorService) Dismiss(key string) MonitorStatus {
	m.mu.Lock()
	m.tracker.Dismiss(key)
	m.mu.Unlock()
	m.changed()
	return m.Status()
}

func (m *MonitorService) GetSettings() (store.Settings, error) { return m.store.Settings() }

func (m *MonitorService) SaveSettings(s store.Settings) (store.Settings, error) {
	if s.IntervalMinutes < 1 {
		s.IntervalMinutes = 1
	}
	if s.IntervalMinutes > 60 {
		s.IntervalMinutes = 60
	}
	if err := m.store.SaveSettings(s); err != nil {
		return s, err
	}
	if !s.MonitorEnabled {
		// Clear alerts so nothing stale lingers while monitoring is off.
		m.mu.Lock()
		m.tracker = alerts.NewTracker()
		m.mu.Unlock()
		m.changed()
	} else {
		m.CheckNow()
	}
	return s, nil
}

func (m *MonitorService) changed() {
	st := m.Status()
	if app := application.Get(); app != nil {
		app.Event.Emit(EventAlertsChanged, st)
	}
	if m.OnChange != nil {
		m.OnChange(st.Alerts)
	}
}

func (m *MonitorService) check(ctx context.Context, set store.Settings) {
	m.mu.Lock()
	m.checking = true
	m.mu.Unlock()
	m.changed()

	servers, _ := m.store.List()
	projects, _ := m.store.ListProjects()

	obs := make([]alerts.ServerObs, len(servers))
	autoRuns := map[string]*alerts.AutoRun{}
	var runsMu sync.Mutex
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func(i int, srv store.Server) {
			defer wg.Done()
			var auto []store.Project
			for _, p := range projects {
				if p.ServerID == srv.ID && p.AutoDeploy {
					auto = append(auto, p)
				}
			}
			var withJobs []store.Project
			for _, p := range projects {
				if p.ServerID == srv.ID && slices.ContainsFunc(p.Jobs, func(j jobs.Job) bool { return j.Enabled }) {
					withJobs = append(withJobs, p)
				}
			}
			o, runs := m.observe(ctx, srv, auto, withJobs)
			obs[i] = o
			runsMu.Lock()
			for k, v := range runs {
				autoRuns[k] = v
			}
			runsMu.Unlock()
		}(i, srv)
	}
	wg.Wait()

	var pobs []alerts.ProjectObs
	for _, p := range projects {
		po := alerts.ProjectObs{ID: p.ID, Name: p.Name, Slug: slugOf(p), ServerID: p.ServerID, LastAutoDeploy: autoRuns[p.ID]}
		for _, d := range m.projects.deployments(p.ID) {
			if d.Status == "success" {
				po.Deployed = true
			}
			if d.Status == "running" {
				po.Deploying = true
			}
		}
		pobs = append(pobs, po)
	}
	checkSites(ctx, servers, projects, pobs)
	m.record(time.Now(), servers, projects, obs, pobs)

	now := time.Now()
	m.mu.Lock()
	opened, resolved := m.tracker.Update(now, alerts.Evaluate(obs, pobs))
	m.lastCheck = now
	m.checking = false
	m.mu.Unlock()
	m.changed()
	m.notifyChannels(opened, resolved, now)

	if set.Notify && m.notifier != nil {
		for _, a := range opened {
			_ = m.notifier.SendNotification(notifications.NotificationOptions{ID: "td-" + a.Key, Title: a.Title, Body: a.Detail})
		}
		for _, a := range resolved {
			_ = m.notifier.SendNotification(notifications.NotificationOptions{ID: "td-ok-" + a.Key, Title: "Resolved: " + a.Title, Body: "Back to normal."})
		}
	}
}

// observe connects once and gathers metrics, containers and auto-deploy history.
func (m *MonitorService) observe(ctx context.Context, srv store.Server, auto, withJobs []store.Project) (alerts.ServerObs, map[string]*alerts.AutoRun) {
	o := alerts.ServerObs{ID: srv.ID, Name: srv.Name}
	t, _, err := savedTarget(m.store, srv.ID)
	if err != nil {
		o.Err = err.Error()
		return o, nil
	}
	c, err := sshx.Dial(t, 15*time.Second)
	if err != nil {
		o.Err = friendly(err).Error()
		return o, nil
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	if lines, err := collect(ctx, c, monitor.MetricsScript); err == nil {
		met := monitor.ParseMetrics(lines)
		o.Metrics = &met
		if met.DockerReady {
			if lines, err := collect(ctx, c, monitor.ContainersScript); err == nil {
				o.Containers = monitor.ParseContainers(lines)
			}
		}
	} else {
		o.Err = "connected, but couldn't read server stats: " + strings.TrimSpace(err.Error())
	}

	runs := map[string]*alerts.AutoRun{}
	for _, p := range auto {
		lines, err := collect(ctx, c, autodeploy.HistoryScript(slugOf(p)))
		if err != nil {
			continue
		}
		if hist, _ := autodeploy.ParseHistory(lines); len(hist) > 0 {
			r := hist[0]
			runs[p.ID] = &alerts.AutoRun{Status: r.Status, Commit: r.Commit, Message: r.Message, At: r.StartedAt}
		}
	}
	for _, p := range withJobs {
		var ids []string
		names := map[string]string{}
		for _, j := range p.Jobs {
			if j.Enabled {
				ids = append(ids, j.ID)
				names[j.ID] = j.Name
			}
		}
		lines, err := collect(ctx, c, jobs.StatusScript(slugOf(p), ids))
		if err != nil {
			continue
		}
		for id, st := range jobs.ParseStatus(lines) {
			if st.Start == 0 {
				continue
			}
			if o.Jobs == nil {
				o.Jobs = map[string][]alerts.JobRun{}
			}
			o.Jobs[p.ID] = append(o.Jobs[p.ID], alerts.JobRun{ID: id, Name: names[id], Exit: st.Exit, At: time.Unix(st.Start, 0)})
		}
	}
	return o, runs
}

// checkSites requests every deployed project's domains over HTTPS, in
// parallel, filling pobs[i].Sites. Domains whose DNS doesn't point at the
// project's server are skipped: they're still being set up (or moved), and
// the Domains panel explains that better than an alert would.
func checkSites(ctx context.Context, servers []store.Server, projects []store.Project, pobs []alerts.ProjectObs) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ips := map[string][]string{}
	for _, srv := range servers {
		if ip := net.ParseIP(srv.Host); ip != nil {
			ips[srv.ID] = []string{ip.String()}
		} else if got, err := lookup(ctx, srv.Host); err == nil {
			ips[srv.ID] = got
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, p := range projects {
		// In maintenance mode the 503 page is intended: no "site down" alerts.
		if !pobs[i].Deployed || p.Maintenance || len(p.Domains) == 0 || len(ips[p.ServerID]) == 0 {
			continue
		}
		for _, d := range p.Domains {
			wg.Add(1)
			go func(i int, domain string, serverIPs []string) {
				defer wg.Done()
				host, _ := proxy.SplitRoute(domain)
				if got, err := lookup(ctx, host); err != nil || !overlaps(got, serverIPs) {
					return
				}
				r := httpsGet(ctx, "https://"+domain+"/")
				obs := alerts.SiteObs{Domain: domain, Up: r.Up(), Status: r.Status, Err: r.Err, Ms: int(r.Latency), CertDays: r.CertDaysLeft(time.Now())}
				mu.Lock()
				pobs[i].Sites = append(pobs[i].Sites, obs)
				mu.Unlock()
			}(i, d, ips[p.ServerID])
		}
	}
	wg.Wait()
	for i := range pobs {
		sort.Slice(pobs[i].Sites, func(a, b int) bool { return pobs[i].Sites[a].Domain < pobs[i].Sites[b].Domain })
	}
}
