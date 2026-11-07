package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"truedeploy/internal/updates"
)

// The version lives in three places; releases are cut from all of them.
func TestVersionIsConsistent(t *testing.T) {
	cfg, err := os.ReadFile("../build/config.yml")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s+version:\s*"([^"]+)"\s*#\s*The application version`).FindSubmatch(cfg)
	if m == nil || string(m[1]) != Version {
		t.Errorf("build/config.yml info.version = %q, services.Version = %q", m, Version)
	}
	raw, err := os.ReadFile("../build/windows/info.json")
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Fixed struct {
			File    string `json:"file_version"`
			Product string `json:"product_version"`
		} `json:"fixed"`
		Info map[string]map[string]string `json:"info"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if info.Fixed.File != Version || info.Fixed.Product != Version {
		t.Errorf("info.json fixed versions %+v, want %s", info.Fixed, Version)
	}
	for lang, kv := range info.Info {
		if kv["FileVersion"] != Version || kv["ProductVersion"] != Version {
			t.Errorf("info.json %s versions %v, want %s", lang, kv, Version)
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	tag, status := "v99.0.0", 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/x/y/releases/latest"}`, tag)
	}))
	defer srv.Close()
	old := updates.API
	updates.API = srv.URL
	defer func() { updates.API = old }()

	s := NewAppService(newTestStore(t), nil)
	if st := s.CheckForUpdate(); !st.Available || st.Latest.Version != "99.0.0" || st.Current != Version || st.Error != "" {
		t.Errorf("newer release: %+v", st)
	}
	if s.notified != "99.0.0" {
		t.Error("should remember the notified version")
	}

	// A failed check keeps the last known release.
	status = 500
	if st := s.CheckForUpdate(); !st.Available || st.Error == "" {
		t.Errorf("failed check: %+v", st)
	}

	status, tag = 200, "v"+Version
	if st := s.CheckForUpdate(); st.Available {
		t.Errorf("same version: %+v", st)
	}
	status = 404
	if st := s.CheckForUpdate(); st.Available || st.Error != "" || st.Latest != nil {
		t.Errorf("no releases yet: %+v", st)
	}
}
