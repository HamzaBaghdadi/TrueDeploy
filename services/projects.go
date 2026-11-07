package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/deploy"
	"truedeploy/internal/github"
	"truedeploy/internal/presets"
	"truedeploy/internal/proxy"
	"truedeploy/internal/setup"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
	"truedeploy/internal/templates"
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
	Domains       []string          `json:"domains"`
	Redirects     []string          `json:"redirects"`
	AppPort       int               `json:"appPort"`
	StripPath     bool              `json:"stripPath"`
	Folder        string            `json:"folder"`
	Preset        string            `json:"preset"`
	PresetOptions map[string]string `json:"presetOptions"`
	EnvGroups     []string          `json:"envGroups"`
	NoHealthCheck bool              `json:"noHealthCheck"`
	HealthPath    string            `json:"healthPath"`
	HealthTimeout int               `json:"healthTimeout"`
	AutoRollback  bool              `json:"autoRollback"`
	MemoryMB      int               `json:"memoryMb"`
	CPUs          float64           `json:"cpus"`
	Links         []store.DBLink    `json:"links"`
}

type ProjectView struct {
	store.Project
	AppDir     string `json:"appDir"`
	ProxyPort  int    `json:"proxyPort"` // local port domains forward to
	ServerName string `json:"serverName"`
	UsesSSH    bool   `json:"usesSsh"`
	IsGitHub   bool   `json:"isGitHub"`
	// GitKind is the repository's provider (github, gitlab, bitbucket, gitea) when
	// known; GitConnected means TrueDeploy can reach it (deploy keys, auto-deploy).
	GitKind      string            `json:"gitKind,omitempty"`
	GitConnected bool              `json:"gitConnected"`
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
	// Live usage while running (docker stats): "1.25%", "120MiB / 512MiB", "23.40%".
	CPU     string `json:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty"`
	MemPerc string `json:"memPerc,omitempty"`
}

type ProjectService struct {
	store  *store.Store
	github *GitHubService
	git    *GitService
	mu     sync.Mutex
	// OnDeployFinished is told about every finished deploy (for notifications).
	OnDeployFinished func(store.Project, store.Deployment)
	// active deploys: deployment ID -> cancel
	active map[string]context.CancelFunc
}

func NewProjectService(st *store.Store, gh *GitHubService) *ProjectService {
	return &ProjectService{store: st, github: gh, git: NewGitService(st, gh), active: map[string]context.CancelFunc{}}
}

func (s *ProjectService) view(p store.Project, servers map[string]store.Server) ProjectView {
	p.AuthHash = "" // never sent to the UI
	v := ProjectView{
		Project:      p,
		AppDir:       "~/apps/" + slugOf(p),
		ProxyPort:    proxyPort(p),
		UsesSSH:      deploy.UsesSSH(p.RepoURL),
		DeployKeyURL: deploy.DeployKeyURL(p.RepoURL),
	}
	_, _, v.IsGitHub = github.ParseRepoURL(p.RepoURL)
	if !p.IsApp() {
		v.GitKind = string(s.git.hostKind(p.RepoURL))
		if prov, repo, err := s.git.forRepo(p.RepoURL); err == nil {
			v.GitConnected = true
			v.DeployKeyURL = prov.DeployKeyPage(repo)
		}
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
		Compose:       p.Compose,
		Folder:        p.Folder,
		// With a domain, only the proxy needs to reach the app.
		LocalOnly:     p.Runtime == deploy.RuntimeDockerfile && len(p.Domains) > 0,
		HealthCheck:   !p.NoHealthCheck,
		HealthPort:    proxyPort(p),
		HealthPath:    p.HealthPath,
		HealthTimeout: healthTimeout(p),
		AutoRollback:  p.AutoRollback,
		MemoryMB:      p.MemoryMB,
		CPUs:          p.CPUs,
	}
}

// healthTimeout: apps (databases inside, first-run migrations) get longer.
func healthTimeout(p store.Project) int {
	if p.HealthTimeout > 0 {
		return p.HealthTimeout
	}
	if p.IsApp() {
		return 180
	}
	return 60
}

