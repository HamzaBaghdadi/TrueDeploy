package store

import (
	"fmt"
	"sort"
	"time"
	"truedeploy/internal/jobs"
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
	HostPort      int `json:"hostPort,omitempty"`
	ContainerPort int `json:"containerPort,omitempty"`
	// Folder is the folder to build inside the repository (monorepos); "" is the root.
	Folder string `json:"folder,omitempty"`
	// Preset generates the Dockerfile (and nginx config) for a framework when
	// the repository has none. PresetOptions are its settings.
	Preset        string            `json:"preset,omitempty"`
	PresetOptions map[string]string `json:"presetOptions,omitempty"`
	// Domains are served over HTTPS by the server's proxy, forwarding to
	// localhost:AppPort (compose) or localhost:HostPort (dockerfile).
	Domains []string `json:"domains,omitempty"`
	// Redirects are hosts (www.example.com) that redirect to the first domain.
	Redirects []string `json:"redirects,omitempty"`
	AppPort   int      `json:"appPort,omitempty"`
	// StripPath removes a route's path prefix (example.com/api) before
	// forwarding, so the app sees /users instead of /api/users.
	StripPath bool `json:"stripPath,omitempty"`
	// Health check after each deploy (on unless NoHealthCheck), and rollback
	// to the last healthy commit when it fails.
	NoHealthCheck bool   `json:"noHealthCheck,omitempty"`
	HealthPath    string `json:"healthPath,omitempty"`
	HealthTimeout int    `json:"healthTimeout,omitempty"`
	AutoRollback  bool   `json:"autoRollback,omitempty"`
	// MemoryMB and CPUs limit each of the project's containers (0 = no limit).
	MemoryMB int     `json:"memoryMb,omitempty"`
	CPUs     float64 `json:"cpus,omitempty"`
	// AutoDeploy: GitHub Actions deploys on every push. AutoDeployStale is set
	// when settings the hook depends on change, until it is set up again.
	// Links inject managed databases' URLs into the environment on deploy.
	Links           []DBLink `json:"links,omitempty"`
	AutoDeploy      bool     `json:"autoDeploy,omitempty"`
	AutoDeployStale bool     `json:"autoDeployStale,omitempty"`
	// Backups is the volume backup schedule (nil: never set).
	Backups *BackupSchedule `json:"backups,omitempty"`
	// Jobs are scheduled commands run with cron inside the app's container.
	Jobs []jobs.Job `json:"jobs,omitempty"`
	// PreviewOf is the project this one previews a branch of ("" for normal
	// projects). PreviewDomain (on the main project) is the base domain
	// previews get a subdomain of, e.g. preview.example.com.
	PreviewOf     string `json:"previewOf,omitempty"`
	PreviewDomain string `json:"previewDomain,omitempty"`
	// Access: a password in front of the project's domains (bcrypt hash only),
	// and maintenance mode (a "back soon" page instead of the app).
	AuthUser           string `json:"authUser,omitempty"`
	AuthHash           string `json:"authHash,omitempty"`
	Maintenance        bool   `json:"maintenance,omitempty"`
	MaintenanceMessage string `json:"maintenanceMessage,omitempty"`
	// EnvGroups are shared variable groups, applied before the project's own
	// variables (which win on conflicts).
	EnvGroups []string  `json:"envGroups,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Template is set for one-click apps (the catalog ID). They have no
	// repository: Compose holds their compose file, copied at creation so
	// catalog updates never change a running app behind the user's back.
	Template string `json:"template,omitempty"`
	Compose  string `json:"compose,omitempty"`
}

// IsApp reports whether the project is a one-click app (no Git repository).
func (p Project) IsApp() bool { return p.Compose != "" }

// Deployment is one deploy attempt of a project.
type Deployment struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	// Status is "running", "success", "failed" or "cancelled".
	Status string `json:"status"`
	Commit string `json:"commit,omitempty"` // short SHA
	SHA    string `json:"sha,omitempty"`    // full SHA, needed for rollbacks
	// RollbackOf is the deployment whose commit this one redeployed.
	RollbackOf string `json:"rollbackOf,omitempty"`
	Message    string `json:"message,omitempty"` // commit subject
	// Unhealthy is why the post-deploy health check failed; RolledBackTo is
	// the commit the server went back to automatically.
	Unhealthy    string `json:"unhealthy,omitempty"`
	RolledBackTo string `json:"rolledBackTo,omitempty"`
	Error        string `json:"error,omitempty"`
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
