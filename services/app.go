package services

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"truedeploy/internal/store"
	"truedeploy/internal/updates"
)

// Version is the app version shown in the UI. Keep it in sync with
// build/config.yml and build/windows/info.json (a test checks this); the
// release workflow refuses tags that don't match it.
const Version = "0.1.0"

const EventUpdateStatus = "update:status"

type AppInfo struct {
	Version  string `json:"version"`
	Platform string `json:"platform"` // e.g. "windows/amd64"
	DataDir  string `json:"dataDir"`  // where servers, projects and encrypted secrets live
}

// UpdateStatus is the result of the last update check.
type UpdateStatus struct {
	Current   string           `json:"current"`
	Available bool             `json:"available"`
	Latest    *updates.Release `json:"latest,omitempty"`
	CheckedAt time.Time        `json:"checkedAt,omitempty"`
	Checking  bool             `json:"checking"`
	Error     string           `json:"error,omitempty"`
	// CanInstall: this copy can update itself (else InstallNote says why and
	// the person downloads it). Phase: "", "downloading", "ready" or
	// "installing"; Progress is 0..1 while downloading.
	CanInstall  bool    `json:"canInstall"`
	InstallNote string  `json:"installNote,omitempty"`
	Phase       string  `json:"phase,omitempty"`
	Progress    float64 `json:"progress,omitempty"`
}

func init() {
	application.RegisterEvent[UpdateStatus](EventUpdateStatus)
}

type AppService struct {
	store    *store.Store
	notifier *notifications.NotificationService
	client   *http.Client

	mu       sync.Mutex
	status   UpdateStatus
	notified string // version we already showed a notification for

	// Self-update: the plan for the latest release, and the verified download.
	plan       updates.Plan
	downloaded string
	applied    bool
	preparing  bool
}

func NewAppService(st *store.Store, notifier *notifications.NotificationService) *AppService {
	return &AppService{
		store: st, notifier: notifier, client: &http.Client{Timeout: 20 * time.Second},
		status: UpdateStatus{Current: Version},
	}
}

func (s *AppService) Info() AppInfo {
	return AppInfo{Version: Version, Platform: runtime.GOOS + "/" + runtime.GOARCH, DataDir: s.store.Dir()}
}

// OpenDataFolder shows the data folder in Explorer / Finder / the file manager.
func (s *AppService) OpenDataFolder() error {
	return application.Get().Browser.OpenFile(s.store.Dir())
}

// ServiceStartup checks for updates shortly after launch, then daily.
func (s *AppService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	go func() {
		wait := time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = 24 * time.Hour
			if set, _ := s.store.Settings(); !set.SkipUpdateCheck {
				s.check(ctx)
			}
		}
	}()
	return nil
}

func (s *AppService) UpdateStatus() UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// CheckForUpdate checks GitHub now (the "Check now" button).
func (s *AppService) CheckForUpdate() UpdateStatus {
	return s.check(context.Background())
}

func (s *AppService) check(ctx context.Context) UpdateStatus {
	s.mu.Lock()
	s.status.Checking = true
	s.mu.Unlock()
	s.emit()

	rel, err := updates.Latest(ctx, s.client)

	s.mu.Lock()
	st := UpdateStatus{Current: Version, CheckedAt: time.Now()}
	switch {
	case errors.Is(err, updates.ErrNoReleases):
		// Nothing published yet: that's "up to date", not an error.
	case err != nil:
		st.Error = err.Error()
		st.Latest, st.Available = s.status.Latest, s.status.Available // keep what we knew
	default:
		st.Latest = &rel
		st.Available = updates.Newer(rel.Version, Version)
	}
	if st.Available {
		plan, why := updates.PlanFor(*st.Latest, currentTarget())
		st.CanInstall, st.InstallNote = plan.Method != "", why
		// Keep a download already done (or running) for the same version.
		if s.plan.Asset.URL == plan.Asset.URL {
			st.Phase, st.Progress = s.status.Phase, s.status.Progress
		} else {
			s.plan, s.downloaded = plan, ""
		}
	}
	s.status = st
	download := st.Available && st.CanInstall && st.Phase == "" && !s.preparing
	notify := st.Available && s.notified != st.Latest.Version
	if notify {
		s.notified = st.Latest.Version
	}
	s.mu.Unlock()
	s.emit()

	if download {
		// Fetch it in the background, so updating is just a restart.
		go func() { _ = s.prepare(context.Background()) }()
	}
	if notify && s.notifier != nil {
		body := "Open Settings → About to download it. You're on " + Version + "."
		if st.CanInstall {
			body = "It's downloading in the background. Restart TrueDeploy to update, or it installs when you quit."
		}
		_ = s.notifier.SendNotification(notifications.NotificationOptions{
			ID: "td-update-" + st.Latest.Version, Title: "TrueDeploy " + st.Latest.Version + " is available", Body: body,
		})
	}
	return st
}