// withPreset adds the framework preset's generated files to a deploy spec.
func withPreset(p store.Project, sp deploy.Spec) (deploy.Spec, error) {
	if p.Preset == "" || p.Runtime != deploy.RuntimeDockerfile {
		return sp, nil
	}
	pr, ok := presets.Get(p.Preset)
	if !ok {
		return sp, fmt.Errorf("the framework preset %q no longer exists; pick another in the project settings", p.Preset)
	}
	files, err := presets.Render(p.Preset, p.PresetOptions, p.ContainerPort)
	if err != nil {
		return sp, err
	}
	sp.Files, sp.PresetName = files, pr.Name
	return sp, nil
}

func (s *ProjectService) SaveProject(in ProjectInput) (*ProjectView, error) {
	return s.save(in, "", "")
}

// save stores a project. template and compose are set only when creating a
// one-click app; edits keep the ones the project already has.
func (s *ProjectService) save(in ProjectInput, template, compose string) (*ProjectView, error) {
	if in.ID != "" {
		if prev, _, err := s.store.GetProject(in.ID); err == nil {
			template, compose = prev.Template, prev.Compose
		}
	}
	p := store.Project{
		Template:      template,
		Compose:       compose,
		ID:            in.ID,
		Name:          strings.TrimSpace(in.Name),
		ServerID:      in.ServerID,
		RepoURL:       strings.TrimSpace(in.RepoURL),
		Branch:        strings.TrimSpace(in.Branch),
		Runtime:       in.Runtime,
		ComposeFile:   strings.TrimSpace(in.ComposeFile),
		HostPort:      in.HostPort,
		ContainerPort: in.ContainerPort,
		AppPort:       in.AppPort,
		StripPath:     in.StripPath,
		EnvGroups:     in.EnvGroups,
		NoHealthCheck: in.NoHealthCheck,
		HealthPath:    strings.TrimSpace(in.HealthPath),
		HealthTimeout: in.HealthTimeout,
		AutoRollback:  in.AutoRollback,
		MemoryMB:      in.MemoryMB,
		CPUs:          in.CPUs,
		Links:         in.Links,
		CreatedAt:     time.Now(),
	}
	seen := map[string]bool{}
	for _, d := range in.Domains {
		if d = proxy.NormalizeDomain(d); d != "" && !seen[d] {
			seen[d] = true
			p.Domains = append(p.Domains, d)
		}
	}
	// Redirects are whole hosts; they only make sense with a domain to go to.
	for _, r := range in.Redirects {
		if h, _ := proxy.SplitRoute(proxy.NormalizeDomain(r)); h != "" && !seen[h] && len(p.Domains) > 0 {
			seen[h] = true
			p.Redirects = append(p.Redirects, h)
		}
	}
	if p.IsApp() {
		// Apps have no repository and always run with compose; their port is
		// kept even without a domain because the app is published on it.
		p.RepoURL, p.Branch, p.Runtime, p.ComposeFile = "", "", deploy.RuntimeCompose, ""
	}
	if p.Branch == "" && !p.IsApp() {
		p.Branch = "main"
	}
	if p.Runtime == "" {
		p.Runtime = deploy.RuntimeCompose
	}
	if p.Runtime == deploy.RuntimeCompose {
		p.HostPort, p.ContainerPort = 0, 0
		p.Folder, p.Preset, p.PresetOptions = "", "", nil
	} else {
		p.ComposeFile, p.AppPort = "", 0
		p.Folder = strings.Trim(strings.TrimSpace(in.Folder), "/")
		if p.Folder == "." {
			p.Folder = ""
		}
		if in.Preset != "" {
			pr, ok := presets.Get(in.Preset)
			if !ok {
				return nil, fmt.Errorf("unknown framework preset %q", in.Preset)
			}
			p.Preset, p.PresetOptions = pr.ID, in.PresetOptions
			if pr.Kind == presets.Static {
				p.ContainerPort = 80 // nginx inside the container
			} else if p.ContainerPort == 0 {
				p.ContainerPort = pr.Port
			}
			if _, err := presets.Render(pr.ID, p.PresetOptions, p.ContainerPort); err != nil {
				return nil, err
			}
		}
	}
	if len(p.Domains) == 0 && !p.IsApp() {
		p.AppPort = 0
	}
	env := map[string]string{}
	for k, v := range in.Env {
		if k = strings.TrimSpace(k); k != "" {
			env[k] = v
		}
	}
	var groups []string
	for _, id := range p.EnvGroups {
		if slices.Contains(groups, id) {
			continue
		}
		if _, err := s.store.GetEnvGroup(id); err != nil {
			return nil, errors.New("a selected variable group no longer exists; refresh and choose again")
		}
		groups = append(groups, id)
	}
	p.EnvGroups = groups
	srv, _, err := s.store.Get(p.ServerID)
	if err != nil {
		return nil, errors.New("choose a server for this project")
	}
	if p.IsApp() {
		if p.AppPort < 1 || p.AppPort > 65535 {
			return nil, errors.New("the app port must be between 1 and 65535")
		}
		for k, v := range templates.ManagedEnv(p.AppPort, p.Domains, srv.Host) {
			env[k] = v
		}
	}

	existing, _ := s.store.ListProjects()
	jobsStale := false
	for _, e := range existing {
		if e.ID == p.ID {
			p.CreatedAt, p.Slug = e.CreatedAt, slugOf(e)
			// Auto-deploy survives edits, but the server hook and workflow
			// must be refreshed when what they deploy changes.
			p.AutoDeploy, p.AutoDeployStale = e.AutoDeploy, e.AutoDeployStale
			p.Backups = e.Backups
			p.Jobs = e.Jobs
			p.PreviewOf, p.PreviewDomain = e.PreviewOf, e.PreviewDomain
			p.AuthUser, p.AuthHash, p.Maintenance, p.MaintenanceMessage = e.AuthUser, e.AuthHash, e.Maintenance, e.MaintenanceMessage
			// Job scripts depend on how the app runs: rewrite them if that changed.
			jobsStale = len(e.Jobs) > 0 && (e.Runtime != p.Runtime || e.ComposeFile != p.ComposeFile)
			if e.AutoDeploy && hookInputsChanged(e, p) {
				p.AutoDeployStale = true
			}
		}
	}
	if p.Slug == "" {
		p.Slug = deploy.Slug(p.Name)
	}
	if err := spec(p, env).Validate(); err != nil {
		return nil, err
	}
	if err := s.validateLinks(p, env); err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.ID != p.ID && e.ServerID == p.ServerID && slugOf(e) == p.Slug {
			return nil, fmt.Errorf("project %q already uses the folder ~/apps/%s on this server", e.Name, p.Slug)
		}
		if p.IsApp() && e.ID != p.ID && e.ServerID == p.ServerID && proxyPort(e) == p.AppPort {
			return nil, fmt.Errorf("port %d is already used by %s on this server", p.AppPort, e.Name)
		}
	}
	if len(p.Domains) > 0 {
		if proxyPort(p) == 0 {
			return nil, proxy.ErrNoPort
		}
		if err := proxy.Validate(s.sites(p.ServerID, &p, existing)); err != nil {
			return nil, err
		}
	}
	if p.ID == "" {
		p.ID = store.NewID()
	}
	if err := s.store.PutProject(p, env); err != nil {
		return nil, err
	}
	if jobsStale {
		go func() { _, _ = s.applyJobs(p, env, p.Jobs) }() // best effort; saving the job list again retries
	}
	v := s.view(p, s.serverMap())
	return &v, nil
}

