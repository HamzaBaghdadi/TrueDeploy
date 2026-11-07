package alerts

import (
	"strings"
	"testing"
	"time"

	"truedeploy/internal/monitor"
)

const gb = int64(1 << 30)

func healthy() ServerObs {
	return ServerObs{ID: "s1", Name: "vps", Metrics: &monitor.Metrics{DiskTotal: 100 * gb, DiskUsed: 40 * gb, MemTotal: 4 * gb, MemUsed: 2 * gb},
		Containers: []monitor.Container{{Name: "api-web-1", State: "running", Project: "api"}}}
}

func api() ProjectObs {
	return ProjectObs{ID: "p1", Name: "API", Slug: "api", ServerID: "s1", Deployed: true}
}

func keys(as []Alert) string {
	var k []string
	for _, a := range as {
		k = append(k, a.Key)
	}
	return strings.Join(k, ",")
}

func TestEvaluate(t *testing.T) {
	if got := Evaluate([]ServerObs{healthy()}, []ProjectObs{api()}); len(got) != 0 {
		t.Fatalf("healthy setup should have no alerts: %+v", got)
	}

	full := healthy()
	full.Metrics.DiskUsed = 98 * gb
	full.Metrics.MemUsed = 39 * gb / 10
	got := Evaluate([]ServerObs{full}, nil)
	if keys(got) != "disk:s1,mem:s1" || got[0].Severity != Critical || !strings.Contains(got[0].Title, "98%") {
		t.Fatalf("disk/mem alerts: %+v", got)
	}

	down := ServerObs{ID: "s1", Name: "vps", Err: "connection timed out"}
	if got := Evaluate([]ServerObs{down}, []ProjectObs{api()}); keys(got) != "down:s1" {
		t.Fatalf("an unreachable server should only raise the server alert: %+v", got)
	}

	stopped := healthy()
	stopped.Containers = []monitor.Container{{Name: "api-web-1", State: "exited", Project: "api"}, {Name: "other", State: "exited", Project: "other"}}
	got = Evaluate([]ServerObs{stopped}, []ProjectObs{api()})
	if keys(got) != "app-down:p1" || !strings.Contains(got[0].Detail, "api-web-1") || strings.Contains(got[0].Detail, "other") {
		t.Fatalf("app-down: %+v", got)
	}

	deploying := api()
	deploying.Deploying = true
	if got := Evaluate([]ServerObs{stopped}, []ProjectObs{deploying}); len(got) != 0 {
		t.Fatalf("containers are ignored while deploying: %+v", got)
	}
	neverDeployed := api()
	neverDeployed.Deployed = false
	if got := Evaluate([]ServerObs{stopped}, []ProjectObs{neverDeployed}); len(got) != 0 {
		t.Fatalf("a project that was never deployed isn't 'down': %+v", got)
	}

	loop := healthy()
	loop.Containers = []monitor.Container{{Name: "api-web-1", State: "restarting", Project: "api"}}
	if got := Evaluate([]ServerObs{loop}, []ProjectObs{api()}); keys(got) != "app-restarting:p1" {
		t.Fatalf("restart loop: %+v", got)
	}

	failed := api()
	failed.LastAutoDeploy = &AutoRun{Status: "failed", Commit: "abc1234", Message: "No compose.yaml", At: time.Unix(100, 0)}
	got = Evaluate([]ServerObs{healthy()}, []ProjectObs{failed})
	if len(got) != 1 || !strings.HasPrefix(got[0].Key, "autodeploy:p1:") || !strings.Contains(got[0].Detail, "abc1234") {
		t.Fatalf("auto-deploy failure: %+v", got)
	}
}

func TestTrackerConfirmsAndResolves(t *testing.T) {
	tr := NewTracker()
	now := time.Now()
	down := []Alert{{Key: "down:s1", Title: "Can't reach vps"}}

	if o, _ := tr.Update(now, down); len(o) != 0 {
		t.Fatal("one bad check must not open an alert (flap protection)")
	}
	if o, _ := tr.Update(now, nil); len(o) != 0 {
		t.Fatal("a blip that recovered must not open an alert")
	}
	tr.Update(now, down)
	o, _ := tr.Update(now.Add(time.Minute), down)
	if keys(o) != "down:s1" || len(tr.Active()) != 1 {
		t.Fatalf("two bad checks in a row should open it: %v", keys(o))
	}
	if o, _ := tr.Update(now.Add(2*time.Minute), down); len(o) != 0 {
		t.Fatal("an open alert must not notify again")
	}
	if !tr.Active()[0].Since.Equal(now.Add(time.Minute)) {
		t.Fatal("Since should be when it opened")
	}
	_, r := tr.Update(now.Add(3*time.Minute), nil)
	if keys(r) != "down:s1" || len(tr.Active()) != 0 {
		t.Fatalf("recovery should resolve it: %v", keys(r))
	}
}

func TestAutoDeployFailureOpensImmediately(t *testing.T) {
	tr := NewTracker()
	o, _ := tr.Update(time.Now(), []Alert{{Key: "autodeploy:p1:2026-11-05T10:00:00Z"}})
	if len(o) != 1 {
		t.Fatal("a failed auto-deploy is a fact, not a flaky reading: alert at once")
	}
}

