package services

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"truedeploy/internal/databases"
	"truedeploy/internal/deploy"
	"truedeploy/internal/monitor"
	"truedeploy/internal/offsite"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

type DatabaseService struct {
	store *store.Store
}

func NewDatabaseService(st *store.Store) *DatabaseService {
	return &DatabaseService{store: st}
}

type DatabaseInput struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	Version  string `json:"version"`
}

type DatabaseView struct {
	store.Database
	EngineName string   `json:"engineName"`
	ServerName string   `json:"serverName"`
	Host       string   `json:"host"` // hostname on the private network
	Port       int      `json:"port"`
	EnvVar     string   `json:"envVar"` // suggested env var when linking
	CanBackup  bool     `json:"canBackup"`
	LinkedTo   []string `json:"linkedTo"` // project names
}

type DatabaseStatus struct {
	State  string `json:"state"`  // running, exited, missing...
	Health string `json:"health"` // healthy, starting, unhealthy or ""
	Status string `json:"status"`
	Size   string `json:"size"` // data volume size
}

type Backup struct {
	Name    string    `json:"name"`
	Bytes   int64     `json:"bytes"`
	Created time.Time `json:"created"`
}

func (s *DatabaseService) Engines() []databases.Engine { return databases.Engines }

func dbSpec(d store.Database, password string) databases.Spec {
	return databases.Spec{Slug: d.Slug, Engine: d.Engine, Version: d.Version, User: d.User, Database: d.DBName, Password: password}
}

func (s *DatabaseService) view(d store.Database, servers map[string]store.Server, projects []store.Project) DatabaseView {
	e, _ := databases.FindEngine(d.Engine)
	v := DatabaseView{Database: d, EngineName: e.Name, ServerName: servers[d.ServerID].Name, Host: databases.ContainerName(d.Slug),
		Port: e.Port, EnvVar: e.DefaultEnv, CanBackup: e.Backup, LinkedTo: []string{}}
	for _, p := range projects {
		for _, l := range p.Links {
			if l.DatabaseID == d.ID {
				v.LinkedTo = append(v.LinkedTo, p.Name)
			}
		}
	}
	return v
}

func (s *DatabaseService) ListDatabases() ([]DatabaseView, error) {
	dbs, err := s.store.ListDatabases()
	if err != nil {
		return nil, err
	}
	servers := map[string]store.Server{}
	list, _ := s.store.List()
	for _, srv := range list {
		servers[srv.ID] = srv
	}
	projects, _ := s.store.ListProjects()
	out := make([]DatabaseView, 0, len(dbs))
	for _, d := range dbs {
		out = append(out, s.view(d, servers, projects))
	}
	return out, nil
}

