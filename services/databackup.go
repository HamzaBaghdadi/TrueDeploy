package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/backupfile"
	"truedeploy/internal/store"
)

// ExportStatus tells the Settings page whether a fresh export is due.
type ExportStatus struct {
	LastExport time.Time `json:"lastExport,omitempty"`
	// Changed: servers, projects, secrets or channels changed since then.
	Changed bool `json:"changed"`
	HasData bool `json:"hasData"`
}

func (s *AppService) ExportStatus() (ExportStatus, error) {
	sn, err := s.store.Snapshot()
	if err != nil {
		return ExportStatus{}, err
	}
	rec, _ := s.store.LastExport()
	_, hasServers := sn.Files["servers.json"]
	return ExportStatus{LastExport: rec.At, Changed: rec.Fingerprint != sn.Fingerprint(), HasData: hasServers}, nil
}

// exportBytes seals a snapshot of everything with the passphrase.
func (s *AppService) exportBytes(passphrase string) ([]byte, store.Snapshot, error) {
	if err := backupfile.CheckPassphrase(passphrase); err != nil {
		return nil, store.Snapshot{}, err
	}
	sn, err := s.store.Snapshot()
	if err != nil {
		return nil, sn, err
	}
	sn.App, sn.Version = "TrueDeploy", 1
	plain, err := json.Marshal(sn)
	if err != nil {
		return nil, sn, err
	}
	file, err := backupfile.Seal(plain, passphrase)
	return file, sn, err
}

// ExportData asks where to save, then writes the encrypted backup there.
// It returns the saved path, or "" if the user cancelled.
func (s *AppService) ExportData(passphrase string) (string, error) {
	file, sn, err := s.exportBytes(passphrase)
	if err != nil {
		return "", err
	}
	dst, err := application.Get().Dialog.SaveFile().
		SetFilename("TrueDeploy-"+time.Now().Format("2006-01-02")+".tdbackup").
		AddFilter("TrueDeploy backup", "*.tdbackup").
		SetButtonText("Save backup").
		PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	if filepath.Ext(dst) == "" {
		dst += ".tdbackup"
	}
	if err := os.WriteFile(dst, file, 0o600); err != nil {
		return "", err
	}
	_ = s.store.SetLastExport(store.ExportRecord{At: time.Now(), Fingerprint: sn.Fingerprint()})
	return dst, nil
}

// PickBackup asks for a backup file to import and returns its path.
func (s *AppService) PickBackup() (string, error) {
	return application.Get().Dialog.OpenFile().
		AddFilter("TrueDeploy backup", "*.tdbackup").
		AddFilter("All files", "*.*").
		SetButtonText("Open backup").
		PromptForSingleSelection()
}

func openBackup(path, passphrase string) (store.Snapshot, error) {
	var sn store.Snapshot
	info, err := os.Stat(path)
	if err != nil {
		return sn, err
	}
	if info.Size() > 64<<20 {
		return sn, errors.New("this file is too large to be a TrueDeploy backup")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return sn, err
	}
	plain, err := backupfile.Open(raw, passphrase)
	if err != nil {
		return sn, err
	}
	if err := json.Unmarshal(plain, &sn); err != nil || sn.App != "TrueDeploy" {
		return sn, errors.New("the backup's contents aren't readable")
	}
	return sn, nil
}

// PreviewImport decrypts a backup and lists what it contains (nothing changes).
func (s *AppService) PreviewImport(path, passphrase string) (*store.SnapshotSummary, error) {
	sn, err := openBackup(path, passphrase)
	if err != nil {
		return nil, err
	}
	sum := sn.Summary()
	return &sum, nil
}

// ImportData restores a backup: merge (add what's missing here) or replace
// (this computer's data becomes the backup's). Before replacing, the current
// data is saved next to the store as a safety copy.
func (s *AppService) ImportData(path, passphrase string, replace bool) (string, error) {
	sn, err := openBackup(path, passphrase)
	if err != nil {
		return "", err
	}
	safety := ""
	if replace {
		file, _, err := s.exportBytes(passphrase)
		if err != nil {
			return "", fmt.Errorf("couldn't save a safety copy first: %w", err)
		}
		safety = filepath.Join(s.store.Dir(), "before-import-"+time.Now().Format("20060102-150405")+".tdbackup")
		if err := os.WriteFile(safety, file, 0o600); err != nil {
			return "", err
		}
	}
	if err := s.store.Restore(sn, replace); err != nil {
		return "", err
	}
	sum := sn.Summary()
	msg := fmt.Sprintf("Imported %d server(s), %d project(s) and %d database(s).", len(sum.Servers), len(sum.Projects), len(sum.Databases))
	if replace {
		msg = fmt.Sprintf("Replaced this computer's data with the backup (%d servers, %d projects). Your previous data is saved as %s (same passphrase).",
			len(sum.Servers), len(sum.Projects), filepath.Base(safety))
	} else {
		msg += " Items that already existed here were kept as they were."
	}
	return strings.TrimSpace(msg), nil
}
