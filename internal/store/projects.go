package store

import (
	"fmt"
	"sort"
	"time"
)

// Project links a Git repository to a server and says how to run it.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug names the server folder (~/apps/<slug>) and compose project. It is
	// fixed at creation so renaming a project never orphans its containers.
	Slug     string `json:"slug"`
	ServerID string `json:"serverId"`
	RepoURL  string `json:"repoUrl"`
	Branch   string `json:"branch"`
	// Runtime is "compose" (docker compose up) or "dockerfile" (docker build + run).
	Runtime string `json:"runtime"`
	// ComposeFile is optional; empty means compose.yaml / docker-compose.yml auto-detection.
	ComposeFile string `json:"composeFile,omitempty"`
	// HostPort/ContainerPort are used by the dockerfile runtime ("8080:3000").
	HostPort      int       `json:"hostPort,omitempty"`
	ContainerPort int       `json:"containerPort,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Deployment is one deploy attempt of a project.
type Deployment struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Status is "running", "success", "failed" or "cancelled".
	Status  string `json:"status"`
	Commit  string `json:"commit,omitempty"`  // short SHA
	Message string `json:"message,omitempty"` // commit subject
	Error   string `json:"error,omitempty"`
	// Log keeps the last lines of output for the history view.
	Log []string `json:"log,omitempty"`
}

const (
	maxDeploymentsPerProject = 50
	MaxDeploymentLogLines    = 400
)

func (s *Store) readProjects() (map[string]Project, error) {
	m := map[string]Project{}
	return m, readJSON(s.path("projects.json"), &m)
}

func (s *Store) ListProjects() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readProjects()
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// GetProject returns a project and its environment variables.
func (s *Store) GetProject(id string) (Project, map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readProjects()
	if err != nil {
		return Project{}, nil, err
	}
	p, ok := m[id]
	if !ok {
		return Project{}, nil, fmt.Errorf("project %s not found", id)
	}
	envs := map[string]map[string]string{}
	if err := s.readEncrypted("envs.json", &envs); err != nil {
		return Project{}, nil, err
	}
	env := envs[id]
	if env == nil {
		env = map[string]string{}
	}
	return p, env, nil
}

// PutProject saves a project and its environment variables (encrypted).
func (s *Store) PutProject(p Project, env map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readProjects()
	if err != nil {
		return err
	}
	envs := map[string]map[string]string{}
	if err := s.readEncrypted("envs.json", &envs); err != nil {
		return err
	}
	m[p.ID] = p
	envs[p.ID] = env
	if err := s.writeEncrypted("envs.json", envs); err != nil {
		return err
	}
	return writeJSON(s.path("projects.json"), m)
}

func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readProjects()
	if err != nil {
		return err
	}
	envs := map[string]map[string]string{}
	if err := s.readEncrypted("envs.json", &envs); err != nil {
		return err
	}
	deps, err := s.readDeployments()
	if err != nil {
		return err
	}
	delete(m, id)
	delete(envs, id)
	delete(deps, id)
	if err := s.writeEncrypted("envs.json", envs); err != nil {
		return err
	}
	if err := writeJSON(s.path("deployments.json"), deps); err != nil {
		return err
	}
	return writeJSON(s.path("projects.json"), m)
}

// readDeployments returns deployments per project, newest first.
func (s *Store) readDeployments() (map[string][]Deployment, error) {
	m := map[string][]Deployment{}
	return m, readJSON(s.path("deployments.json"), &m)
}

func (s *Store) ListDeployments(projectID string) ([]Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDeployments()
	if err != nil {
		return nil, err
	}
	return m[projectID], nil
}

// SaveDeployment inserts or updates a deployment, keeping the newest first
// and capping history per project.
func (s *Store) SaveDeployment(d Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readDeployments()
	if err != nil {
		return err
	}
	if len(d.Log) > MaxDeploymentLogLines {
		d.Log = d.Log[len(d.Log)-MaxDeploymentLogLines:]
	}
	list := m[d.ProjectID]
	replaced := false
	for i := range list {
		if list[i].ID == d.ID {
			list[i] = d
			replaced = true
			break
		}
	}
	if !replaced {
		list = append([]Deployment{d}, list...)
	}
	if len(list) > maxDeploymentsPerProject {
		list = list[:maxDeploymentsPerProject]
	}
	m[d.ProjectID] = list
	return writeJSON(s.path("deployments.json"), m)
}