// DeleteProject forgets the project locally. Containers on the server are left
// alone; use StopProject first to remove them.
func (s *ProjectService) DeleteProject(id string) error {
	if previews, _ := s.Previews(id); len(previews) > 0 {
		return fmt.Errorf("delete its %d branch preview(s) first: they use this project's settings", len(previews))
	}
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

// DeployKey returns the project's public deploy key on its server, creating
// it if needed. Each project has its own key because GitHub allows a deploy
// key on one repository only.
func (s *ProjectService) DeployKey(projectID string) (string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	return s.deployKey(p)
}

func (s *ProjectService) deployKey(p store.Project) (string, error) {
	if p.IsApp() {
		return "", errNoRepo
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, deploy.KeyScript(slugOf(p)))
	if err != nil {
		return "", err
	}
	key := lastLine(lines)
	if !strings.HasPrefix(key, "ssh-") {
		return "", errors.New("couldn't read the deploy key from the server")
	}
	return key, nil
}

// AddDeployKey puts the project's key on its repository (read-only), on
// whichever connected Git host the repository lives.
func (s *ProjectService) AddDeployKey(projectID string) (string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if p.IsApp() {
		return "", errNoRepo
	}
	prov, repo, err := s.git.forRepo(p.RepoURL)
	if err != nil {
		return "", err
	}
	key, err := s.deployKey(p)
	if err != nil {
		return "", err
	}
	title := "TrueDeploy · " + slugOf(p)
	if srv, _, err := s.store.Get(p.ServerID); err == nil {
		title += " · " + srv.Name
	}
	ctx, cancel := ghCtx()
	defer cancel()
	added, err := prov.AddDeployKey(ctx, repo, title, key)
	if err != nil {
		return "", err
	}
	if !added {
		return "The deploy key is already on " + repo + ".", nil
	}
	return fmt.Sprintf("Added a read-only deploy key to %s on %s.", repo, prov.Kind().Label()), nil
}

// CheckRepoAccess verifies the server can read the repo and branch.
func (s *ProjectService) CheckRepoAccess(projectID string) (string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if p.IsApp() {
		return "", errNoRepo
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
	return s.deploy(projectID, "")
}

// Rollback redeploys the exact commit of an earlier successful deployment.
func (s *ProjectService) Rollback(projectID, deploymentID string) (string, error) {
	deps, err := s.store.ListDeployments(projectID)
	if err != nil {
		return "", err
	}
	for _, d := range deps {
		if d.ID == deploymentID {
			if d.SHA == "" {
				return "", errors.New("this deploy is too old to roll back to (its full commit wasn't recorded)")
			}
			return s.deploy(projectID, d.ID)
		}
	}
	return "", errors.New("deployment not found")
}

func (s *ProjectService) deploy(projectID, rollbackOf string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	sp, err := s.deploySpec(p, env)
	if err != nil {
		return "", err
	}
	if rollbackOf != "" {
		deps, _ := s.store.ListDeployments(projectID)
		for _, d := range deps {
			if d.ID == rollbackOf {
				sp.Commit = d.SHA
			}
		}
	}
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

	d := store.Deployment{ID: store.NewID(), ProjectID: p.ID, StartedAt: time.Now(), Status: "running", RollbackOf: rollbackOf}
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
			case "sha":
				d.SHA = v
			case "message":
				d.Message = v
			case "unhealthy":
				d.Unhealthy = v
			case "rolledback":
				d.RolledBackTo = v
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
	if s.OnDeployFinished != nil && d.Status != "cancelled" {
		if p, _, err := s.store.GetProject(d.ProjectID); err == nil {
			go s.OnDeployFinished(p, d)
		}
	}
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
	return parseStatus(lines), nil
}

// parseStatus reads StatusScript output: container lines, then "stat" lines
// with live usage for the running ones.
func parseStatus(lines []string) []ContainerStatus {
	out := []ContainerStatus{}
	stats := map[string][]string{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if f[0] == "stat" {
			if len(f) >= 5 {
				stats[f[1]] = f[2:5]
			}
			continue
		}
		if len(f) < 4 {
			continue
		}
		out = append(out, ContainerStatus{Name: f[0], State: f[1], Status: f[2], Ports: f[3]})
	}
	for i, c := range out {
		if st, ok := stats[c.Name]; ok {
			out[i].CPU, out[i].Memory, out[i].MemPerc = st[0], st[1], st[2]
		}
	}
	return out
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
