package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"truedeploy/internal/databases"
	"truedeploy/internal/deploy"
	"truedeploy/internal/importer"
	"truedeploy/internal/store"
)

// ImportService adopts what already runs on a server set up without TrueDeploy.
type ImportService struct {
	store    *store.Store
	projects *ProjectService
	dbs      *DatabaseService

	mu    sync.Mutex
	scans map[string]importer.Scan // last scan per server (holds DB passwords)
}

func NewImportService(st *store.Store, projects *ProjectService, dbs *DatabaseService) *ImportService {
	return &ImportService{store: st, projects: projects, dbs: dbs, scans: map[string]importer.Scan{}}
}

type ImportCompose struct {
	importer.ComposeProject
	ComposeFile string   `json:"composeFile"`
	Problems    []string `json:"problems"` // blocking
	Warnings    []string `json:"warnings"`
	// ManagedAs is the TrueDeploy project already using this Compose project.
	ManagedAs string `json:"managedAs,omitempty"`
}

type ImportDatabase struct {
	importer.Database
	HasPassword   bool     `json:"hasPassword"`
	TargetVersion string   `json:"targetVersion"`
	Warnings      []string `json:"warnings"`
}

type ImportScan struct {
	Docker     bool                 `json:"docker"`
	Compose    []ImportCompose      `json:"compose"`
	Databases  []ImportDatabase     `json:"databases"`
	Containers []importer.Container `json:"containers"`
	Images     map[string]string    `json:"images"`
	Sites      []importer.Site      `json:"sites"`
	Listeners  []importer.Listener  `json:"listeners"`
	Services   []string             `json:"services"`
	// WebPorts names what holds 80/443 when it isn't TrueDeploy's proxy.
	WebPorts string `json:"webPorts,omitempty"`
}

func validProjectName(n string) bool { return n != "" && deploy.Slug(n) == n }

