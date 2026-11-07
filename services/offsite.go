package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/databases"
	"truedeploy/internal/offsite"
	"truedeploy/internal/s3"
	"truedeploy/internal/store"
	"truedeploy/internal/volbackup"
)

// OffsiteService manages the storage backups are copied to, and the copies.
type OffsiteService struct {
	store    *store.Store
	projects *ProjectService
}

func NewOffsiteService(st *store.Store, projects *ProjectService) *OffsiteService {
	return &OffsiteService{store: st, projects: projects}
}

// OffsiteView is the storage as the settings page shows it: the secret key
// never leaves the app, only whether one is saved.
type OffsiteView struct {
	Configured bool           `json:"configured"`
	Target     offsite.Target `json:"target"`
	HasSecret  bool           `json:"hasSecret"`
}

func (s *OffsiteService) Offsite() (*OffsiteView, error) {
	t, err := s.store.Offsite()
	if err != nil {
		return nil, err
	}
	if t == nil {
		return &OffsiteView{Target: offsite.Target{Provider: "aws", Prefix: "truedeploy"}}, nil
	}
	v := &OffsiteView{Configured: true, Target: *t, HasSecret: t.SecretKey != ""}
	v.Target.SecretKey = ""
	return v, nil
}

// probeBucket checks the app can write, list and delete in the bucket.
func probeBucket(ctx context.Context, c *s3.Client, t offsite.Target) error {
	key := t.Prefix + "/.truedeploy-check"
	if err := c.Put(ctx, key, []byte("TrueDeploy can write here.\n")); err != nil {
		return fmt.Errorf("couldn't write to the bucket: %w", err)
	}
	if _, err := c.List(ctx, t.Prefix+"/", 1); err != nil {
		return fmt.Errorf("couldn't list the bucket: %w", err)
	}
	if err := c.Delete(ctx, key); err != nil {
		return fmt.Errorf("couldn't delete in the bucket: %w", err)
	}
	return nil
}

// SaveOffsite tests the storage, saves it, and updates servers whose backups
// copy offsite. An empty secret key keeps the saved one.
func (s *OffsiteService) SaveOffsite(t offsite.Target) (string, error) {
	t = t.Clean()
	if t.SecretKey == "" {
		if old, _ := s.store.Offsite(); old != nil {
			t.SecretKey = old.SecretKey
		}
	}
	if err := t.Validate(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := probeBucket(ctx, t.Client(), t); err != nil {
		return "", err
	}
	if err := s.store.PutOffsite(&t); err != nil {
		return "", err
	}
	msg := "Saved. TrueDeploy could write, list and delete in the bucket."
	updated, failed := s.pushToServers(&t)
	if updated > 0 {
		msg += fmt.Sprintf(" Updated %d server%s.", updated, plural(updated))
	}
	if len(failed) > 0 {
		msg += " Couldn't update " + strings.Join(failed, ", ") + "; their next backup won't be copied until you save again."
	}
	return msg, nil
}

// RemoveOffsite forgets the storage and removes its credentials from servers.
// Copies already in the bucket are kept.
func (s *OffsiteService) RemoveOffsite() (string, error) {
	if err := s.store.PutOffsite(nil); err != nil {
		return "", err
	}
	_, failed := s.pushToServers(nil)
	msg := "Offsite storage removed. Copies already in the bucket were kept."
	if len(failed) > 0 {
		msg += " Couldn't reach " + strings.Join(failed, ", ") + " to remove the credentials there."
	}
	return msg, nil
}

// offsiteServers lists servers with at least one backup schedule that copies offsite.
func (s *OffsiteService) offsiteServers() []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string, b *store.BackupSchedule) {
		if b != nil && b.Enabled && b.Offsite && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if dbs, err := s.store.ListDatabases(); err == nil {
		for _, d := range dbs {
			add(d.ServerID, d.Backups)
		}
	}
	if ps, err := s.store.ListProjects(); err == nil {
		for _, p := range ps {
			add(p.ServerID, p.Backups)
		}
	}
	return out
}

// pushToServers installs t (or removes the settings when nil) on those servers.
func (s *OffsiteService) pushToServers(t *offsite.Target) (updated int, failed []string) {
	for _, id := range s.offsiteServers() {
		name := id
		if srv, _, err := s.store.Get(id); err == nil {
			name = srv.Name
		}
		script := offsite.RemoveScript
		if t != nil {
			var err error
			if script, err = t.InstallScript(id); err != nil {
				failed = append(failed, name)
				continue
			}
		}
		if _, err := s.projects.runOnServer(id, script, 3*time.Minute); err != nil {
			failed = append(failed, name)
			continue
		}
		updated++
	}
	return updated, failed
}

// installOffsite puts the storage settings on a server before a schedule or
// backup that copies offsite. It fails clearly when storage isn't set up.
func installOffsite(st *store.Store, run func(serverID, script string, timeout time.Duration) ([]string, error), serverID string) error {
	t, err := st.Offsite()
	if err != nil {
		return err
	}
	if t == nil {
		return errors.New("set up offsite storage in Settings first")
	}
	script, err := t.InstallScript(serverID)
	if err != nil {
		return err
	}
	_, err = run(serverID, script, 3*time.Minute)
	return err
}