// currentTarget describes this running copy, for picking how to update it.
func currentTarget() updates.Target {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return updates.Target{GOOS: runtime.GOOS, Exe: exe, AppImage: os.Getenv("APPIMAGE"), LocalAppData: os.Getenv("LOCALAPPDATA")}
}

func updateDir(version string) string {
	return filepath.Join(os.TempDir(), "truedeploy-update", version)
}

// prepare downloads and verifies the update (once).
func (s *AppService) prepare(ctx context.Context) error {
	s.mu.Lock()
	if s.preparing {
		s.mu.Unlock()
		return errors.New("the update is already downloading")
	}
	if s.downloaded != "" {
		s.mu.Unlock()
		return nil
	}
	if !s.status.Available || !s.status.CanInstall || s.status.Latest == nil {
		s.mu.Unlock()
		return errors.New("there's no update to install")
	}
	s.preparing = true
	rel, plan := *s.status.Latest, s.plan
	s.status.Phase, s.status.Progress, s.status.Error = "downloading", 0, ""
	s.mu.Unlock()
	s.emit()

	client := &http.Client{Timeout: 30 * time.Minute}
	lastPct := -1
	path, err := updates.Download(ctx, client, rel, plan, updateDir(rel.Version), func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := int(done * 100 / total)
		if pct == lastPct {
			return
		}
		lastPct = pct
		s.mu.Lock()
		s.status.Progress = float64(done) / float64(total)
		s.mu.Unlock()
		s.emit()
	})

	s.mu.Lock()
	s.preparing = false
	if err != nil {
		s.status.Phase, s.status.Error = "", "Couldn't download the update: "+err.Error()
	} else {
		s.downloaded = path
		s.status.Phase, s.status.Progress = "ready", 1
	}
	s.mu.Unlock()
	s.emit()
	return err
}

// apply starts the helper that installs the downloaded update after the app
// exits (and starts the new version when relaunch is set).
func (s *AppService) apply(relaunch bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied || s.downloaded == "" {
		return nil
	}
	script := updates.Script(s.plan, s.downloaded, os.Getpid(), relaunch)
	if script == "" {
		return errors.New("this copy of TrueDeploy can't update itself")
	}
	if err := updates.Launch(script, filepath.Dir(s.downloaded)); err != nil {
		return err
	}
	s.applied = true
	return nil
}

// InstallUpdate downloads the update if needed, then restarts into it.
func (s *AppService) InstallUpdate() error {
	if err := s.prepare(context.Background()); err != nil {
		return err
	}
	if err := s.apply(true); err != nil {
		return err
	}
	s.mu.Lock()
	s.status.Phase = "installing"
	s.mu.Unlock()
	s.emit()
	go func() {
		time.Sleep(400 * time.Millisecond) // let the UI show "Restarting…"
		if app := application.Get(); app != nil {
			app.Quit()
		}
	}()
	return nil
}

// ServiceShutdown installs a downloaded update as the app quits, so the next
// launch is the new version.
func (s *AppService) ServiceShutdown() error {
	_ = s.apply(false)
	return nil
}

func (s *AppService) emit() {
	if app := application.Get(); app != nil {
		app.Event.Emit(EventUpdateStatus, s.UpdateStatus())
	}
}
