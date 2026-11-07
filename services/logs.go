package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"truedeploy/internal/deploy"
	"truedeploy/internal/store"
)

const (
	EventLogsLine = "logs:line"
	EventLogsEnd  = "logs:end"
)

type LogsLine struct {
	StreamID string `json:"streamId"`
	Line     string `json:"line"`
}

type LogsEnd struct {
	StreamID string `json:"streamId"`
	Error    string `json:"error,omitempty"`
}

func init() {
	application.RegisterEvent[LogsLine](EventLogsLine)
	application.RegisterEvent[LogsEnd](EventLogsEnd)
}

// logStreams tracks live `logs -f` sessions so the UI can stop them.
var logStreams = struct {
	sync.Mutex
	m map[string]context.CancelFunc
}{m: map[string]context.CancelFunc{}}

// StreamLogs follows the project's container logs, starting with the last
// `tail` lines. Lines arrive as logs:line events until StopLogs or logs:end.
func (s *ProjectService) StreamLogs(projectID string, tail int, since string) (string, error) {
	if !deploy.LogRanges[since] {
		return "", fmt.Errorf("unknown time range %q", since)
	}
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if tail <= 0 || tail > 5000 {
		tail = 200
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	ctx, cancel := context.WithCancel(context.Background())
	logStreams.Lock()
	logStreams.m[id] = cancel
	logStreams.Unlock()

	go func() {
		emit := application.Get().Event.Emit
		var last string
		err := run(ctx, c, deploy.LogsSinceScript(spec(p, env), tail, since, true), func(_, line string) {
			last = line
			emit(EventLogsLine, LogsLine{StreamID: id, Line: line})
		})
		c.Close()
		logStreams.Lock()
		delete(logStreams.m, id)
		logStreams.Unlock()
		end := LogsEnd{StreamID: id}
		if err != nil && ctx.Err() == nil {
			end.Error = strings.TrimSpace(last)
			if end.Error == "" {
				end.Error = err.Error()
			}
		}
		emit(EventLogsEnd, end)
	}()
	return id, nil
}

func (s *ProjectService) StopLogs(streamID string) {
	logStreams.Lock()
	cancel := logStreams.m[streamID]
	logStreams.Unlock()
	if cancel != nil {
		cancel()
	}
}

// DeploymentEntry is one deployment in the cross-project timeline.
type DeploymentEntry struct {
	store.Deployment
	ProjectName string `json:"projectName"`
	ServerName  string `json:"serverName"`
}

// RecentDeployments lists the latest deployments of all projects, newest first.
// Logs are left out to keep the list light.
func (s *ProjectService) RecentDeployments(limit int) ([]DeploymentEntry, error) {
	projects, err := s.store.ListProjects()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	servers := s.serverMap()
	out := []DeploymentEntry{}
	for _, p := range projects {
		for _, d := range s.deployments(p.ID) {
			d.Log = nil
			out = append(out, DeploymentEntry{Deployment: d, ProjectName: p.Name, ServerName: servers[p.ServerID].Name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// DownloadLogs asks where to save, then writes the project's logs for the
// range (up to 50,000 lines) to that file. It returns the path, or "" if
// the user cancelled.
func (s *ProjectService) DownloadLogs(projectID, since string) (string, error) {
	if !deploy.LogRanges[since] {
		return "", fmt.Errorf("unknown time range %q", since)
	}
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	name := slugOf(p) + "-logs-" + time.Now().Format("2006-01-02-1504") + ".log"
	dst, err := application.Get().Dialog.SaveFile().
		SetFilename(name).
		AddFilter("Log file", "*.log;*.txt").
		SetButtonText("Save logs").
		PromptForSingleSelection()
	if err != nil || dst == "" {
		return "", err
	}
	if filepath.Ext(dst) == "" {
		dst += ".log"
	}
	lines, err := s.runOn(p, deploy.LogsSinceScript(spec(p, env), 50000, since, false), 5*time.Minute)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	return dst, nil
}
