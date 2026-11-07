package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/databases"
	"truedeploy/internal/offsite"
	"truedeploy/internal/store"
	"truedeploy/internal/volbackup"
)

// ProjectBackups is everything the backups panel shows, read in one connection.
type ProjectBackups struct {
	Backups  []Backup             `json:"backups"`
	Schedule BackupScheduleStatus `json:"schedule"`
}

// runOn runs a script on the project's server with a timeout.
func (s *ProjectService) runOn(p store.Project, script string, timeout time.Duration) ([]string, error) {
	return s.runOnServer(p.ServerID, script, timeout)
}

// runOnServer runs a script on a server with a timeout.
func (s *ProjectService) runOnServer(serverID, script string, timeout time.Duration) ([]string, error) {
	c, err := s.dial(serverID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return collect(ctx, c, script)
}

func (s *ProjectService) ProjectBackups(id string) (*ProjectBackups, error) {
	p, _, err := s.store.GetProject(id)
	if err != nil {
		return nil, err
	}
	list, err := volbackup.ListScript(slugOf(p))
	if err != nil {
		return nil, err
	}
	status, _ := volbackup.StatusScript(slugOf(p))
	lines, err := s.runOn(p, list+"\necho '::td::status'\n"+status, 30*time.Second)
	if err != nil {
		return nil, err
	}
	out := &ProjectBackups{Backups: []Backup{}, Schedule: BackupScheduleStatus{Schedule: defaultSchedule(), LastLog: []string{}}}
	if p.Backups != nil {
		out.Schedule.Schedule = databases.Schedule(*p.Backups)
	}
	inStatus := false
	for _, l := range lines {
		switch {
		case l == "::td::status":
			inStatus = true
		case !inStatus:
			f := strings.Split(l, "\t")
			if len(f) < 3 || !volbackup.ValidName(f[0]) {
				continue
			}
			n, _ := strconv.ParseInt(f[1], 10, 64)
			ts, _ := strconv.ParseInt(f[2], 10, 64)
			out.Backups = append(out.Backups, Backup{Name: f[0], Bytes: n, Created: time.Unix(ts, 0)})
		case strings.HasPrefix(l, "cron="):
			out.Schedule.Installed = true
		default:
			if v, ok := strings.CutPrefix(l, "log="); ok {
				out.Schedule.LastLog = append(out.Schedule.LastLog, v)
			}
		}
	}
	out.Schedule.Enabled = out.Schedule.Installed
	return out, nil
}

// BackupProjectNow archives the project's volumes (containers pause briefly).
func (s *ProjectService) BackupProjectNow(id string) (string, error) {
	p, _, err := s.store.GetProject(id)
	if err != nil {
		return "", err
	}
	script, err := volbackup.BackupScript(slugOf(p))
	if err != nil {
		return "", err
	}
	script += offsiteStep(s.store, s.runOnServer, p.ServerID, p.Backups, offsite.ProjectSub(slugOf(p)))
	lines, err := s.runOn(p, script, 2*time.Hour)
	if err != nil {
		return "", err
	}
	return backupMessage(lines), nil
}

func (s *ProjectService) SetProjectBackupSchedule(id string, sc databases.Schedule) (string, error) {
	p, env, err := s.store.GetProject(id)
	if err != nil {
		return "", err
	}
	if sc.Enabled && sc.Offsite {
		if err := installOffsite(s.store, s.runOnServer, p.ServerID); err != nil {
			return "", err
		}
	}
	var script string
	if sc.Enabled {
		script, err = volbackup.ScheduleScript(slugOf(p), sc)
	} else {
		script, err = volbackup.UnscheduleScript(slugOf(p))
	}
	if err != nil {
		return "", err
	}
	lines, err := s.runOn(p, script, time.Minute)
	if err != nil {
		return "", err
	}
	saved := store.BackupSchedule(sc)
	p.Backups = &saved
	if err := s.store.PutProject(p, env); err != nil {
		return "", err
	}
	msg := lastLine(lines)
	if sc.Enabled {
		msg += " " + describeSchedule(sc)
	}
	return msg, nil
}

// RestoreProjectBackup replaces the volumes with a backup, after a safety backup.
func (s *ProjectService) RestoreProjectBackup(id, name string) (string, error) {
	p, _, err := s.store.GetProject(id)
	if err != nil {
		return "", err
	}
	script, err := volbackup.RestoreScript(slugOf(p), name)
	if err != nil {
		return "", err
	}
	lines, err := s.runOn(p, script, 2*time.Hour)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

func (s *ProjectService) DeleteProjectBackup(id, name string) (string, error) {
	p, _, err := s.store.GetProject(id)
	if err != nil {
		return "", err
	}
	script, err := volbackup.DeleteScript(slugOf(p), name)
	if err != nil {
		return "", err
	}
	lines, err := s.runOn(p, script, 30*time.Second)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// DownloadProjectBackup asks where to save, then copies the backup here.
// It returns the saved path, or "" if the user cancelled.
func (s *ProjectService) DownloadProjectBackup(id, name string) (string, error) {
	p, _, err := s.store.GetProject(id)
	if err != nil {
		return "", err
	}
	if !volbackup.ValidName(name) {
		return "", fmt.Errorf("%q is not a backup of this project", name)
	}
	dst, err := application.Get().Dialog.SaveFile().
		SetFilename(slugOf(p)+"-"+name).
		AddFilter("Compressed archive", "*.tar.gz").
		SetButtonText("Save backup").
		PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(dst), ".gz") && filepath.Ext(dst) == "" {
		dst += ".tar.gz"
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	if err := copyFromServer(c, volbackup.Path(slugOf(p), name), dst); err != nil {
		return "", err
	}
	return dst, nil
}
