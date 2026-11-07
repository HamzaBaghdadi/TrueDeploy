// Package history keeps what the monitor saw over time (server load and
// site checks) in one small local file, for charts and uptime figures.
// Points older than Keep are dropped; each series is also capped in length.
package history

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	Keep      = 7 * 24 * time.Hour
	maxPoints = 7 * 24 * 60 // one point a minute for a week
)

// ServerPoint is one check of a server. Percentages are 0-100; a server that
// couldn't be reached has Up=false and no readings.
type ServerPoint struct {
	T    int64   `json:"t"` // unix seconds
	Up   bool    `json:"up"`
	CPU  float32 `json:"cpu,omitempty"`
	Mem  float32 `json:"mem,omitempty"`
	Disk float32 `json:"disk,omitempty"`
	Load float32 `json:"load,omitempty"` // 1-minute load average
}

// SitePoint is one HTTPS check of a domain.
type SitePoint struct {
	T      int64 `json:"t"`
	Up     bool  `json:"up"`
	Ms     int32 `json:"ms,omitempty"`
	Status int16 `json:"status,omitempty"`
}

type data struct {
	Servers map[string][]ServerPoint `json:"servers"`
	Sites   map[string][]SitePoint   `json:"sites"` // key: SiteKey(project, route)
}

// Store is the history file. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	d    data
}

// SiteKey identifies a project's route.
func SiteKey(projectID, route string) string { return projectID + "|" + route }

// Open loads the history (a missing or unreadable file starts empty: it's
// only history).
func Open(path string) *Store {
	s := &Store{path: path, d: data{Servers: map[string][]ServerPoint{}, Sites: map[string][]SitePoint{}}}
	if b, err := os.ReadFile(path); err == nil {
		var d data
		if json.Unmarshal(b, &d) == nil {
			if d.Servers != nil {
				s.d.Servers = d.Servers
			}
			if d.Sites != nil {
				s.d.Sites = d.Sites
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		_ = os.Rename(path, path+".unreadable")
	}
	return s
}

func trim[T any](list []T, at func(T) int64, cutoff int64) []T {
	i := sort.Search(len(list), func(i int) bool { return at(list[i]) >= cutoff })
	list = list[i:]
	if len(list) > maxPoints {
		list = list[len(list)-maxPoints:]
	}
	return list
}

// Add records one monitoring round and saves the file. Series of servers
// and sites not in keepServers/keepSites (deleted ones) are dropped.
func (s *Store) Add(now time.Time, servers map[string]ServerPoint, sites map[string]SitePoint, keepServers, keepSites map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-Keep).Unix()
	for id, p := range servers {
		p.T = now.Unix()
		s.d.Servers[id] = append(s.d.Servers[id], p)
	}
	for k, p := range sites {
		p.T = now.Unix()
		s.d.Sites[k] = append(s.d.Sites[k], p)
	}
	for id, list := range s.d.Servers {
		if keepServers != nil && !keepServers[id] {
			delete(s.d.Servers, id)
			continue
		}
		s.d.Servers[id] = trim(list, func(p ServerPoint) int64 { return p.T }, cutoff)
	}
	for k, list := range s.d.Sites {
		if keepSites != nil && !keepSites[k] {
			delete(s.d.Sites, k)
			continue
		}
		s.d.Sites[k] = trim(list, func(p SitePoint) int64 { return p.T }, cutoff)
	}
	return s.save()
}

func (s *Store) save() error {
	b, err := json.Marshal(s.d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ServerSeries returns a server's points since a time, averaged into at
// most n buckets. A bucket counts as down if any check in it was.
func (s *Store) ServerSeries(id string, since time.Time, n int) []ServerPoint {
	s.mu.Lock()
	src := s.d.Servers[id]
	s.mu.Unlock()
	pts := between(src, since.Unix(), func(p ServerPoint) int64 { return p.T })
	return bucket(pts, since.Unix(), n, func(p ServerPoint) int64 { return p.T }, func(b []ServerPoint) ServerPoint {
		out := ServerPoint{T: b[len(b)-1].T, Up: true}
		var up int
		for _, p := range b {
			if !p.Up {
				out.Up = false
				continue
			}
			up++
			out.CPU += p.CPU
			out.Mem += p.Mem
			out.Disk += p.Disk
			out.Load += p.Load
		}
		if up > 0 {
			f := float32(up)
			out.CPU, out.Mem, out.Disk, out.Load = round(out.CPU/f), round(out.Mem/f), round(out.Disk/f), round(out.Load/f)
		}
		return out
	})
}

// SiteStats summarizes a route's checks since a time.
type SiteStats struct {
	Checks int     `json:"checks"`
	Uptime float64 `json:"uptime"` // percent of checks that were up (-1: no checks)
	AvgMs  int     `json:"avgMs"`  // average response time of successful checks
	// Points are averaged into buckets: Up is false when any check in the bucket failed.
	Points []SitePoint `json:"points"`
}

func (s *Store) SiteStats(key string, since time.Time, n int) SiteStats {
	s.mu.Lock()
	src := s.d.Sites[key]
	s.mu.Unlock()
	pts := between(src, since.Unix(), func(p SitePoint) int64 { return p.T })
	st := SiteStats{Checks: len(pts), Uptime: -1, Points: []SitePoint{}}
	if len(pts) == 0 {
		return st
	}
	var up, ms int
	for _, p := range pts {
		if p.Up {
			up++
			ms += int(p.Ms)
		}
	}
	st.Uptime = math.Round(1000*float64(up)/float64(len(pts))) / 10
	if up > 0 {
		st.AvgMs = ms / up
	}
	st.Points = bucket(pts, since.Unix(), n, func(p SitePoint) int64 { return p.T }, func(b []SitePoint) SitePoint {
		out := SitePoint{T: b[len(b)-1].T, Up: true}
		var ok, sum int
		for _, p := range b {
			if !p.Up {
				out.Up = false
				out.Status = p.Status
				continue
			}
			ok++
			sum += int(p.Ms)
		}
		if ok > 0 {
			out.Ms = int32(sum / ok)
		}
		return out
	})
	return st
}

func between[T any](list []T, from int64, at func(T) int64) []T {
	i := sort.Search(len(list), func(i int) bool { return at(list[i]) >= from })
	return append([]T(nil), list[i:]...)
}

// bucket splits [since, last point] into n equal time slices and merges each.
func bucket[T any](pts []T, since int64, n int, at func(T) int64, merge func([]T) T) []T {
	out := []T{}
	if len(pts) == 0 || n <= 0 {
		return out
	}
	if len(pts) <= n {
		return pts
	}
	end := at(pts[len(pts)-1]) + 1
	width := float64(end-since) / float64(n)
	var cur []T
	idx := -1
	for _, p := range pts {
		b := int(float64(at(p)-since) / width)
		if b != idx && len(cur) > 0 {
			out = append(out, merge(cur))
			cur = cur[:0]
		}
		idx = b
		cur = append(cur, p)
	}
	if len(cur) > 0 {
		out = append(out, merge(cur))
	}
	return out
}

func round(f float32) float32 { return float32(math.Round(float64(f)*10) / 10) }
