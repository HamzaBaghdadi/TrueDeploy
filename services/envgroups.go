package services

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"truedeploy/internal/store"
)

var groupVarRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type ProjectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EnvGroupView struct {
	store.EnvGroup
	UsedBy []ProjectRef `json:"usedBy"`
}

type EnvGroupInput struct {
	ID   string            `json:"id"` // "" creates a group
	Name string            `json:"name"`
	Vars map[string]string `json:"vars"`
}

func (s *ProjectService) usedBy(groupID string) []ProjectRef {
	all, _ := s.store.ListProjects()
	out := []ProjectRef{}
	for _, p := range all {
		if slices.Contains(p.EnvGroups, groupID) {
			out = append(out, ProjectRef{ID: p.ID, Name: p.Name})
		}
	}
	return out
}

// ListEnvGroups lists the shared variable groups and which projects use them.
func (s *ProjectService) ListEnvGroups() ([]EnvGroupView, error) {
	list, err := s.store.ListEnvGroups()
	if err != nil {
		return nil, err
	}
	out := make([]EnvGroupView, 0, len(list))
	for _, g := range list {
		if g.Vars == nil {
			g.Vars = map[string]string{}
		}
		out = append(out, EnvGroupView{EnvGroup: g, UsedBy: s.usedBy(g.ID)})
	}
	return out, nil
}

// SaveEnvGroup creates or updates a group. Projects pick up the new values
// on their next deploy (see RedeployEnvGroup).
func (s *ProjectService) SaveEnvGroup(in EnvGroupInput) (*EnvGroupView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("give the group a name")
	}
	vars := map[string]string{}
	for k, v := range in.Vars {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !groupVarRe.MatchString(k) {
			return nil, fmt.Errorf("%q isn't a valid variable name (letters, digits and _, not starting with a digit)", k)
		}
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("the value of %s can't contain line breaks", k)
		}
		vars[k] = v
	}
	all, err := s.store.ListEnvGroups()
	if err != nil {
		return nil, err
	}
	for _, g := range all {
		if g.ID != in.ID && strings.EqualFold(g.Name, name) {
			return nil, fmt.Errorf("a group named %q already exists", g.Name)
		}
	}
	g := store.EnvGroup{ID: in.ID, Name: name, Vars: vars, UpdatedAt: time.Now()}
	if g.ID == "" {
		g.ID = store.NewID()
	} else if _, err := s.store.GetEnvGroup(g.ID); err != nil {
		return nil, err
	}
	if err := s.store.PutEnvGroup(g); err != nil {
		return nil, err
	}
	return &EnvGroupView{EnvGroup: g, UsedBy: s.usedBy(g.ID)}, nil
}

// DeleteEnvGroup removes a group no project uses.
func (s *ProjectService) DeleteEnvGroup(id string) error {
	if used := s.usedBy(id); len(used) > 0 {
		names := make([]string, len(used))
		for i, p := range used {
			names[i] = p.Name
		}
		return fmt.Errorf("it's used by %s: remove it from those projects first", strings.Join(names, ", "))
	}
	return s.store.DeleteEnvGroup(id)
}

// RedeployEnvGroup starts a deploy of every project using the group, so new
// values reach the servers.
func (s *ProjectService) RedeployEnvGroup(id string) (string, error) {
	used := s.usedBy(id)
	if len(used) == 0 {
		return "No project uses this group.", nil
	}
	var started []string
	var failed []string
	for _, p := range used {
		if _, err := s.Deploy(p.ID); err != nil {
			failed = append(failed, p.Name+" ("+err.Error()+")")
		} else {
			started = append(started, p.Name)
		}
	}
	msg := ""
	if len(started) > 0 {
		msg = "Deploying " + strings.Join(started, ", ") + "."
	}
	if len(failed) > 0 {
		msg = strings.TrimSpace(msg + " Couldn't start: " + strings.Join(failed, "; ") + ".")
	}
	return msg, nil
}
