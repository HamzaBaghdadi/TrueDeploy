package store

import (
	"fmt"
	"sort"
	"time"
)

// Database is a managed database container on a server. Its password is kept
// encrypted in dbsecrets.json.
type Database struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"serverId"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Engine    string    `json:"engine"`
	Version   string    `json:"version"`
	User      string    `json:"user,omitempty"`
	DBName    string    `json:"dbName,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Backup schedule as last saved; the cron job on the server is the source of truth for whether it runs.
	Backups *BackupSchedule `json:"backups,omitempty"`
}

type BackupSchedule struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
	Hour      int    `json:"hour"`
	Keep      int    `json:"keep"`
	Offsite   bool   `json:"offsite,omitempty"`
}

// DBLink injects a database's connection URL into a project's environment.
type DBLink struct {
	DatabaseID string `json:"databaseId"`
	EnvVar     string `json:"envVar"`
}

func (s *Store) readDatabases() (map[string]Database, error) {
	m := map[string]Database{}
	return m, readJSON(s.path("databases.json"), &m)
}

func (s *Store) ListDatabases() ([]Database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDatabases()
	if err != nil {
		return nil, err
	}
	out := make([]Database, 0, len(m))
	for _, d := range m {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// GetDatabase returns the database and its password.
func (s *Store) GetDatabase(id string) (Database, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDatabases()
	if err != nil {
		return Database{}, "", err
	}
	d, ok := m[id]
	if !ok {
		return Database{}, "", fmt.Errorf("database %s not found", id)
	}
	pw := map[string]string{}
	if err := s.readEncrypted("dbsecrets.json", &pw); err != nil {
		return Database{}, "", err
	}
	return d, pw[id], nil
}

func (s *Store) PutDatabase(d Database, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDatabases()
	if err != nil {
		return err
	}
	pw := map[string]string{}
	if err := s.readEncrypted("dbsecrets.json", &pw); err != nil {
		return err
	}
	m[d.ID] = d
	pw[d.ID] = password
	if err := s.writeEncrypted("dbsecrets.json", pw); err != nil {
		return err
	}
	return writeJSON(s.path("databases.json"), m)
}

func (s *Store) DeleteDatabase(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDatabases()
	if err != nil {
		return err
	}
	pw := map[string]string{}
	if err := s.readEncrypted("dbsecrets.json", &pw); err != nil {
		return err
	}
	delete(m, id)
	delete(pw, id)
	if err := s.writeEncrypted("dbsecrets.json", pw); err != nil {
		return err
	}
	return writeJSON(s.path("databases.json"), m)
}
