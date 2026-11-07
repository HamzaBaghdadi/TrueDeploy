package store

import (
	"fmt"

	"truedeploy/internal/channels"
)

// Alert channels hold webhook URLs and bot tokens, so the whole list is encrypted.
const channelsFile = "channels.json"

func (s *Store) ListChannels() ([]channels.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []channels.Channel
	return list, s.readEncrypted(channelsFile, &list)
}

func (s *Store) GetChannel(id string) (channels.Channel, error) {
	list, err := s.ListChannels()
	if err != nil {
		return channels.Channel{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return channels.Channel{}, fmt.Errorf("channel %s not found", id)
}

// PutChannel inserts or replaces a channel, keeping the list's order.
func (s *Store) PutChannel(c channels.Channel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []channels.Channel
	if err := s.readEncrypted(channelsFile, &list); err != nil {
		return err
	}
	replaced := false
	for i := range list {
		if list[i].ID == c.ID {
			list[i], replaced = c, true
		}
	}
	if !replaced {
		list = append(list, c)
	}
	return s.writeEncrypted(channelsFile, list)
}

func (s *Store) DeleteChannel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []channels.Channel
	if err := s.readEncrypted(channelsFile, &list); err != nil {
		return err
	}
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return s.writeEncrypted(channelsFile, out)
}
