package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/databases"
	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

type BackupScheduleStatus struct {
	databases.Schedule
	// Installed reports whether the cron job actually exists on the server.
	Installed bool     `json:"installed"`
	LastLog   []string `json:"lastLog"`
}

func defaultSchedule() databases.Schedule {
	return databases.Schedule{Frequency: "daily", Hour: 3, Keep: 7}
}

// BackupSchedule returns the saved schedule and whether cron has it.
func (s *DatabaseService) BackupSchedule(id string) (*BackupScheduleStatus, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	st := &BackupScheduleStatus{Schedule: defaultSchedule(), LastLog: []string{}}
	if d.Backups != nil {
		st.Schedule = databases.Schedule(*d.Backups)
	}
	lines, err := s.runLines(d.ServerID, databases.ScheduleStatusScript(d.Slug), 30*time.Second)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "cron=") {
			st.Installed = true
		} else if v, ok := strings.CutPrefix(l, "log="); ok {
			st.LastLog = append(st.LastLog, v)
		}
	}
	// What actually runs wins over what was saved.
	st.Enabled = st.Installed
	return st, nil
}

// SetBackupSchedule turns scheduled backups on (installing/replacing the
// cron job) or off (removing it). Existing backups are never deleted here.
func (s *DatabaseService) SetBackupSchedule(id string, sc databases.Schedule) (string, error) {
	d, pw, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	if e, _ := databases.FindEngine(d.Engine); !e.Backup {
		return "", errors.New("scheduled backups aren't available for this engine")
	}
	if sc.Enabled && sc.Offsite {
		if err := installOffsite(s.store, s.runLines, d.ServerID); err != nil {
			return "", err
		}
	}
	var script string
	if sc.Enabled {
		if script, err = databases.ScheduleScript(dbSpec(d, pw), sc); err != nil {
			return "", err
		}
	} else {
		script = databases.UnscheduleScript(d.Slug)
	}
	lines, err := s.runLines(d.ServerID, script, time.Minute)
	if err != nil {
		return "", err
	}
	saved := store.BackupSchedule(sc)
	d.Backups = &saved
	if err := s.store.PutDatabase(d, pw); err != nil {
		return "", err
	}
	msg := lastLine(lines)
	if sc.Enabled {
		msg += " " + describeSchedule(sc)
	}
	return msg, nil
}

func describeSchedule(sc databases.Schedule) string {
	when := map[string]string{"6h": "Every 6 hours", "12h": "Every 12 hours"}[sc.Frequency]
	if when == "" {
		when = fmt.Sprintf("Daily at %02d:15 UTC", sc.Hour)
	}
	if sc.Offsite {
		return fmt.Sprintf("%s, keeping the last %d here and offsite.", when, sc.Keep)
	}
	return fmt.Sprintf("%s, keeping the last %d.", when, sc.Keep)
}

// RestoreBackup restores a backup after taking a safety backup of the current data.
func (s *DatabaseService) RestoreBackup(id, name string) (string, error) {
	d, pw, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	script, err := databases.RestoreScript(dbSpec(d, pw), name)
	if err != nil {
		return "", err
	}
	lines, err := s.runLines(d.ServerID, script, 30*time.Minute)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

func (s *DatabaseService) DeleteBackup(id, name string) (string, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	script, err := databases.DeleteBackupScript(d.Slug, name)
	if err != nil {
		return "", err
	}
	lines, err := s.runLines(d.ServerID, script, 30*time.Second)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// DownloadBackup asks where to save, then copies the backup from the server.
// It returns the saved path, or "" if the user cancelled.
func (s *DatabaseService) DownloadBackup(id, name string) (string, error) {
	d, _, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	if !databases.ValidBackupName(d.Slug, name) {
		return "", fmt.Errorf("%q is not a backup of this database", name)
	}
	dst, err := application.Get().Dialog.SaveFile().
		SetFilename(name).
		AddFilter("Compressed backup", "*.gz").
		SetButtonText("Save backup").
		PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	if filepath.Ext(dst) == "" {
		dst += ".gz"
	}
	if err := s.copyFromServer(d.ServerID, databases.BackupPath(name), dst); err != nil {
		return "", err
	}
	return dst, nil
}

// copyFromServer streams a remote file into a local one. A failed copy never
// leaves a partial file behind.
func (s *DatabaseService) copyFromServer(serverID, remote, local string) error {
	c, err := s.dial(serverID)
	if err != nil {
		return err
	}
	defer c.Close()
	return copyFromServer(c, remote, local)
}

// copyFromServer streams a remote file over an open connection.
func copyFromServer(c *sshx.Client, remote, local string) error {
	sess, err := c.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	out, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	sess.Stderr = &stderr

	tmp := local + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	go func() {
		<-ctx.Done()
		sess.Close()
	}()

	if err := sess.Start(`cat "` + remote + `"`); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	_, copyErr := io.Copy(f, out)
	waitErr := sess.Wait()
	closeErr := f.Close()
	if err := errors.Join(copyErr, waitErr, closeErr); err != nil {
		os.Remove(tmp)
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("download failed: %s", msg)
		}
		return fmt.Errorf("download failed: %w", err)
	}
	os.Remove(local)
	return os.Rename(tmp, local)
}
