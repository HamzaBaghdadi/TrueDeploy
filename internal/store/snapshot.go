package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// Snapshot is all of TrueDeploy's data in plain form, for export. Secrets
// are included (decrypted), so a snapshot must only leave the app sealed
// with a passphrase (see package backupfile).
type Snapshot struct {
	App        string                     `json:"app"`
	Version    int                        `json:"version"`
	ExportedAt time.Time                  `json:"exportedAt"`
	Files      map[string]json.RawMessage `json:"files"`
}

const snapshotVersion = 1

// dataFiles lists every file, and whether it is encrypted at rest.
var dataFiles = []struct {
	name      string
	encrypted bool
}{
	{"servers.json", false}, {"secrets.json", true},
	{"projects.json", false}, {"envs.json", true}, {"deployments.json", false},
	{"databases.json", false}, {"dbsecrets.json", true},
	{"integrations.json", true}, {"channels.json", true}, {"envgroups.json", true}, {"offsite.json", true}, {"settings.json", false},
}

// keyed files are maps by ID and can be merged entry by entry.
var keyed = map[string]bool{
	"servers.json": true, "secrets.json": true, "projects.json": true, "envs.json": true, "deployments.json": true,
	"databases.json": true, "dbsecrets.json": true, "envgroups.json": true,
}

func (s *Store) readRaw(name string, encrypted bool) (json.RawMessage, error) {
	if encrypted {
		var v json.RawMessage
		if err := s.readEncrypted(name, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (s *Store) writeRaw(name string, encrypted bool, v json.RawMessage) error {
	if encrypted {
		return s.writeEncrypted(name, v)
	}
	return writeJSON(s.path(name), v)
}

// Snapshot reads everything (decrypting secrets).
func (s *Store) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn := Snapshot{App: "TrueDeploy", Version: snapshotVersion, ExportedAt: time.Now().UTC(), Files: map[string]json.RawMessage{}}
	for _, f := range dataFiles {
		v, err := s.readRaw(f.name, f.encrypted)
		if err != nil {
			return sn, fmt.Errorf("reading %s: %w", f.name, err)
		}
		if len(v) > 0 && string(v) != "null" {
			sn.Files[f.name] = v
		}
	}
	return sn, nil
}

// Fingerprint identifies the exportable state (deploy history and settings
// excluded), to tell whether anything changed since the last export.
func (sn Snapshot) Fingerprint() string {
	h := sha256.New()
	names := make([]string, 0, len(sn.Files))
	for n := range sn.Files {
		if n != "deployments.json" && n != "settings.json" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		var v any
		_ = json.Unmarshal(sn.Files[n], &v)
		canon, _ := json.Marshal(v) // map keys sorted: stable across writes
		h.Write([]byte(n))
		h.Write(canon)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SnapshotSummary is what a backup contains, shown before importing.
type SnapshotSummary struct {
	ExportedAt time.Time `json:"exportedAt"`
	Servers    []string  `json:"servers"`
	Projects   []string  `json:"projects"`
	Databases  []string  `json:"databases"`
	Channels   int       `json:"channels"`
	GitHub     string    `json:"github,omitempty"` // connected account
}

func names[T any](raw json.RawMessage, name func(T) string) []string {
	m := map[string]T{}
	_ = json.Unmarshal(raw, &m)
	out := []string{}
	for _, v := range m {
		out = append(out, name(v))
	}
	sort.Strings(out)
	return out
}

func (sn Snapshot) Summary() SnapshotSummary {
	sum := SnapshotSummary{ExportedAt: sn.ExportedAt}
	sum.Servers = names(sn.Files["servers.json"], func(s Server) string { return s.Name })
	sum.Projects = names(sn.Files["projects.json"], func(p Project) string { return p.Name })
	sum.Databases = names(sn.Files["databases.json"], func(d Database) string { return d.Name })
	var ch []json.RawMessage
	_ = json.Unmarshal(sn.Files["channels.json"], &ch)
	sum.Channels = len(ch)
	var in integrations
	if json.Unmarshal(sn.Files["integrations.json"], &in) == nil && in.GitHub != nil {
		sum.GitHub = in.GitHub.Login
	}
	return sum
}

// Restore writes a snapshot. With replace, this computer's data is replaced
// entirely. Otherwise it merges: entries missing here are added, entries
// that exist on both sides keep this computer's version, settings stay.
func (s *Store) Restore(sn Snapshot, replace bool) error {
	if sn.App != "TrueDeploy" || sn.Version < 1 {
		return errors.New("this isn't a TrueDeploy backup")
	}
	if sn.Version > snapshotVersion {
		return fmt.Errorf("this backup was made by a newer TrueDeploy (version %d): update the app first", sn.Version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range dataFiles {
		in, ok := sn.Files[f.name]
		if !ok {
			if replace {
				if f.encrypted {
					if err := s.writeEncrypted(f.name, nil); err != nil {
						return err
					}
				} else {
					_ = os.Remove(s.path(f.name))
				}
			}
			continue
		}
		var merged json.RawMessage
		if replace {
			merged = in
		} else {
			cur, err := s.readRaw(f.name, f.encrypted)
			if err != nil {
				return err
			}
			if merged, err = mergeFile(f.name, cur, in); err != nil {
				return fmt.Errorf("%s: %w", f.name, err)
			}
		}
		if merged == nil {
			continue
		}
		if err := s.writeRaw(f.name, f.encrypted, merged); err != nil {
			return err
		}
	}
	return nil
}

func mergeFile(name string, cur, in json.RawMessage) (json.RawMessage, error) {
	empty := len(cur) == 0 || string(cur) == "null"
	switch {
	case keyed[name]:
		a, b := map[string]json.RawMessage{}, map[string]json.RawMessage{}
		if !empty {
			if err := json.Unmarshal(cur, &a); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(in, &b); err != nil {
			return nil, err
		}
		for k, v := range b {
			if _, ok := a[k]; !ok {
				a[k] = v
			}
		}
		return json.Marshal(a)
	case name == "channels.json":
		var a, b []map[string]any
		if !empty {
			_ = json.Unmarshal(cur, &a)
		}
		if err := json.Unmarshal(in, &b); err != nil {
			return nil, err
		}
		seen := map[any]bool{}
		for _, c := range a {
			seen[c["id"]] = true
		}
		for _, c := range b {
			if !seen[c["id"]] {
				a = append(a, c)
			}
		}
		return json.Marshal(a)
	case name == "integrations.json":
		var a, b integrations
		if !empty {
			_ = json.Unmarshal(cur, &a)
		}
		if err := json.Unmarshal(in, &b); err != nil {
			return nil, err
		}
		if a.GitHub == nil {
			a.GitHub = b.GitHub
		}
		have := map[string]bool{}
		for _, g := range a.Git {
			have[g.Host()] = true
		}
		for _, g := range b.Git {
			if !have[g.Host()] {
				a.Git = append(a.Git, g)
			}
		}
		return json.Marshal(a)
	}
	// settings.json: this computer's preferences stay when merging.
	if empty {
		return in, nil
	}
	return nil, nil
}

// ExportRecord remembers the last export, to remind about stale backups.
type ExportRecord struct {
	At          time.Time `json:"at"`
	Fingerprint string    `json:"fingerprint"`
}

func (s *Store) LastExport() (ExportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r ExportRecord
	return r, readJSON(s.path("export.json"), &r)
}

func (s *Store) SetLastExport(r ExportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("export.json"), r)
}
