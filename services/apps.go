package services

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"truedeploy/internal/store"
	"truedeploy/internal/templates"
)

var errNoRepo = errors.New("this app has no Git repository: TrueDeploy manages its compose file")

// AppInput creates a one-click app from the catalog.
type AppInput struct {
	Template string   `json:"template"`
	Name     string   `json:"name"`
	ServerID string   `json:"serverId"`
	Domains  []string `json:"domains"`
	// Port on the server; 0 picks a free one.
	Port int `json:"port"`
	// Vars overrides the template's defaults (empty values are generated).
	Vars map[string]string `json:"vars"`
}

// Templates lists the one-click app catalog.
func (s *ProjectService) Templates() []templates.Template { return templates.All() }

// CreateApp saves a new project from a template. It isn't deployed yet.
func (s *ProjectService) CreateApp(in AppInput) (*ProjectView, error) {
	tp, ok := templates.Get(in.Template)
	if !ok {
		return nil, errors.New("unknown app")
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = tp.Name
	}
	if in.Port == 0 {
		p, err := s.SuggestPort(in.ServerID, tp.ID)
		if err != nil {
			return nil, err
		}
		in.Port = p
	}
	return s.save(ProjectInput{
		Name:     in.Name,
		ServerID: in.ServerID,
		Runtime:  "compose",
		Domains:  in.Domains,
		AppPort:  in.Port,
		Env:      tp.Env(in.Vars),
	}, tp.ID, tp.Compose)
}

// SuggestPort returns the first free port from the template's default on:
// not used by another project on the server, nor (when the server can be
// reached) by anything already listening there.
func (s *ProjectService) SuggestPort(serverID, templateID string) (int, error) {
	tp, ok := templates.Get(templateID)
	if !ok {
		return 0, errors.New("unknown app")
	}
	return s.freePort(serverID, tp.DefaultPort)
}

// freePort finds the first port from start that no project on the server
// uses and (when the server can be reached) nothing listens on.
func (s *ProjectService) freePort(serverID string, start int) (int, error) {
	used := map[int]bool{}
	projects, _ := s.store.ListProjects()
	for _, p := range projects {
		if p.ServerID == serverID {
			used[proxyPort(p)] = true
			used[p.HostPort] = true
		}
	}
	for port := range s.listeningPorts(serverID) {
		used[port] = true
	}
	for port := start; port < start+1000 && port <= 65535; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, errors.New("no free port found near " + strconv.Itoa(start))
}

const listeningScript = `(ss -Hltn 2>/dev/null || netstat -ltn 2>/dev/null) | awk '{print $4}'`

// listeningPorts is best effort: an unreachable server just means fewer checks.
func (s *ProjectService) listeningPorts(serverID string) map[int]bool {
	out := map[int]bool{}
	c, err := s.dial(serverID)
	if err != nil {
		return out
	}
	defer c.Close()
	lines, _ := collect(context.Background(), c, listeningScript)
	return parseListening(lines)
}

// parseListening reads local addresses like 0.0.0.0:80, [::]:443 or *:22.
func parseListening(lines []string) map[int]bool {
	out := map[int]bool{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		i := strings.LastIndexByte(l, ':')
		if i < 0 {
			continue
		}
		if n, err := strconv.Atoi(l[i+1:]); err == nil && n > 0 && n <= 65535 {
			out[n] = true
		}
	}
	return out
}

// appNeedsDBNetwork: some apps (Adminer, Metabase) reach TrueDeploy databases.
func appNeedsDBNetwork(p store.Project) bool {
	tp, ok := templates.Get(p.Template)
	return ok && p.IsApp() && tp.DBNetwork
}
