package services

import (
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"truedeploy/internal/proxy"
	"truedeploy/internal/store"
)

type AccessInput struct {
	ProjectID string `json:"projectId"`
	// Protect asks for User/Password before the site loads. An empty
	// Password keeps the current one.
	Protect  bool   `json:"protect"`
	User     string `json:"user"`
	Password string `json:"password"`
	// Maintenance shows a "back soon" page with Message instead of the app.
	Maintenance bool   `json:"maintenance"`
	Message     string `json:"message"`
}

// SetAccess saves password protection and maintenance mode, then applies
// them through the server's HTTPS proxy.
func (s *ProjectService) SetAccess(in AccessInput) (string, error) {
	p, env, err := s.store.GetProject(in.ProjectID)
	if err != nil {
		return "", err
	}
	if in.Protect {
		user := strings.TrimSpace(in.User)
		if user == "" {
			return "", errors.New("enter a username")
		}
		switch {
		case in.Password != "":
			if utf8.RuneCountInString(in.Password) < 8 {
				return "", errors.New("use a password of at least 8 characters")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
			if err != nil {
				return "", err
			}
			p.AuthHash = string(hash)
		case p.AuthHash == "":
			return "", errors.New("enter a password")
		}
		p.AuthUser = user
	} else {
		p.AuthUser, p.AuthHash = "", ""
	}
	p.Maintenance = in.Maintenance
	p.MaintenanceMessage = strings.TrimSpace(in.Message)

	all, _ := s.store.ListProjects()
	if err := validateSites(s, p, all); err != nil {
		return "", err
	}
	if err := s.store.PutProject(p, env); err != nil {
		return "", err
	}
	if len(p.Domains) == 0 {
		return "Saved. It takes effect once the project has a domain.", nil
	}
	msg, err := s.applyServerDomains(p.ServerID)
	if err != nil {
		return "", errors.New("saved, but the proxy couldn't be updated: " + err.Error() + " Try Apply changes in the Domains panel.")
	}
	var parts []string
	if p.AuthUser != "" {
		parts = append(parts, "the site asks for a password")
	}
	if p.Maintenance {
		parts = append(parts, "visitors see the maintenance page")
	}
	if len(parts) == 0 {
		return "Done: the site is open to everyone again. " + msg, nil
	}
	return "Done: " + strings.Join(parts, " and ") + ". " + msg, nil
}

// validateSites checks the proxy config with p's new settings.
func validateSites(s *ProjectService, p store.Project, all []store.Project) error {
	if len(p.Domains) == 0 {
		return nil
	}
	return proxy.Validate(s.sites(p.ServerID, &p, all))
}
