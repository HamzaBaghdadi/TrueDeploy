package store

import (
	"fmt"
	"sort"
	"time"
)

// EnvGroup is a named set of environment variables shared by projects
// (secrets included, so the whole file is encrypted).
type EnvGroup struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Vars      map[string]string `json:"vars"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

const envGroupsFile = "envgroups.json"

func (s *Store) readEnvGroups() (map[string]EnvGroup, error) {
	m := map[string]EnvGroup{}
	if err := s.readEncrypted(envGroupsFile, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]EnvGroup{}
	}
	return m, nil
}

// ListEnvGroups returns the groups sorted by name.
func (s *Store) ListEnvGroups() ([]EnvGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readEnvGroups()
	if err != nil {
		return nil, err
	}
	out := make([]EnvGroup, 0, len(m))
	for _, g := range m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) GetEnvGroup(id string) (EnvGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readEnvGroups()
	if err != nil {
		return EnvGroup{}, err
	}
	g, ok := m[id]
	if !ok {
		return EnvGroup{}, fmt.Errorf("variable group %s not found", id)
	}
	return g, nil
}

func (s *Store) PutEnvGroup(g EnvGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readEnvGroups()
	if err != nil {
		return err
	}
	m[g.ID] = g
	return s.writeEncrypted(envGroupsFile, m)
}

func (s *Store) DeleteEnvGroup(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readEnvGroups()
	if err != nil {
		return err
	}
	delete(m, id)
	return s.writeEncrypted(envGroupsFile, m)
}
