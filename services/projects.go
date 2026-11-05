package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/deploy"
	"truedeploy/internal/setup"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

const (
	EventDeployLog      = "deploy:log"
	EventDeployFinished = "deploy:finished"
)

type DeployLog struct {
	DeploymentID string `json:"deploymentId"`
	Stream       string `json:"stream"`
	Line         string `json:"line"`
}

type DeployFinished struct {
	DeploymentID string `json:"deploymentId"`
	ProjectID    string `json:"projectId"`
	Status       string `json:"status"` // "success" | "failed" | "cancelled"
	Commit       string `json:"commit,omitempty"`
	Error        string `json:"error,omitempty"`
}

func init() {
	application.RegisterEvent[DeployLog](EventDeployLog)
	application.RegisterEvent[DeployFinished](EventDeployFinished)
}

// ProjectInput is what the project form sends. Empty ID creates a project.
type ProjectInput struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	ServerID      string            `json:"serverId"`
	RepoURL       string            `json:"repoUrl"`
	Branch        string            `json:"branch"`
	Runtime       string            `json:"runtime"`
	ComposeFile   string            `json:"composeFile"`
	HostPort      int               `json:"hostPort"`
	ContainerPort int               `json:"containerPort"`
	Env           map[string]string `json:"env"`
}

type ProjectView struct {
	store.Project
	AppDir       string            `json:"appDir"`
	ServerName   string            `json:"serverName"`
	UsesSSH      bool              `json:"usesSsh"`
	DeployKeyURL string            `json:"deployKeyUrl,omitempty"`
	Last         *store.Deployment `json:"last,omitempty"`
}

type ProjectDetail struct {
	ProjectView
	Env         map[string]string  `json:"env"`
	Deployments []store.Deployment `json:"deployments"`
}

type ContainerStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"` // running, exited, restarting...
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

type ProjectService struct {
	store *store.Store
	mu    sync.Mutex
	// active deploys: deployment ID -> cancel
	active map[string]context.CancelFunc
}

func NewProjectService(st *store.Store) *ProjectService {
	return &ProjectService{store: st, active: map[string]context.CancelFunc{}}
}

func (s *ProjectService) view(p store.Project, servers map[string]store.Server) ProjectView {
	v := ProjectView{
		Project:      p,
		AppDir:       "~/apps/" + slugOf(p),
		UsesSSH:      deploy.UsesSSH(p.RepoURL),
		DeployKeyURL: deploy.DeployKeyURL(p.RepoURL),
	}
	if srv, ok := servers[p.ServerID]; ok {
		v.ServerName = srv.Name
	}
	if deps := s.deployments(p.ID); len(deps) > 0 {
		v.Last = &deps[0]
	}
	return v
}

func (s *ProjectService) serverMap() map[string]store.Server {
	list, _ := s.store.List()
	m := map[string]store.Server{}
	for _, srv := range list {
		m[srv.ID] = srv
	}
	return m
}

