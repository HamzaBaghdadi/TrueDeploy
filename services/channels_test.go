package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"truedeploy/internal/alerts"
	"truedeploy/internal/store"
)

func TestChannelsKeepSecretsAndDeliver(t *testing.T) {
	var mu sync.Mutex
	var events []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		events = append(events, r.URL.Path+" "+m["event"].(string))
		mu.Unlock()
		if strings.Contains(r.URL.Path, "broken") {
			w.WriteHeader(410)
		}
	}))
	defer srv.Close()

	m := NewMonitorService(newTestStore(t), nil, nil)
	m.hub.client = srv.Client()

	v, err := m.SaveChannel(ChannelInput{Kind: "webhook", Name: "Ops", Enabled: true, Recoveries: true, URL: srv.URL + "/ops?key=SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(v.Target, "SECRET") {
		t.Errorf("view leaks the secret: %+v", v)
	}
	// Editing without the URL keeps it.
	if _, err := m.SaveChannel(ChannelInput{ID: v.ID, Kind: "webhook", Name: "Ops team", Enabled: true, Recoveries: true}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.store.GetChannel(v.ID); c.URL != srv.URL+"/ops?key=SECRET" || c.Name != "Ops team" {
		t.Errorf("secret not kept: %+v", c)
	}
	if _, err := m.SaveChannel(ChannelInput{Kind: "webhook", Name: "Critical", Enabled: true, CriticalOnly: true, URL: srv.URL + "/crit"}); err != nil {
		t.Fatal(err)
	}
	broken, _ := m.SaveChannel(ChannelInput{Kind: "webhook", Name: "Old", Enabled: true, URL: srv.URL + "/broken"})
	if _, err := m.SaveChannel(ChannelInput{Kind: "webhook", Name: "Off", Enabled: false, URL: srv.URL + "/off"}); err != nil {
		t.Fatal(err)
	}

	warn := alerts.Alert{Key: "disk:s1", Severity: alerts.Warning, Title: "Disk"}
	crit := alerts.Alert{Key: "down:s1", Severity: alerts.Critical, Title: "Down"}
	list, _ := m.store.ListChannels()
	m.hub.dispatch(list, messages([]alerts.Alert{warn, crit}, []alerts.Alert{warn}, time.Now()))

	got := map[string]int{}
	for _, e := range events {
		got[e]++
	}
	want := map[string]int{
		"/ops alert.opened": 2, "/ops alert.resolved": 1, // everything
		"/crit alert.opened":   1, // critical only, no recoveries
		"/broken alert.opened": 1, // stops after the first failure
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("%s: got %d, want %d (all: %v)", k, got[k], n, events)
		}
	}
	if len(events) != 5 {
		t.Errorf("unexpected deliveries: %v", events)
	}

	views, _ := m.ListChannels()
	for _, cv := range views {
		switch cv.ID {
		case v.ID:
			if cv.LastSent.IsZero() || cv.LastError != "" {
				t.Errorf("ops status: %+v", cv)
			}
		case broken.ID:
			if !strings.Contains(cv.LastError, "410") || strings.Contains(cv.LastError, "SECRET") {
				t.Errorf("broken status: %+v", cv)
			}
		}
	}

	// Tests work for unsaved channels and report failures.
	if msg, err := m.TestChannel(ChannelInput{Kind: "webhook", Name: "New", URL: srv.URL + "/new"}); err != nil || !strings.Contains(msg, "sent to New") {
		t.Errorf("test: %q %v", msg, err)
	}
	if _, err := m.TestChannel(ChannelInput{ID: broken.ID, Kind: "webhook", Name: "Old"}); err == nil {
		t.Error("test to a broken channel should fail")
	}
	if _, err := m.TestChannel(ChannelInput{Kind: "discord", Name: "x", URL: "https://example.com/hook"}); err == nil {
		t.Error("invalid channels can't be tested")
	}

	if err := m.DeleteChannel(v.ID); err != nil {
		t.Fatal(err)
	}
	if views, _ := m.ListChannels(); len(views) != 3 {
		t.Errorf("after delete: %d channels", len(views))
	}
}

func TestDeployMessage(t *testing.T) {
	p := store.Project{ID: "p1", Name: "shop", ServerID: "s1"}
	ok := deployMessage(p, store.Deployment{Status: "success", Commit: "a1b2c3d", Message: "Add cart"})
	if ok.Alert.Severity != "" || ok.Alert.Title != "Deployed shop (a1b2c3d)" || ok.Alert.Detail != "Add cart" {
		t.Errorf("success: %+v", ok.Alert)
	}
	f := deployMessage(p, store.Deployment{Status: "failed", Commit: "bad", Error: "build failed"})
	if f.Alert.Severity != alerts.Critical || f.Alert.Detail != "build failed" {
		t.Errorf("failed: %+v", f.Alert)
	}
	r := deployMessage(p, store.Deployment{Status: "failed", Commit: "bad", Unhealthy: "nothing answered", RolledBackTo: "good"})
	if r.Alert.Severity != alerts.Warning || !strings.Contains(r.Alert.Title, "rolled back to good") || !strings.Contains(r.Alert.Detail, "nothing answered") {
		t.Errorf("rollback: %+v", r.Alert)
	}
}
