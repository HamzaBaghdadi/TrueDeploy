package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAddPersistTrimAndStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	s := Open(path)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	keepSrv := map[string]bool{"s1": true}
	key := SiteKey("p1", "shop.io")
	keepSites := map[string]bool{key: true}

	// 8 days of checks every 5 minutes; the site is down for one hour on the last day.
	for i := 0; i < 8*24*12; i++ {
		now := start.Add(time.Duration(i) * 5 * time.Minute)
		down := now.After(start.Add(7*24*time.Hour+10*time.Hour)) && now.Before(start.Add(7*24*time.Hour+11*time.Hour))
		site := SitePoint{Up: !down, Ms: 120, Status: 200}
		if down {
			site = SitePoint{Up: false, Status: 502}
		}
		if err := s.Add(now, map[string]ServerPoint{"s1": {Up: true, CPU: 20, Mem: 50, Disk: 70, Load: 0.5}},
			map[string]SitePoint{key: site}, keepSrv, keepSites); err != nil {
			t.Fatal(err)
		}
	}
	end := start.Add(8*24*time.Hour - 5*time.Minute)

	// Older than 7 days is gone, and it survives a reload.
	r := Open(path)
	all := r.ServerSeries("s1", end.Add(-30*24*time.Hour), 1_000_000)
	if len(all) == 0 || time.Unix(all[0].T, 0).Before(end.Add(-Keep)) {
		t.Fatalf("not trimmed: first %v", time.Unix(all[0].T, 0))
	}

	day := r.ServerSeries("s1", end.Add(-24*time.Hour), 48)
	if len(day) < 40 || len(day) > 48 || day[0].CPU != 20 || day[0].Mem != 50 || !day[0].Up {
		t.Errorf("24h buckets: %d %+v", len(day), day[0])
	}

	st := r.SiteStats(key, end.Add(-24*time.Hour), 96)
	// 11 down checks out of ~288 in the last 24h (the start is inclusive).
	if st.Checks < 288 || st.Checks > 289 || st.Uptime < 95 || st.Uptime > 97 || st.AvgMs != 120 {
		t.Errorf("stats: %+v", st)
	}
	downBuckets := 0
	for _, p := range st.Points {
		if !p.Up {
			downBuckets++
		}
	}
	if downBuckets < 3 || downBuckets > 6 {
		t.Errorf("the outage should show in a few buckets, got %d", downBuckets)
	}
	if none := r.SiteStats("p9|x.io", end.Add(-time.Hour), 10); none.Uptime != -1 || none.Checks != 0 {
		t.Errorf("no data: %+v", none)
	}

	// Deleted servers/sites are dropped on the next round.
	if err := r.Add(end.Add(5*time.Minute), nil, nil, map[string]bool{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if len(r.ServerSeries("s1", start, 10)) != 0 || r.SiteStats(key, start, 10).Checks != 0 {
		t.Error("removed series should be dropped")
	}
}

func TestUnreadableFileStartsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	s := Open(path)
	if err := s.Add(time.Now(), map[string]ServerPoint{"s1": {Up: false}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if pts := s.ServerSeries("s1", time.Now().Add(-time.Hour), 10); len(pts) != 1 || pts[0].Up {
		t.Errorf("points: %+v", pts)
	}
}
