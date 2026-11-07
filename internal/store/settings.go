package store

// Settings are app preferences (not secret).
type Settings struct {
	MonitorEnabled  bool `json:"monitorEnabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
	Notify          bool `json:"notify"`
	CloseToTray     bool `json:"closeToTray"`
	// SkipUpdateCheck turns off the daily check for a new version (on by default).
	SkipUpdateCheck bool `json:"skipUpdateCheck,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{MonitorEnabled: true, IntervalMinutes: 5, Notify: true, CloseToTray: true}
}

// Settings returns saved preferences, or the defaults on first run.
func (s *Store) Settings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var wrapped struct {
		Saved bool `json:"saved"`
		Settings
	}
	if err := readJSON(s.path("settings.json"), &wrapped); err != nil {
		return DefaultSettings(), err
	}
	if !wrapped.Saved {
		return DefaultSettings(), nil
	}
	return wrapped.Settings, nil
}

func (s *Store) SaveSettings(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("settings.json"), struct {
		Saved bool `json:"saved"`
		Settings
	}{true, v})
}
