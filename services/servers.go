// Package services holds the Go structs whose exported methods Wails
// exposes to the frontend as typed TypeScript functions.
package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

const dialTimeout = 10 * time.Second

// ServerInput is what the "Add server" form sends.
type ServerInput struct {
	Name string `json:"name"`
	sshx.Target
}

// ServerInfo is what a successful connection test reports.
type ServerInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Kernel        string `json:"kernel"`
	Arch          string `json:"arch"`
	Uptime        string `json:"uptime"`
	DockerVersion string `json:"dockerVersion"` // empty if Docker is not installed
	IsRoot        bool   `json:"isRoot"`
	HasSudo       bool   `json:"hasSudo"`
	Fingerprint   string `json:"fingerprint"`
	LatencyMs     int64  `json:"latencyMs"`
}

type ServerService struct {
	store *store.Store
}

func NewServerService(st *store.Store) *ServerService {
	return &ServerService{store: st}
}

// TestConnection connects with the given details without saving anything.
func (s *ServerService) TestConnection(t sshx.Target) (*ServerInfo, error) {
	return probe(t)
}

// SaveServer stores the server and pins the host key seen during the test.
func (s *ServerService) SaveServer(in ServerInput) (*store.Server, error) {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.Host
	}
	if in.Port == 0 {
		in.Port = 22
	}
	srv := store.Server{
		ID:                 store.NewID(),
		Name:               strings.TrimSpace(in.Name),
		Host:               strings.TrimSpace(in.Host),
		Port:               in.Port,
		User:               strings.TrimSpace(in.User),
		Auth:               string(in.Auth),
		KeyPath:            in.KeyPath,
		HostKeyFingerprint: in.HostKeyFingerprint,
		CreatedAt:          time.Now(),
	}
	sec := store.Secrets{Password: in.Password, PrivateKey: in.PrivateKey, Passphrase: in.Passphrase}
	if err := s.store.Put(srv, sec); err != nil {
		return nil, err
	}
	return &srv, nil
}

// PickKeyFile opens a native file dialog in ~/.ssh. Returns "" if cancelled.
func (s *ServerService) PickKeyFile() (string, error) {
	dlg := application.Get().Dialog.OpenFile().
		SetTitle("Choose SSH private key").
		CanChooseFiles(true).
		ShowHiddenFiles(true)
	if home, err := os.UserHomeDir(); err == nil {
		if dir := filepath.Join(home, ".ssh"); dirExists(dir) {
			dlg.SetDirectory(dir)
		}
	}
	return dlg.PromptForSingleSelection()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (s *ServerService) ListServers() ([]store.Server, error) {
	return s.store.List()
}

func (s *ServerService) DeleteServer(id string) error {
	return s.store.Delete(id)
}

// TestSavedServer reconnects to a saved server, enforcing its pinned host key.
func (s *ServerService) TestSavedServer(id string) (*ServerInfo, error) {
	t, _, err := savedTarget(s.store, id)
	if err != nil {
		return nil, err
	}
	return probe(t)
}

// savedTarget builds connection details for a saved server, with its pinned host key.
func savedTarget(st *store.Store, id string) (sshx.Target, store.Server, error) {
	srv, sec, err := st.Get(id)
	if err != nil {
		return sshx.Target{}, store.Server{}, err
	}
	return sshx.Target{
		Host:               srv.Host,
		Port:               srv.Port,
		User:               srv.User,
		Auth:               sshx.AuthMethod(srv.Auth),
		Password:           sec.Password,
		PrivateKey:         sec.PrivateKey,
		KeyPath:            srv.KeyPath,
		Passphrase:         sec.Passphrase,
		HostKeyFingerprint: srv.HostKeyFingerprint,
	}, srv, nil
}

// probeScript prints key=value lines so parsing doesn't depend on the distro.
const probeScript = `echo "hostname=$(hostname)"
if [ -r /etc/os-release ]; then . /etc/os-release; echo "os=$PRETTY_NAME"; else echo "os=$(uname -s)"; fi
echo "kernel=$(uname -r)"
echo "arch=$(uname -m)"
echo "uptime=$(uptime -p 2>/dev/null || uptime)"
echo "docker=$(docker --version 2>/dev/null)"
echo "uid=$(id -u)"
if sudo -n true 2>/dev/null; then echo "sudo=yes"; else echo "sudo=no"; fi`

func probe(t sshx.Target) (*ServerInfo, error) {
	start := time.Now()
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return nil, friendly(err)
	}
	defer c.Close()
	latency := time.Since(start).Milliseconds()

	out, err := c.Run(probeScript)
	if err != nil && out == "" {
		return nil, errors.New("connected, but could not run commands: " + err.Error())
	}

	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	return &ServerInfo{
		Hostname:      kv["hostname"],
		OS:            kv["os"],
		Kernel:        kv["kernel"],
		Arch:          kv["arch"],
		Uptime:        kv["uptime"],
		DockerVersion: kv["docker"],
		IsRoot:        kv["uid"] == "0",
		HasSudo:       kv["uid"] == "0" || kv["sudo"] == "yes",
		Fingerprint:   c.Fingerprint,
		LatencyMs:     latency,
	}, nil
}

// friendly turns low-level network/SSH errors into messages a user can act on.
func friendly(err error) error {
	msg := err.Error()
	switch {
	case errors.Is(err, sshx.ErrHostKeyMismatch):
		return err
	case strings.Contains(msg, "unable to authenticate"):
		return errors.New("authentication failed: check the username, password or key")
	case strings.Contains(msg, "i/o timeout"):
		return errors.New("connection timed out: check the host, port and firewall")
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"):
		return errors.New("connection refused: is SSH running on that port?")
	case strings.Contains(msg, "no such host"):
		return errors.New("host not found: check the address")
	}
	return err
}
