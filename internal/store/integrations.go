package store

import "truedeploy/internal/gitprov"

// GitHubAccount is the connected GitHub identity. The token is encrypted at
// rest along with the other secrets.
type GitHubAccount struct {
	Token     string `json:"token"`
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

type integrations struct {
	GitHub *GitHubAccount `json:"github,omitempty"`
	// Git holds the other providers (GitLab, Bitbucket, Gitea), one per host.
	Git []gitprov.Account `json:"git,omitempty"`
}

func (s *Store) readIntegrations() (integrations, error) {
	var in integrations
	return in, s.readEncrypted("integrations.json", &in)
}

// GitHub returns the connected account, or nil.
func (s *Store) GitHub() (*GitHubAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := s.readIntegrations()
	return in.GitHub, err
}

// SetGitHub saves the account; nil disconnects.
func (s *Store) SetGitHub(a *GitHubAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := s.readIntegrations()
	if err != nil {
		return err
	}
	in.GitHub = a
	return s.writeEncrypted("integrations.json", in)
}

// GitAccounts returns the connected GitLab, Bitbucket and Gitea accounts.
func (s *Store) GitAccounts() ([]gitprov.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := s.readIntegrations()
	return in.Git, err
}

// PutGitAccount saves an account, replacing the one for the same host.
func (s *Store) PutGitAccount(a gitprov.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := s.readIntegrations()
	if err != nil {
		return err
	}
	kept := in.Git[:0]
	for _, x := range in.Git {
		if x.Host() != a.Host() {
			kept = append(kept, x)
		}
	}
	in.Git = append(kept, a)
	return s.writeEncrypted("integrations.json", in)
}

// DeleteGitAccount forgets the account for a host.
func (s *Store) DeleteGitAccount(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, err := s.readIntegrations()
	if err != nil {
		return err
	}
	kept := in.Git[:0]
	for _, x := range in.Git {
		if x.Host() != host {
			kept = append(kept, x)
		}
	}
	in.Git = kept
	return s.writeEncrypted("integrations.json", in)
}
