package services

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/sshx"
	"truedeploy/internal/store"
)

const (
	EventTermData = "term:data"
	EventTermExit = "term:exit"
)

// TermData carries raw terminal output, base64-encoded so multi-byte
// characters split across chunks survive the trip to the UI intact.
type TermData struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

type TermExit struct {
	SessionID string `json:"sessionId"`
	Error     string `json:"error,omitempty"`
}

func init() {
	application.RegisterEvent[TermData](EventTermData)
	application.RegisterEvent[TermExit](EventTermExit)
}

type termSession struct {
	client *sshx.Client
	shell  *sshx.Shell
}

// TerminalService runs interactive SSH shells for the in-app terminal.
type TerminalService struct {
	store    *store.Store
	mu       sync.Mutex
	sessions map[string]*termSession
	// emit is swapped in tests.
	emit func(name string, data any)
}

func NewTerminalService(st *store.Store) *TerminalService {
	return &TerminalService{
		store:    st,
		sessions: map[string]*termSession{},
		emit:     func(name string, data any) { application.Get().Event.Emit(name, data) },
	}
}

// Open starts a login shell on the server and returns its session ID.
// Output arrives as term:data events; term:exit marks the end.
func (s *TerminalService) Open(serverID string, cols, rows int) (string, error) {
	return s.open(serverID, "", cols, rows)
}

var containerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// OpenContainer opens a shell inside a running container (bash if it has
// it, else sh), as an interactive terminal.
func (s *TerminalService) OpenContainer(serverID, container string, cols, rows int) (string, error) {
	if !containerNameRe.MatchString(container) {
		return "", fmt.Errorf("%q isn't a container name", container)
	}
	return s.open(serverID, ContainerShellCommand(container), cols, rows)
}

// ContainerShellCommand runs docker exec on a PTY, finding rootless Docker
// the same way deploys do. The script travels base64-encoded.
func ContainerShellCommand(container string) string {
	script := `if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then
  export PATH="$HOME/bin:$PATH" DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"
fi
exec docker exec -it '` + container + `' sh -c 'command -v bash >/dev/null && exec bash || exec sh'`
	return `bash -c "$(echo ` + base64.StdEncoding.EncodeToString([]byte(script)) + ` | base64 -d)"`
}

func (s *TerminalService) open(serverID, command string, cols, rows int) (string, error) {
	t, _, err := savedTarget(s.store, serverID)
	if err != nil {
		return "", err
	}
	c, err := sshx.Dial(t, dialTimeout)
	if err != nil {
		return "", friendly(err)
	}
	id := store.NewID()
	sh, err := c.OpenCommand(cols, rows, command, func(b []byte) {
		s.emit(EventTermData, TermData{SessionID: id, Data: base64.StdEncoding.EncodeToString(b)})
	})
	if err != nil {
		c.Close()
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = &termSession{client: c, shell: sh}
	s.mu.Unlock()

	go func() {
		<-sh.Done()
		ev := TermExit{SessionID: id}
		if err := sh.Err(); err != nil {
			ev.Error = err.Error()
		}
		s.drop(id)
		s.emit(EventTermExit, ev)
	}()
	return id, nil
}

func (s *TerminalService) get(id string) (*termSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.sessions[id]
	if ts == nil {
		return nil, errors.New("this terminal session has ended")
	}
	return ts, nil
}

func (s *TerminalService) drop(id string) {
	s.mu.Lock()
	ts := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()
	if ts != nil {
		ts.shell.Close()
		ts.client.Close()
	}
}

// Write sends typed or pasted text to the shell.
func (s *TerminalService) Write(id, data string) error {
	ts, err := s.get(id)
	if err != nil {
		return err
	}
	return ts.shell.Write([]byte(data))
}

func (s *TerminalService) Resize(id string, cols, rows int) error {
	ts, err := s.get(id)
	if err != nil {
		return err
	}
	return ts.shell.Resize(cols, rows)
}

// Close ends a session (closing its tab).
func (s *TerminalService) Close(id string) {
	s.drop(id)
}

// ServiceShutdown ends every session when the app quits (Wails calls it).
func (s *TerminalService) ServiceShutdown() error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.drop(id)
	}
	return nil
}