// Scan reads the server (nothing is changed) and lists what can be imported.
func (s *ImportService) Scan(serverID string) (*ImportScan, error) {
	c, err := s.projects.dial(serverID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	lines, err := collect(ctx, c, importer.ScanScript)
	if err != nil {
		return nil, err
	}
	sc := importer.Parse(lines)
	s.mu.Lock()
	s.scans[serverID] = sc
	s.mu.Unlock()

	projects, _ := s.store.ListProjects()
	managed := map[string]string{}
	for _, p := range projects {
		if p.ServerID == serverID {
			managed[slugOf(p)] = p.Name
		}
	}
	out := &ImportScan{Docker: sc.Docker, Compose: []ImportCompose{}, Databases: []ImportDatabase{}, Containers: sc.Containers,
		Images: sc.Images, Sites: sc.Sites, Listeners: sc.Listeners, Services: sc.Services}
	if out.Containers == nil {
		out.Containers = []importer.Container{}
	}
	for _, cp := range sc.Compose {
		ic := ImportCompose{ComposeProject: cp, ComposeFile: cp.ComposeFile(), Problems: []string{}, Warnings: []string{}, ManagedAs: managed[cp.Name]}
		for _, p := range cp.Problems(validProjectName) {
			if strings.Contains(p, "several compose files") {
				ic.Warnings = append(ic.Warnings, p)
			} else {
				ic.Problems = append(ic.Problems, p)
			}
		}
		if cp.Dirty > 0 {
			ic.Warnings = append(ic.Warnings, fmt.Sprintf("%d tracked file(s) were changed on the server and not committed. A deploy resets them to the repository's version: commit them first if you need them.", cp.Dirty))
		}
		out.Compose = append(out.Compose, ic)
	}
	for _, db := range sc.Databases {
		id := ImportDatabase{Database: db, HasPassword: db.HasPassword(), Warnings: []string{}}
		id.TargetVersion, id.Warnings = targetVersion(db)
		out.Databases = append(out.Databases, id)
	}
	var holders []string
	for _, l := range sc.Listeners {
		if l.Process != "" && !strings.Contains(l.Process, "caddy") && !slices.Contains(holders, l.Process) {
			holders = append(holders, l.Process)
		}
	}
	if len(holders) > 0 {
		out.WebPorts = strings.Join(holders, ", ")
	} else if len(sc.Listeners) > 0 && slices.ContainsFunc(sc.Services, func(s string) bool { return s == "nginx" || s == "apache2" || s == "httpd" }) {
		out.WebPorts = "a web server"
	}
	return out, nil
}

// targetVersion picks the TrueDeploy engine version a database moves to.
func targetVersion(db importer.Database) (string, []string) {
	e, ok := databases.FindEngine(db.Engine)
	if !ok || len(e.Versions) == 0 {
		return "", []string{"This engine can't be imported."}
	}
	var warn []string
	if db.MariaDB {
		warn = append(warn, "MariaDB moves to MySQL "+e.Versions[0]+". Most apps work unchanged, but test before switching over.")
	}
	if slices.Contains(e.Versions, db.Version) {
		return db.Version, warn
	}
	if db.Version != "" && !db.MariaDB {
		warn = append(warn, fmt.Sprintf("Version %s isn't offered; it moves to %s %s.", db.Version, e.Name, e.Versions[0]))
	}
	return e.Versions[0], warn
}

func (s *ImportService) scanned(serverID string) (importer.Scan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.scans[serverID]
	if !ok {
		return sc, errors.New("scan the server first")
	}
	return sc, nil
}

type AdoptInput struct {
	ServerID string   `json:"serverId"`
	Name     string   `json:"name"` // the Compose project name
	Branch   string   `json:"branch"`
	Domains  []string `json:"domains"`
	AppPort  int      `json:"appPort"`
}

// AdoptCompose turns a running Compose project into a TrueDeploy project
// without moving anything: ~/apps/<name> links to its folder and the Docker
// project name stays the same, so containers and volumes carry over.
func (s *ImportService) AdoptCompose(in AdoptInput) (*ProjectView, error) {
	sc, err := s.scanned(in.ServerID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(sc.Compose, func(c importer.ComposeProject) bool { return c.Name == in.Name })
	if i < 0 {
		return nil, fmt.Errorf("%s wasn't found in the last scan; scan again", in.Name)
	}
	cp := sc.Compose[i]
	for _, p := range cp.Problems(validProjectName) {
		if !strings.Contains(p, "several compose files") {
			return nil, errors.New(p)
		}
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		branch = cp.Branch
	}
	if _, err := s.projects.runOn(store.Project{ServerID: in.ServerID}, importer.AdoptScript(cp.Name, cp.WorkDir), time.Minute); err != nil {
		return nil, err
	}
	v, err := s.projects.SaveProject(ProjectInput{
		Name: cp.Name, ServerID: in.ServerID, RepoURL: cp.RepoURL, Branch: branch, Runtime: deploy.RuntimeCompose,
		ComposeFile: cp.ComposeFile(), Domains: in.Domains, AppPort: in.AppPort, Env: cp.Env,
	})
	if err != nil {
		return nil, err
	}
	if v.Slug != cp.Name {
		return nil, fmt.Errorf("internal error: project folder %q doesn't match the Compose project %q", v.Slug, cp.Name)
	}
	// It is already running: record that, so monitoring watches it right away.
	now := time.Now()
	_ = s.store.SaveDeployment(store.Deployment{ID: store.NewID(), ProjectID: v.ID, StartedAt: now, FinishedAt: now, Status: "success",
		Message: "Imported: already running from " + cp.WorkDir})
	got, err := s.projects.GetProject(v.ID)
	if err != nil {
		return nil, err
	}
	return &got.ProjectView, nil
}

type ImportDBInput struct {
	ServerID  string `json:"serverId"`
	Container string `json:"container"`
	Name      string `json:"name"` // the new TrueDeploy database's name
	// Password is needed when the container's environment doesn't have it.
	Password string `json:"password"`
}

type ImportDBResult struct {
	Database DatabaseView `json:"database"`
	Message  string       `json:"message"`
}

// ImportDatabase creates a TrueDeploy database and copies the container's
// data into it. The old container is only read.
func (s *ImportService) ImportDatabase(in ImportDBInput) (*ImportDBResult, error) {
	sc, err := s.scanned(in.ServerID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(sc.Databases, func(d importer.Database) bool { return d.Container == in.Container })
	if i < 0 {
		return nil, fmt.Errorf("%s wasn't found in the last scan; scan again", in.Container)
	}
	src := sc.Databases[i]
	if !src.HasPassword() && src.Engine != "mongo" {
		if in.Password == "" {
			return nil, errors.New("enter the database password: it isn't in the container's environment")
		}
		src = src.WithPassword(in.Password)
	} else if in.Password != "" {
		src = src.WithPassword(in.Password)
	}
	version, _ := targetVersion(src)
	view, err := s.dbs.CreateDatabase(DatabaseInput{ServerID: in.ServerID, Name: in.Name, Engine: src.Engine, Version: version})
	if err != nil {
		return nil, err
	}
	d, pw, err := s.store.GetDatabase(view.ID)
	if err != nil {
		return nil, err
	}
	script, err := importer.CopyScript(src, importer.CopyTarget{Container: databases.ContainerName(d.Slug), User: d.User, Database: d.DBName, Password: pw})
	if err != nil {
		return nil, err
	}
	// The new container needs a moment before it accepts connections.
	lines, err := s.dbs.runLines(in.ServerID, "sleep 5\n"+script, time.Hour)
	if err != nil {
		return nil, fmt.Errorf("the database %s was created, but copying the data failed: %w", view.Name, err)
	}
	return &ImportDBResult{Database: *view, Message: lastLine(lines)}, nil
}
