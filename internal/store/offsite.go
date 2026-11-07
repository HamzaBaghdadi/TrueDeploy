package store

import (
	"os"

	"truedeploy/internal/offsite"
)

// offsite.json holds the offsite backup storage (credentials included, so
// it's encrypted). Missing file = not set up.
const offsiteFile = "offsite.json"

type offsiteConfig struct {
	Target *offsite.Target `json:"target,omitempty"`
}

// Offsite returns the offsite storage, or nil if it isn't set up.
func (s *Store) Offsite() (*offsite.Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c offsiteConfig
	if err := s.readEncrypted(offsiteFile, &c); err != nil {
		return nil, err
	}
	return c.Target, nil
}

// PutOffsite saves the offsite storage; nil removes it.
func (s *Store) PutOffsite(t *offsite.Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t == nil {
		if err := os.Remove(s.path(offsiteFile)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return s.writeEncrypted(offsiteFile, offsiteConfig{Target: t})
}