func TestDismissSilencesUntilRecovery(t *testing.T) {
	tr := NewTracker()
	disk := []Alert{{Key: "disk:s1"}}
	tr.Update(time.Now(), disk)
	tr.Update(time.Now(), disk)
	tr.Dismiss("disk:s1")
	for i := 0; i < 3; i++ {
		if o, _ := tr.Update(time.Now(), disk); len(o) != 0 || len(tr.Active()) != 0 {
			t.Fatal("a dismissed alert must stay quiet while the problem persists")
		}
	}
	tr.Update(time.Now(), nil) // recovered
	tr.Update(time.Now(), disk)
	if o, _ := tr.Update(time.Now(), disk); len(o) != 1 {
		t.Fatal("after recovering, the same problem should alert again")
	}
}

func TestActiveOrder(t *testing.T) {
	tr := NewTracker()
	tr.Confirm = 1
	t0 := time.Now()
	tr.Update(t0, []Alert{{Key: "mem:s1", Severity: Warning}})
	tr.Update(t0.Add(time.Minute), []Alert{{Key: "mem:s1", Severity: Warning}, {Key: "down:s2", Severity: Critical}, {Key: "disk:s1", Severity: Warning}})
	if got := keys(tr.Active()); got != "down:s2,disk:s1,mem:s1" {
		t.Fatalf("critical first, then newest: %s", got)
	}
}

func TestSiteAndCertAlerts(t *testing.T) {
	p := api()
	p.Sites = []SiteObs{
		{Domain: "api.example.com", Up: true, Status: 200, CertDays: 60},
		{Domain: "www.example.com", Up: false, Status: 502, CertDays: 10},
		{Domain: "old.example.com", Up: false, Err: "nothing is listening on port 443", CertDays: -1},
	}
	got := Evaluate([]ServerObs{healthy()}, []ProjectObs{p})
	if keys(got) != "cert:www.example.com,site:p1:old.example.com,site:p1:www.example.com" {
		t.Fatalf("keys: %s", keys(got))
	}
	byKey := map[string]Alert{}
	for _, a := range got {
		byKey[a.Key] = a
	}
	if a := byKey["site:p1:www.example.com"]; a.Severity != Critical || !strings.Contains(a.Detail, "502") || a.ProjectID != "p1" {
		t.Errorf("502 alert: %+v", a)
	}
	if a := byKey["site:p1:old.example.com"]; !strings.Contains(a.Detail, "port 443") {
		t.Errorf("unreachable alert: %+v", a)
	}
	if a := byKey["cert:www.example.com"]; a.Severity != Warning || !strings.Contains(a.Title, "expires in 10 days") {
		t.Errorf("cert warning: %+v", a)
	}

	p.Sites = []SiteObs{{Domain: "api.example.com", Up: true, Status: 200, CertDays: 1}}
	if got := Evaluate([]ServerObs{healthy()}, []ProjectObs{p}); len(got) != 1 || got[0].Severity != Critical || !strings.Contains(got[0].Title, "tomorrow") {
		t.Errorf("cert critical: %+v", got)
	}

	// App down: the site-down alert would only repeat it.
	down := healthy()
	down.Containers = nil
	down.Containers = []monitor.Container{{Name: "api-web-1", State: "exited", Project: "api"}}
	p.Sites = []SiteObs{{Domain: "api.example.com", Up: false, Status: 502, CertDays: 60}}
	if got := Evaluate([]ServerObs{down}, []ProjectObs{p}); keys(got) != "app-down:p1" {
		t.Errorf("app down should cover the site: %s", keys(got))
	}
	// Mid-deploy or never deployed: no site alerts.
	p.Deploying = true
	if got := Evaluate([]ServerObs{healthy()}, []ProjectObs{p}); len(got) != 0 {
		t.Errorf("deploying: %s", keys(got))
	}
}

func TestFailedJobAlerts(t *testing.T) {
	s := healthy()
	at := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	s.Jobs = map[string][]JobRun{"p1": {
		{ID: "j1", Name: "cleanup", Exit: 0, At: at},
		{ID: "j2", Name: "digest", Exit: 2, At: at},
		{ID: "j3", Name: "import", Exit: 124, At: at},
	}}
	got := Evaluate([]ServerObs{s}, []ProjectObs{api()})
	if len(got) != 2 || !strings.Contains(got[0].Title, `"digest" of API failed`) || !strings.Contains(got[0].Detail, "code 2") || !strings.Contains(got[1].Detail, "time limit") {
		t.Fatalf("job alerts: %+v", got)
	}
	// Facts open immediately and stay until the next run changes the result.
	tr := NewTracker()
	if opened, _ := tr.Update(at, got); len(opened) != 2 {
		t.Errorf("job alerts should open on the first check, got %d", len(opened))
	}
	s.Jobs["p1"] = []JobRun{{ID: "j2", Name: "digest", Exit: 0, At: at.Add(time.Hour)}}
	if _, resolved := tr.Update(at.Add(time.Hour), Evaluate([]ServerObs{s}, []ProjectObs{api()})); len(resolved) != 2 {
		t.Errorf("a successful run (and a gone job) should resolve them, got %d", len(resolved))
	}
}