// backupMessage is the "Backup saved" line plus what happened offsite.
func backupMessage(lines []string) string {
	msg := lastLine(lines)
	for _, l := range lines {
		if strings.HasPrefix(l, "Backup saved:") {
			msg = l
		}
	}
	for _, l := range lines {
		if k, v, ok := strings.Cut(strings.TrimPrefix(l, "::td::"), "="); ok && strings.HasPrefix(l, "::td::") && k == "offsite" {
			switch v {
			case "ok":
				msg += " Copied to offsite storage."
			case "failed":
				msg += " The offsite copy failed; check the storage in Settings (the backup on the server is fine)."
			}
		}
	}
	return msg
}

// OffsiteCopy is one backup file in the bucket.
type OffsiteCopy struct {
	Name     string    `json:"name"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// copiesOf resolves what a backups panel is about: the server, the folder in
// the bucket, how names look, and the local backup folder.
func (s *OffsiteService) copiesOf(kind, id string) (serverID, sub, namePrefix string, valid func(string) bool, localDir string, err error) {
	switch kind {
	case "database":
		d, _, e := s.store.GetDatabase(id)
		if e != nil {
			return "", "", "", nil, "", e
		}
		return d.ServerID, offsite.DatabasesSub, d.Slug + "-", func(n string) bool { return databases.ValidBackupName(d.Slug, n) }, databases.BackupsDir, nil
	case "project":
		p, _, e := s.store.GetProject(id)
		if e != nil {
			return "", "", "", nil, "", e
		}
		slug := slugOf(p)
		return p.ServerID, offsite.ProjectSub(slug), "", volbackup.ValidName, volbackup.Dir(slug), nil
	}
	return "", "", "", nil, "", fmt.Errorf("unknown backup kind %q", kind)
}

func (s *OffsiteService) target() (*offsite.Target, error) {
	t, err := s.store.Offsite()
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("offsite storage isn't set up")
	}
	return t, nil
}

// OffsiteCopies lists a database's or project's backups in the bucket, newest first.
func (s *OffsiteService) OffsiteCopies(kind, id string) ([]OffsiteCopy, error) {
	t, err := s.target()
	if err != nil {
		return nil, err
	}
	serverID, sub, namePrefix, valid, _, err := s.copiesOf(kind, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.ServerDir(serverID) + "/" + sub + "/"
	objs, err := t.Client().List(ctx, dir+namePrefix, 500)
	if err != nil {
		return nil, err
	}
	out := []OffsiteCopy{}
	for _, o := range objs {
		name := strings.TrimPrefix(o.Key, dir)
		if strings.Contains(name, "/") || !valid(name) {
			continue
		}
		out = append(out, OffsiteCopy{Name: name, Bytes: o.Size, Modified: o.Modified})
	}
	return out, nil
}

// FetchOffsite copies a backup from the bucket back onto the server, where
// it can be restored like any other backup.
func (s *OffsiteService) FetchOffsite(kind, id, name string) (string, error) {
	if _, err := s.target(); err != nil {
		return "", err
	}
	serverID, sub, _, valid, localDir, err := s.copiesOf(kind, id)
	if err != nil {
		return "", err
	}
	if !valid(name) {
		return "", fmt.Errorf("%q isn't one of these backups", name)
	}
	if err := installOffsite(s.store, s.projects.runOnServer, serverID); err != nil {
		return "", err
	}
	script, err := offsite.FetchScript(sub, name, localDir)
	if err != nil {
		return "", err
	}
	lines, err := s.projects.runOnServer(serverID, script, 2*time.Hour)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// DownloadOffsite saves a copy from the bucket to this computer, straight
// from the storage (it works even when the server is gone). It returns the
// saved path, or "" if the user cancelled.
func (s *OffsiteService) DownloadOffsite(kind, id, name string) (string, error) {
	t, err := s.target()
	if err != nil {
		return "", err
	}
	serverID, sub, _, valid, _, err := s.copiesOf(kind, id)
	if err != nil {
		return "", err
	}
	if !valid(name) {
		return "", fmt.Errorf("%q isn't one of these backups", name)
	}
	dst, err := application.Get().Dialog.SaveFile().
		SetFilename(name).
		SetButtonText("Save backup").
		PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	if filepath.Ext(dst) == "" {
		dst += path.Ext(name)
	}
	return dst, downloadObject(t, t.ServerDir(serverID)+"/"+sub+"/"+name, dst)
}

// downloadObject streams an object to a local file, never leaving a partial file.
func downloadObject(t *offsite.Target, key, dst string) error {
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	_, getErr := t.Client().Get(ctx, key, f)
	if err := errors.Join(getErr, f.Close()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download failed: %w", err)
	}
	os.Remove(dst)
	return os.Rename(tmp, dst)
}
