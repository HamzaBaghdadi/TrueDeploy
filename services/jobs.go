package services

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"truedeploy/internal/deploy"
	"truedeploy/internal/jobs"
	"truedeploy/internal/store"
)

type JobView struct {
	jobs.Job
	Last jobs.Status `json:"last"`
}

type ProjectJobs struct {
	Jobs []JobView `json:"jobs"`
	// Services are the compose services jobs can run in (empty for Dockerfile projects).
	Services []string `json:"services"`
	// StatusError is set when the server couldn't be read (jobs are still listed).
	StatusError string `json:"statusError,omitempty"`
}

func jobTarget(p store.Project) jobs.Target {
	return jobs.Target{Slug: slugOf(p), Dockerfile: p.Runtime == deploy.RuntimeDockerfile, ComposeFile: p.ComposeFile}
}

// servicesScript lists the project's compose services.
func servicesScript(p store.Project) string {
	return fmt.Sprintf(`fail() { echo "$*" >&2; exit 1; }
export PATH="$HOME/bin:$PATH"
if ! docker info >/dev/null 2>&1 && [ -x "$HOME/bin/docker" ]; then export DOCKER_HOST="unix:///run/user/$(id -u)/docker.sock"; fi
cd "$HOME/apps/%s" 2>/dev/null || exit 0
%sdocker compose -f "$CF" config --services 2>/dev/null | sed 's/^/V\t/' || true`, slugOf(p), deploy.ComposeDetect(p.ComposeFile))
}

func jobIDs(list []jobs.Job) []string {
	ids := make([]string, len(list))
	for i, j := range list {
		ids[i] = j.ID
	}
	return ids
}

// ListJobs returns the project's jobs with their last run, and the services
// a new job can run in.
func (s *ProjectService) ListJobs(projectID string) (*ProjectJobs, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	out := &ProjectJobs{Jobs: []JobView{}, Services: []string{}}
	script := jobs.StatusScript(slugOf(p), jobIDs(p.Jobs))
	if p.Runtime != deploy.RuntimeDockerfile {
		script += "\n" + servicesScript(p)
	}
	lines, err := s.runOn(p, script, 45*time.Second)
	if err != nil {
		out.StatusError = err.Error()
	}
	status := jobs.ParseStatus(lines)
	for _, l := range lines {
		if v, ok := strings.CutPrefix(l, "V\t"); ok && strings.TrimSpace(v) != "" {
			out.Services = append(out.Services, strings.TrimSpace(v))
		}
	}
	for _, j := range p.Jobs {
		out.Jobs = append(out.Jobs, JobView{Job: j, Last: status[j.ID]})
	}
	return out, nil
}

// applyJobs installs the list on the server, then saves it.
func (s *ProjectService) applyJobs(p store.Project, env map[string]string, list []jobs.Job) (string, error) {
	script, err := jobs.ApplyScript(jobTarget(p), list)
	if err != nil {
		return "", err
	}
	lines, err := s.runOn(p, script, time.Minute)
	if err != nil {
		return "", err
	}
	p.Jobs = list
	if err := s.store.PutProject(p, env); err != nil {
		return "", err
	}
	return lastLine(lines), nil
}

// SaveJob adds (empty ID) or updates a job and installs it on the server.
func (s *ProjectService) SaveJob(projectID string, j jobs.Job) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if p.Runtime == deploy.RuntimeDockerfile {
		j.Service = ""
	}
	j.Name, j.Command, j.Schedule = strings.TrimSpace(j.Name), strings.TrimSpace(j.Command), strings.Join(strings.Fields(j.Schedule), " ")
	list := slices.Clone(p.Jobs)
	if j.ID == "" {
		j.ID = store.NewID()
		list = append(list, j)
	} else {
		i := slices.IndexFunc(list, func(x jobs.Job) bool { return x.ID == j.ID })
		if i < 0 {
			return "", errors.New("job not found")
		}
		list[i] = j
	}
	if err := j.Validate(p.Runtime == deploy.RuntimeDockerfile); err != nil {
		return "", err
	}
	return s.applyJobs(p, env, list)
}

// DeleteJob removes a job from cron and from the project (its log stays on the server).
func (s *ProjectService) DeleteJob(projectID, jobID string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	list := slices.DeleteFunc(slices.Clone(p.Jobs), func(x jobs.Job) bool { return x.ID == jobID })
	return s.applyJobs(p, env, list)
}

// RunJob runs a job now and returns its output.
func (s *ProjectService) RunJob(projectID, jobID string) ([]string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(p.Jobs, func(x jobs.Job) bool { return x.ID == jobID })
	if i < 0 {
		return nil, errors.New("job not found")
	}
	script, err := jobs.RunScript(slugOf(p), jobID)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(p.Jobs[i].TimeoutMinutes) * time.Minute
	if timeout == 0 {
		timeout = time.Hour
	}
	lines, err := s.runOn(p, script, timeout+time.Minute)
	if len(lines) > 400 {
		lines = lines[len(lines)-400:]
	}
	if err != nil && len(lines) == 0 {
		return nil, err
	}
	return lines, nil // a failing job still returns its output; the last line has the exit code
}

// JobLog returns the last lines of a job's log.
func (s *ProjectService) JobLog(projectID, jobID string) ([]string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	script, err := jobs.LogScript(slugOf(p), jobID, 200)
	if err != nil {
		return nil, err
	}
	return s.runOn(p, script, 30*time.Second)
}