func (s *DatabaseService) dial(serverID string) (*sshx.Client, error) {
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

func (s *DatabaseService) runLines(serverID, script string, timeout time.Duration) ([]string, error) {
	c, err := s.dial(serverID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return collect(ctx, c, script)
}

// CreateDatabase starts a new database container and saves it.
func (s *DatabaseService) CreateDatabase(in DatabaseInput) (*DatabaseView, error) {
	if _, _, err := s.store.Get(in.ServerID); err != nil {
		return nil, errors.New("choose a server")
	}
	name := strings.TrimSpace(in.Name)
	d := store.Database{
		ID: store.NewID(), ServerID: in.ServerID, Name: name, Slug: deploy.Slug(name),
		Engine: in.Engine, Version: in.Version, CreatedAt: time.Now(),
	}
	if e, ok := databases.FindEngine(in.Engine); ok && e.HasDatabase {
		ident := strings.ReplaceAll(d.Slug, "-", "_")
		if ident == "" || (ident[0] >= '0' && ident[0] <= '9') {
			ident = "db_" + ident
		}
		d.User, d.DBName = "app", ident
	}
	existing, _ := s.store.ListDatabases()
	for _, e := range existing {
		if e.ServerID == d.ServerID && e.Slug == d.Slug {
			return nil, fmt.Errorf("a database named %q already exists on this server", e.Name)
		}
	}
	password := databases.GeneratePassword()
	sp := dbSpec(d, password)
	if err := sp.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.runLines(d.ServerID, databases.CreateScript(sp), 10*time.Minute); err != nil {
		return nil, err
	}
	if err := s.store.PutDatabase(d, password); err != nil {
		return nil, err
	}
	srvs, _ := s.store.List()
	m := map[string]store.Server{}
	for _, srv := range srvs {
		m[srv.ID] = srv
	}
	v := s.view(d, m, nil)
	return &v, nil
}

type Connection struct {
	URL      string `json:"url"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Database string `json:"database"`
	Password string `json:"password"`
}

// Connection reveals the connection details (including the password).
func (s *DatabaseService) Connection(id string) (*Connection, error) {
	d, pw, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	e, _ := databases.FindEngine(d.Engine)
	return &Connection{URL: databases.URL(dbSpec(d, pw)), Host: databases.ContainerName(d.Slug), Port: e.Port,
		User: d.User, Database: d.DBName, Password: pw}, nil
}

func (s *DatabaseService) Status(id string) (*DatabaseStatus, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	lines, err := s.runLines(d.ServerID, databases.StatusScript(d.Slug), 30*time.Second)
	if err != nil {
		return nil, err
	}
	f := strings.Split(lastLine(lines)+"\t\t\t", "\t")
	return &DatabaseStatus{State: f[0], Health: f[1], Status: f[2], Size: f[3]}, nil
}

// Restart recreates the container from the saved settings. The data volume
// is reused, so nothing is lost; this also brings back a removed container.
func (s *DatabaseService) Restart(id string) (string, error) {
	d, pw, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	lines, err := s.runLines(d.ServerID, databases.CreateScript(dbSpec(d, pw)), 10*time.Minute)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// DeleteDatabase removes the container, and its data only if deleteData.
func (s *DatabaseService) DeleteDatabase(id string, deleteData bool) (string, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	projects, _ := s.store.ListProjects()
	for _, p := range projects {
		for _, l := range p.Links {
			if l.DatabaseID == id {
				return "", fmt.Errorf("%s is linked to %s: unlink it in the project settings first", d.Name, p.Name)
			}
		}
	}
	// Also stop scheduled backups, so cron doesn't keep failing for a database that's gone.
	lines, err := s.runLines(d.ServerID, databases.UnscheduleScript(d.Slug)+"\n"+databases.DeleteScript(d.Slug, deleteData), time.Minute)
	if err != nil {
		return "", err
	}
	if err := s.store.DeleteDatabase(id); err != nil {
		return "", err
	}
	return strings.Join(lines, " "), nil
}

// BackupNow writes a compressed dump on the server.
func (s *DatabaseService) BackupNow(id string) (string, error) {
	d, pw, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	script, err := databases.BackupScript(dbSpec(d, pw))
	if err != nil {
		return "", err
	}
	script += s.offsiteStep(d.ServerID, d.Backups, offsite.DatabasesSub)
	lines, err := s.runLines(d.ServerID, script, 30*time.Minute)
	if err != nil {
		return "", err
	}
	return backupMessage(lines), nil
}

// offsiteStep is the upload to append to a manual backup when the backup
// schedule copies offsite and storage is set up (it installs the settings
// on the server first). Otherwise it's empty.
func (s *DatabaseService) offsiteStep(serverID string, sc *store.BackupSchedule, sub string) string {
	return offsiteStep(s.store, s.runLines, serverID, sc, sub)
}

func offsiteStep(st *store.Store, run func(string, string, time.Duration) ([]string, error), serverID string, sc *store.BackupSchedule, sub string) string {
	if sc == nil || !sc.Offsite {
		return ""
	}
	if t, _ := st.Offsite(); t == nil {
		return ""
	}
	if err := installOffsite(st, run, serverID); err != nil {
		return "\necho \"::td::offsite=failed\"\n"
	}
	return "\n" + offsite.UploadSnippet(sub)
}

func (s *DatabaseService) ListBackups(id string) ([]Backup, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	lines, err := s.runLines(d.ServerID, databases.ListBackupsScript(d.Slug), 30*time.Second)
	if err != nil {
		return nil, err
	}
	out := []Backup{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) < 3 {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		ts, _ := strconv.ParseInt(f[2], 10, 64)
		out = append(out, Backup{Name: f[0], Bytes: n, Created: time.Unix(ts, 0)})
	}
	return out, nil
}

// ContainerLogs shows the database container's recent output.
func (s *DatabaseService) Logs(id string) ([]string, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	script, err := monitor.ContainerLogsScript(databases.ContainerName(d.Slug), 200)
	if err != nil {
		return nil, err
	}
	return s.runLines(d.ServerID, script, 30*time.Second)
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateLinks checks a project's database links: the database exists on
// the same server and each env var name is valid and unique.
func (s *ProjectService) validateLinks(p store.Project, env map[string]string) error {
	seen := map[string]bool{}
	for _, l := range p.Links {
		d, _, err := s.store.GetDatabase(l.DatabaseID)
		if err != nil {
			return errors.New("a linked database no longer exists; remove the link")
		}
		if d.ServerID != p.ServerID {
			return fmt.Errorf("%s runs on a different server than this project", d.Name)
		}
		if !envNameRe.MatchString(l.EnvVar) {
			return fmt.Errorf("%q is not a valid environment variable name", l.EnvVar)
		}
		if seen[l.EnvVar] {
			return fmt.Errorf("%s is used for two linked databases", l.EnvVar)
		}
		if _, ok := env[l.EnvVar]; ok {
			return fmt.Errorf("%s is set both as a variable and by a linked database; remove one", l.EnvVar)
		}
		seen[l.EnvVar] = true
	}
	return nil
}

// deploySpec is spec() plus shared variable groups and linked databases.
// Precedence: group values, then the project's own variables, then the
// URLs of linked databases (and the project joins their private network).
func (s *ProjectService) deploySpec(p store.Project, env map[string]string) (deploy.Spec, error) {
	merged := map[string]string{}
	for _, id := range p.EnvGroups {
		g, err := s.store.GetEnvGroup(id)
		if err != nil {
			return deploy.Spec{}, errors.New("a shared variable group this project uses no longer exists; edit the project to remove it")
		}
		for k, v := range g.Vars {
			merged[k] = v
		}
	}
	for k, v := range env {
		merged[k] = v
	}
	if len(p.Links) == 0 {
		sp := spec(p, merged)
		if appNeedsDBNetwork(p) {
			sp.Network = databases.Network
		}
		return withPreset(p, sp)
	}
	for _, l := range p.Links {
		d, pw, err := s.store.GetDatabase(l.DatabaseID)
		if err != nil {
			return deploy.Spec{}, errors.New("a linked database no longer exists; edit the project to remove the link")
		}
		merged[l.EnvVar] = databases.URL(dbSpec(d, pw))
	}
	sp := spec(p, merged)
	sp.Network = databases.Network
	return withPreset(p, sp)
}
