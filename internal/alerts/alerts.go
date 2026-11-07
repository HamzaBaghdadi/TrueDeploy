// Package alerts turns periodic observations of servers and projects into
// alerts. It is pure logic: the monitor service gathers observations over
// SSH, calls Evaluate, and feeds the result to a Tracker, which decides what
// newly opened or resolved (with flap protection) so notifications fire once.
package alerts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"truedeploy/internal/monitor"
)

type Severity string

const (
	Critical Severity = "critical"
	Warning  Severity = "warning"
)

type Alert struct {
	Key       string    `json:"key"` // stable identity, e.g. "disk:<server>"
	Severity  Severity  `json:"severity"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	ServerID  string    `json:"serverId"`
	ProjectID string    `json:"projectId,omitempty"`
	Since     time.Time `json:"since"`
}

type ServerObs struct {
	ID, Name   string
	Err        string // non-empty: couldn't connect
	Metrics    *monitor.Metrics
	Containers []monitor.Container
	// Jobs are the last runs of scheduled jobs, by project ID.
	Jobs map[string][]JobRun
}

type ProjectObs struct {
	ID, Name, Slug, ServerID string
	// Deployed: at least one successful deploy, so containers are expected.
	Deployed bool
	// Deploying: a deploy is running; container state is in flux, skip it.
	Deploying bool
	// LastAutoDeploy is the newest auto-deploy run recorded on the server.
	LastAutoDeploy *AutoRun
	// Sites are HTTPS checks of the project's domains (only domains whose
	// DNS points at the server, so domains still being set up stay quiet).
	Sites []SiteObs
}

// SiteObs is one HTTPS check of a domain.
type SiteObs struct {
	Domain string
	Up     bool
	Status int    // 0: no answer
	Err    string // why it failed, if it did
	// Ms is how long the check took (for response-time history).
	Ms int
	// CertDays is the number of days until the certificate expires (-1: unknown).
	CertDays int
}

// JobRun is the last finished run of a scheduled job.
type JobRun struct {
	ID, Name string
	Exit     int
	At       time.Time
}

type AutoRun struct {
	Status  string
	Commit  string
	Message string
	At      time.Time
}

const (
	diskWarn = 90
	diskCrit = 97
	memWarn  = 95
)

// Evaluate returns the alerts that hold right now.
func Evaluate(servers []ServerObs, projects []ProjectObs) []Alert {
	var out []Alert
	reachable := map[string]ServerObs{}
	for _, s := range servers {
		if s.Err != "" {
			out = append(out, Alert{Key: "down:" + s.ID, Severity: Critical, ServerID: s.ID,
				Title: "Can't reach " + s.Name, Detail: s.Err})
			continue
		}
		reachable[s.ID] = s
		if m := s.Metrics; m != nil && m.DiskTotal > 0 {
			p := int(100 * m.DiskUsed / m.DiskTotal)
			if p >= diskWarn {
				sev := Warning
				if p >= diskCrit {
					sev = Critical
				}
				out = append(out, Alert{Key: "disk:" + s.ID, Severity: sev, ServerID: s.ID,
					Title:  fmt.Sprintf("%s disk is %d%% full", s.Name, p),
					Detail: "Clean up unused Docker images on the server dashboard, or grow the disk. Deploys fail when it's full."})
			}
			if m.MemTotal > 0 {
				if p := int(100 * m.MemUsed / m.MemTotal); p >= memWarn {
					out = append(out, Alert{Key: "mem:" + s.ID, Severity: Warning, ServerID: s.ID,
						Title: fmt.Sprintf("%s memory is %d%% used", s.Name, p), Detail: "Apps may be killed when it runs out."})
				}
			}
		}
	}

	for _, p := range projects {
		s, ok := reachable[p.ServerID]
		if !ok {
			continue // the server alert already covers it
		}
		appDown := false
		if p.Deployed && !p.Deploying && s.Containers != nil {
			var running, restarting, stopped []string
			for _, c := range s.Containers {
				if c.Project != p.Slug {
					continue
				}
				switch c.State {
				case "running":
					running = append(running, c.Name)
				case "restarting":
					restarting = append(restarting, c.Name)
				default:
					stopped = append(stopped, c.Name)
				}
			}
			switch {
			case len(running) == 0 && len(restarting) == 0:
				detail := "No container of this project is running."
				if len(stopped) > 0 {
					detail = "Stopped: " + strings.Join(stopped, ", ") + "."
				}
				appDown = true
				out = append(out, Alert{Key: "app-down:" + p.ID, Severity: Critical, ServerID: p.ServerID, ProjectID: p.ID,
					Title: p.Name + " is down", Detail: detail + " Check its logs, or deploy again."})
			case len(restarting) > 0:
				out = append(out, Alert{Key: "app-restarting:" + p.ID, Severity: Warning, ServerID: p.ServerID, ProjectID: p.ID,
					Title: p.Name + " keeps restarting", Detail: "Crash-looping: " + strings.Join(restarting, ", ") + ". Its logs usually say why."})
			case len(stopped) > 0:
				out = append(out, Alert{Key: "app-partial:" + p.ID, Severity: Warning, ServerID: p.ServerID, ProjectID: p.ID,
					Title: p.Name + " is partly down", Detail: "Stopped: " + strings.Join(stopped, ", ") + "."})
			}
		}
		if p.Deployed && !p.Deploying {
			out = append(out, siteAlerts(p, appDown)...)
		}
		for _, j := range s.Jobs[p.ID] {
			if j.Exit == 0 {
				continue
			}
			why := fmt.Sprintf("Its last run (%s UTC) exited with code %d.", j.At.UTC().Format("Jan 2 15:04"), j.Exit)
			if j.Exit == 124 {
				why = fmt.Sprintf("Its last run (%s UTC) took longer than its time limit and was stopped.", j.At.UTC().Format("Jan 2 15:04"))
			}
			out = append(out, Alert{Key: fmt.Sprintf("job:%s:%s:%d", p.ID, j.ID, j.At.Unix()), Severity: Warning, ServerID: p.ServerID, ProjectID: p.ID,
				Title: "Scheduled job \"" + j.Name + "\" of " + p.Name + " failed", Detail: why + " Its log is on the project page."})
		}
		if r := p.LastAutoDeploy; r != nil && r.Status == "failed" {
			commit := r.Commit
			if commit == "" {
				commit = "the latest push"
			}
			out = append(out, Alert{Key: "autodeploy:" + p.ID + ":" + r.At.UTC().Format(time.RFC3339), Severity: Warning,
				ServerID: p.ServerID, ProjectID: p.ID,
				Title: "Auto-deploy of " + p.Name + " failed", Detail: fmt.Sprintf("Deploying %s failed: %s", commit, r.Message)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Tracker remembers alerts between checks. A problem must be seen in
// Confirm consecutive checks before it opens, which filters out blips
// (a dropped connection, containers restarting during a deploy).
type Tracker struct {
	Confirm   int
	active    map[string]Alert
	streak    map[string]int
	dismissed map[string]bool // silenced until the problem goes away
}

func NewTracker() *Tracker {
	return &Tracker{Confirm: 2, active: map[string]Alert{}, streak: map[string]int{}, dismissed: map[string]bool{}}
}

// Update applies the alerts from one check and reports what changed.
func (t *Tracker) Update(now time.Time, current []Alert) (opened, resolved []Alert) {
	seen := map[string]bool{}
	for _, a := range current {
		seen[a.Key] = true
		if t.dismissed[a.Key] {
			continue
		}
		if prev, ok := t.active[a.Key]; ok {
			a.Since = prev.Since
			t.active[a.Key] = a // keep wording/severity up to date
			continue
		}
		t.streak[a.Key]++
		// Failed auto-deploys and job runs are facts, not flaky readings: open right away.
		if t.streak[a.Key] >= t.Confirm || strings.HasPrefix(a.Key, "autodeploy:") || strings.HasPrefix(a.Key, "job:") {
			a.Since = now
			t.active[a.Key] = a
			delete(t.streak, a.Key)
			opened = append(opened, a)
		}
	}
	for k := range t.streak {
		if !seen[k] {
			delete(t.streak, k)
		}
	}
	for k := range t.dismissed {
		if !seen[k] {
			delete(t.dismissed, k) // recovered: alert again if it comes back
		}
	}
	for k, a := range t.active {
		if !seen[k] {
			delete(t.active, k)
			resolved = append(resolved, a)
		}
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Key < resolved[j].Key })
	return opened, resolved
}

// Active lists open alerts, critical first, then newest.
func (t *Tracker) Active() []Alert {
	out := make([]Alert, 0, len(t.active))
	for _, a := range t.active {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == Critical
		}
		return out[i].Since.After(out[j].Since)
	})
	return out
}

// Dismiss silences an alert until the problem goes away; if it comes back
// later, it alerts again.
func (t *Tracker) Dismiss(key string) {
	delete(t.active, key)
	delete(t.streak, key)
	t.dismissed[key] = true
}

const (
	certWarnDays = 14 // Caddy renews 30 days ahead, so this means renewal is failing
	certCritDays = 3
)

// siteAlerts turns HTTPS checks into "site down" and certificate alerts.
// A site that's down because the app is down is already covered by the
// app-down alert, so it isn't repeated.
func siteAlerts(p ProjectObs, appDown bool) []Alert {
	var out []Alert
	for _, s := range p.Sites {
		if !s.Up && !appDown {
			detail := "It answered " + fmt.Sprint(s.Status) + "."
			switch {
			case s.Err != "":
				detail = "Visitors can't reach it: " + s.Err + "."
			case s.Status == 502 || s.Status == 504:
				detail = fmt.Sprintf("The proxy answers %d: it can't reach the app. Check the app's port and logs.", s.Status)
			case s.Status >= 500:
				detail = fmt.Sprintf("The app answers %d (server error). Its logs usually say why.", s.Status)
			}
			out = append(out, Alert{Key: "site:" + p.ID + ":" + s.Domain, Severity: Critical, ServerID: p.ServerID, ProjectID: p.ID,
				Title: s.Domain + " is down", Detail: detail})
		}
		if s.CertDays >= 0 && s.CertDays < certWarnDays {
			sev := Warning
			if s.CertDays < certCritDays {
				sev = Critical
			}
			when := fmt.Sprintf("expires in %d days", s.CertDays)
			switch s.CertDays {
			case 0:
				when = "expires today"
			case 1:
				when = "expires tomorrow"
			}
			host, _, _ := strings.Cut(s.Domain, "/") // one certificate per host, whatever the path
			out = append(out, Alert{Key: "cert:" + host, Severity: sev, ServerID: p.ServerID, ProjectID: p.ID,
				Title:  "HTTPS certificate for " + host + " " + when,
				Detail: "Certificates renew automatically 30 days before they expire, so renewal is failing. Check that port 80 is open and the domain still points at the server, then apply the domains again."})
		}
	}
	return out
}
