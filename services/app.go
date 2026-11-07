package services

import (
	"context"
	"errors"
	"net/http"
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
	s.status = st
	notify := st.Available && s.notified != st.Latest.Version
	if notify {
		s.notified = st.Latest.Version
	}
	s.mu.Unlock()
	s.emit()

	if notify && s.notifier != nil {
		_ = s.notifier.SendNotification(notifications.NotificationOptions{
			ID: "td-update-" + st.Latest.Version, Title: "TrueDeploy " + st.Latest.Version + " is available",
			Body: "Open Settings → About to download it. You're on " + Version + ".",
		})
	}
	return st
}

func (s *AppService) emit() {
	if app := application.Get(); app != nil {
		app.Event.Emit(EventUpdateStatus, s.UpdateStatus())
	}
}