// deployments returns history, marking deploys that were cut off when the
// app closed as failed.
func (s *ProjectService) deployments(projectID string) []store.Deployment {
	deps, _ := s.store.ListDeployments(projectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range deps {
		if d.Status == "running" && s.active[d.ID] == nil {
			d.Status, d.Error = "failed", "TrueDeploy was closed while this deploy was running."
			deps[i] = d
			_ = s.store.SaveDeployment(d)
		}
	}
	return deps
}

func (s *ProjectService) ListProjects() ([]ProjectView, error) {
	list, err := s.store.ListProjects()
	if err != nil {
		return nil, err
	}
	servers := s.serverMap()
	out := make([]ProjectView, 0, len(list))
	for _, p := range list {
		out = append(out, s.view(p, servers))
	}
	return out, nil
}

func (s *ProjectService) GetProject(id string) (*ProjectDetail, error) {
	p, env, err := s.store.GetProject(id)
	if err != nil {
		return nil, err
	}
	return &ProjectDetail{
		ProjectView: s.view(p, s.serverMap()),
		Env:         env,
		Deployments: s.deployments(id),
	}, nil
}

// slugOf falls back to the name for projects saved before slugs were stored.
func slugOf(p store.Project) string {
	if p.Slug != "" {
		return p.Slug
	}
	return deploy.Slug(p.Name)
}

func spec(p store.Project, env map[string]string) deploy.Spec {
	return deploy.Spec{
		Slug:          slugOf(p),
		RepoURL:       p.RepoURL,
		Branch:        p.Branch,
		Runtime:       p.Runtime,
		ComposeFile:   p.ComposeFile,
		HostPort:      p.HostPort,
		ContainerPort: p.ContainerPort,
		Env:           env,
	}
}

func (s *ProjectService) SaveProject(in ProjectInput) (*ProjectView, error) {
	p := store.Project{
		ID:            in.ID,
		Name:          strings.TrimSpace(in.Name),
		ServerID:      in.ServerID,
		RepoURL:       strings.TrimSpace(in.RepoURL),
		Branch:        strings.TrimSpace(in.Branch),
		Runtime:       in.Runtime,
		ComposeFile:   strings.TrimSpace(in.ComposeFile),
		HostPort:      in.HostPort,
		ContainerPort: in.ContainerPort,
		CreatedAt:     time.Now(),
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	if p.Runtime == "" {
		p.Runtime = deploy.RuntimeCompose
	}
	if p.Runtime == deploy.RuntimeCompose {
		p.HostPort, p.ContainerPort = 0, 0
	} else {
		p.ComposeFile = ""
	}
	env := map[string]string{}
	for k, v := range in.Env {
		if k = strings.TrimSpace(k); k != "" {
			env[k] = v
		}
	}
	if _, _, err := s.store.Get(p.ServerID); err != nil {
		return nil, errors.New("choose a server for this project")
	}

	existing, _ := s.store.ListProjects()
	for _, e := range existing {
		if e.ID == p.ID {
			p.CreatedAt, p.Slug = e.CreatedAt, slugOf(e)
		}
	}
	if p.Slug == "" {
		p.Slug = deploy.Slug(p.Name)
	}
	if err := spec(p, env).Validate(); err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.ID != p.ID && e.ServerID == p.ServerID && slugOf(e) == p.Slug {
			return nil, fmt.Errorf("project %q already uses the folder ~/apps/%s on this server", e.Name, p.Slug)
		}
	}
	if p.ID == "" {
		p.ID = store.NewID()
	}
	if err := s.store.PutProject(p, env); err != nil {
		return nil, err
	}
	v := s.view(p, s.serverMap())
	return &v, nil
}

// DeleteProject forgets the project locally. Containers on the server are left
// alone; use StopProject first to remove them.
func (s *ProjectService) DeleteProject(id string) error {
	return s.store.DeleteProject(id)
}

func (s *ProjectService) dial(serverID string) (*sshx.Client, error) {
	t, _, err := savedTarget(s.store, serverID)
	if err != nil {
		return nil, err
	}
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return nil, friendly(err)
	}
	return c, nil
}

// run executes a script as the login user and returns its output lines.
func run(ctx context.Context, c *sshx.Client, script string, onLine func(stream, line string)) error {
	return c.Stream(ctx, setup.Wrap(script, setup.Direct), "", onLine)
}

func collect(ctx context.Context, c *sshx.Client, script string) ([]string, error) {
	var lines []string
	err := run(ctx, c, script, func(_, l string) { lines = append(lines, l) })
	if err != nil {
		if msg := lastLine(lines); msg != "" {
			return lines, errors.New(msg)
		}
	}
	return lines, err
}

