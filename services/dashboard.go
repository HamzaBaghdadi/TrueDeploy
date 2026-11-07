package services

import (
	"context"
	"time"

	"truedeploy/internal/monitor"
	"truedeploy/internal/sshx"
)

func (s *ServerService) dial(serverID string) (*sshx.Client, error) {
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

// runLines runs a script as the login user and returns its output lines.
func (s *ServerService) runLines(serverID, script string) ([]string, error) {
	c, err := s.dial(serverID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return collect(ctx, c, script)
}

// Metrics samples CPU (over one second), memory, disk, load and uptime.
func (s *ServerService) Metrics(serverID string) (*monitor.Metrics, error) {
	lines, err := s.runLines(serverID, monitor.MetricsScript)
	if err != nil {
		return nil, err
	}
	m := monitor.ParseMetrics(lines)
	return &m, nil
}

// Containers lists every container on the server with live CPU/memory.
func (s *ServerService) Containers(serverID string) ([]monitor.Container, error) {
	lines, err := s.runLines(serverID, monitor.ContainersScript)
	if err != nil {
		return nil, err
	}
	return monitor.ParseContainers(lines), nil
}

// ContainerAction restarts, stops or starts a container.
func (s *ServerService) ContainerAction(serverID, name, action string) (string, error) {
	script, err := monitor.ActionScript(name, action)
	if err != nil {
		return "", err
	}
	lines, err := s.runLines(serverID, script)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

func (s *ServerService) ContainerLogs(serverID, name string) ([]string, error) {
	script, err := monitor.ContainerLogsScript(name, 300)
	if err != nil {
		return nil, err
	}
	lines, err := s.runLines(serverID, script)
	if lines == nil {
		lines = []string{}
	}
	return lines, err
}

func (s *ServerService) DockerDiskUsage(serverID string) ([]monitor.DiskUsage, error) {
	lines, err := s.runLines(serverID, monitor.DiskUsageScript)
	if err != nil {
		return nil, err
	}
	return monitor.ParseDiskUsage(lines), nil
}

// PruneDocker removes unused images and build cache. Volumes are kept.
func (s *ServerService) PruneDocker(serverID string) (string, error) {
	lines, err := s.runLines(serverID, monitor.PruneScript)
	if err != nil {
		return "", err
	}
	return lastLine(lines), nil
}