func lastLine(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// DeployKey returns the server's public deploy key, creating it if needed.
func (s *ProjectService) DeployKey(serverID string) (string, error) {
	c, err := s.dial(serverID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, `install -d -m 700 "$HOME/.ssh"
K="$HOME/.ssh/truedeploy_ed25519"
[ -f "$K" ] || ssh-keygen -q -t ed25519 -N "" -C "truedeploy@$(hostname)" -f "$K" >/dev/null
cat "$K.pub"`)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// CheckRepoAccess verifies the server can read the repo and branch.
func (s *ProjectService) CheckRepoAccess(projectID string) (string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	sp := spec(p, nil)
	lines, err := collect(context.Background(), c, deploy.AccessScript(sp))
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// Deploy starts a deployment in the background and returns its ID.
// Output arrives as deploy:log events, ending with deploy:finished.
func (s *ProjectService) Deploy(projectID string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	sp := spec(p, env)
	if err := sp.Validate(); err != nil {
		return "", err
	}

	s.mu.Lock()
	for id := range s.active {
		if deps, _ := s.store.ListDeployments(projectID); len(deps) > 0 && deps[0].ID == id {
			s.mu.Unlock()
			return "", errors.New("a deploy of this project is already running")
		}
	}
	s.mu.Unlock()

	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}

	d := store.Deployment{ID: store.NewID(), ProjectID: p.ID, StartedAt: time.Now(), Status: "running"}
	if err := s.store.SaveDeployment(d); err != nil {
		c.Close()
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.active[d.ID] = cancel
	s.mu.Unlock()

	go s.runDeploy(ctx, c, d, deploy.Script(sp))
	return d.ID, nil
}

func (s *ProjectService) runDeploy(ctx context.Context, c *sshx.Client, d store.Deployment, script string) {
	emit := application.Get().Event.Emit
	defer func() {
		c.Close()
		s.mu.Lock()
		if cancel := s.active[d.ID]; cancel != nil {
			cancel()
		}
		delete(s.active, d.ID)
		s.mu.Unlock()
	}()

	var log []string
	err := run(ctx, c, script, func(stream, line string) {
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		if k, v, ok := deploy.ParseMarker(line); ok {
			switch k {
			case "commit":
				d.Commit = v
			case "message":
				d.Message = v
			}
			return
		}
		log = append(log, line)
		if len(log) > store.MaxDeploymentLogLines*2 {
			log = log[len(log)-store.MaxDeploymentLogLines:]
		}
		emit(EventDeployLog, DeployLog{DeploymentID: d.ID, Stream: stream, Line: line})
	})

	d.FinishedAt = time.Now()
	d.Log = log
	switch {
	case ctx.Err() != nil:
		d.Status, d.Error = "cancelled", "Cancelled."
	case err != nil:
		d.Status = "failed"
		var exit interface{ ExitStatus() int }
		if errors.As(err, &exit) && lastLine(log) != "" {
			d.Error = lastLine(log)
		} else {
			d.Error = err.Error()
		}
	default:
		d.Status = "success"
	}
	_ = s.store.SaveDeployment(d)
	emit(EventDeployFinished, DeployFinished{DeploymentID: d.ID, ProjectID: d.ProjectID, Status: d.Status, Commit: d.Commit, Error: d.Error})
}

func (s *ProjectService) CancelDeploy(deploymentID string) {
	s.mu.Lock()
	cancel := s.active[deploymentID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Status lists the project's containers on the server.
func (s *ProjectService) Status(projectID string) ([]ContainerStatus, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, deploy.StatusScript(slugOf(p)))
	if err != nil {
		return nil, err
	}
	out := []ContainerStatus{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) < 4 {
			continue
		}
		out = append(out, ContainerStatus{Name: f[0], State: f[1], Status: f[2], Ports: f[3]})
	}
	return out, nil
}

// StopProject stops and removes the project's containers (volumes are kept).
func (s *ProjectService) StopProject(projectID string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, deploy.StopScript(spec(p, env)))
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}
