package services

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"truedeploy/internal/autodeploy"
	"truedeploy/internal/deploy"
	"truedeploy/internal/gitprov"
	"truedeploy/internal/store"
)

// hookInputsChanged reports whether an edit affects what auto-deploy runs.
func hookInputsChanged(old, new store.Project) bool {
	return old.RepoURL != new.RepoURL || old.Branch != new.Branch || old.Runtime != new.Runtime ||
		old.ComposeFile != new.ComposeFile || old.HostPort != new.HostPort ||
		old.ContainerPort != new.ContainerPort || old.ServerID != new.ServerID ||
		old.Folder != new.Folder || old.Preset != new.Preset || !maps.Equal(old.PresetOptions, new.PresetOptions) ||
		old.NoHealthCheck != new.NoHealthCheck || old.HealthPath != new.HealthPath || old.HealthTimeout != new.HealthTimeout ||
		old.AutoRollback != new.AutoRollback || old.AppPort != new.AppPort || old.MemoryMB != new.MemoryMB || old.CPUs != new.CPUs ||
		// The container is published on 127.0.0.1 only once it has a domain.
		(old.Runtime == "dockerfile" && (len(old.Domains) > 0) != (len(new.Domains) > 0))
}

type AutoDeployRuns struct {
	Runs    []autodeploy.Run `json:"runs"`
	LastLog []string         `json:"lastLog"`
}

// gitRepo finds the provider and repository path for a project's auto-deploy.
func (s *ProjectService) gitRepo(p store.Project) (gitprov.Provider, string, error) {
	if p.IsApp() {
		return nil, "", errNoRepo
	}
	prov, repo, err := s.git.forRepo(p.RepoURL)
	if err != nil {
		if isNotConnected(err) {
			return nil, "", fmt.Errorf("auto-deploy needs TrueDeploy connected to the repository's host: %w", err)
		}
		return nil, "", err
	}
	return prov, repo, nil
}

// EnableAutoDeploy sets up deploy-on-push: a locked CI key and hook on the
// server, then secrets and a pipeline on the Git host (GitHub Actions,
// GitLab CI/CD, Bitbucket Pipelines or Gitea Actions). Running it again
// refreshes everything (and rotates the CI key).
func (s *ProjectService) EnableAutoDeploy(projectID string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	prov, repo, err := s.gitRepo(p)
	if err != nil {
		return "", err
	}
	kind := prov.Kind()
	srv, _, err := s.store.Get(p.ServerID)
	if err != nil {
		return "", err
	}
	if len(env) > 0 && !s.hasSuccessfulDeploy(p.ID) {
		return "", fmt.Errorf("deploy once from TrueDeploy first, so the server has this project's .env (auto-deploys keep it and never send secrets to %s)", kind.Label())
	}
	slug := slugOf(p)
	ctx, cancel := ghCtx()
	defer cancel()

	// A hand-written CI file we must not touch is found before anything changes.
	root := autodeploy.RootFile(kind)
	var rootContent string
	var rootRemove bool
	if root != "" {
		cur, exists, err := prov.GetFile(ctx, repo, root, p.Branch)
		if err != nil {
			return "", err
		}
		var note string
		rootContent, rootRemove, note = autodeploy.UpdateRoot(kind, cur, exists, autodeploy.Entry{Slug: slug, Branch: p.Branch, Name: p.Name}, true)
		if note != "" {
			return "", errors.New(note)
		}
	}

	sp, err := s.deploySpec(p, nil)
	if err != nil {
		return "", err
	}
	sp.KeepEnv = true
	if err := sp.Validate(); err != nil {
		return "", err
	}
	kp, err := autodeploy.NewKeyPair()
	if err != nil {
		return "", err
	}

	// 1. Server: install the hook and authorize the CI key for it only.
	c, err := s.dial(p.ServerID)
	if err != nil {
		return "", err
	}
	lines, err := collect(context.Background(), c, autodeploy.InstallScript(slug, autodeploy.HookScript(slug, deploy.Script(sp)), kp.PublicKey))
	c.Close()
	if err != nil {
		return "", err
	}
	var hostKeys []string
	for _, l := range lines {
		if k, v, ok := deploy.ParseMarker(l); ok && k == "hostkey" {
			hostKeys = append(hostKeys, v)
		}
	}
	if len(hostKeys) == 0 {
		s.uninstallHook(p)
		return "", fmt.Errorf("couldn't read the server's SSH host keys, which %s needs to verify the server", kind.CIName())
	}

	// 2. Git host: CI on, secrets, then the pipeline. Undo on failure.
	secrets := autodeploy.ProviderSecrets(kind, slug, srv.User, srv.Host, srv.Port, kp.PrivatePEM, hostKeys)
	undo := func() {
		s.uninstallHook(p)
		_ = prov.DeleteSecrets(ctx, repo, keys(secrets))
	}
	if err := prov.PrepareCI(ctx, repo); err != nil {
		s.uninstallHook(p)
		return "", err
	}
	if err := prov.SetSecrets(ctx, repo, secrets); err != nil {
		undo()
		return "", err
	}
	msg := "Add TrueDeploy auto-deploy for " + p.Name
	changed := false
	if file := autodeploy.ProjectFile(kind, slug); file != "" {
		ch, err := prov.PutFile(ctx, repo, file, p.Branch, autodeploy.ProjectPipeline(kind, slug, p.Name, p.Branch), msg)
		if err != nil {
			undo()
			return "", err
		}
		changed = changed || ch
	}
	if root != "" && rootContent != "" && !rootRemove {
		ch, err := prov.PutFile(ctx, repo, root, p.Branch, rootContent, msg)
		if err != nil {
			undo()
			return "", err
		}
		changed = changed || ch
	}

	p.AutoDeploy, p.AutoDeployStale = true, false
	if err := s.store.PutProject(p, env); err != nil {
		return "", err
	}
	out := fmt.Sprintf("Auto-deploy is on: every push to %s deploys %s.", p.Branch, p.Name)
	if changed {
		out += fmt.Sprintf(" The pipeline was committed to the repo, so a first deploy is starting on %s now.", kind.CIName())
	}
	return out, nil
}

// DisableAutoDeploy removes the CI key and hook from the server, then the
// pipeline and secrets from the Git host. The server key is removed first:
// that is what actually revokes access.
func (s *ProjectService) DisableAutoDeploy(projectID string) (string, error) {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	if err := s.uninstallHook(p); err != nil {
		return "", fmt.Errorf("couldn't remove the CI key from the server, so auto-deploy is still on: %w", err)
	}
	p.AutoDeploy, p.AutoDeployStale = false, false
	if err := s.store.PutProject(p, env); err != nil {
		return "", err
	}

	slug := slugOf(p)
	prov, repo, err := s.gitRepo(p)
	if err != nil {
		return "Auto-deploy is off (the server no longer accepts the CI key). Remove TrueDeploy's pipeline from the repo yourself: " + err.Error(), nil
	}
	kind := prov.Kind()
	ctx, cancel := ghCtx()
	defer cancel()
	msg := "Remove TrueDeploy auto-deploy for " + p.Name
	var problems, notes []string

	keepJob := false // a hand-written .gitlab-ci.yml still includes the job file
	if root := autodeploy.RootFile(kind); root != "" {
		cur, exists, err := prov.GetFile(ctx, repo, root, p.Branch)
		if err != nil {
			problems = append(problems, root+": "+err.Error())
		} else {
			content, remove, note := autodeploy.UpdateRoot(kind, cur, exists, autodeploy.Entry{Slug: slug, Branch: p.Branch, Name: p.Name}, false)
			switch {
			case note != "":
				notes = append(notes, note)
				keepJob = kind == gitprov.GitLab
			case remove:
				if err := prov.DeleteFile(ctx, repo, root, p.Branch, msg); err != nil {
					problems = append(problems, root+": "+err.Error())
				}
			case content != "":
				if _, err := prov.PutFile(ctx, repo, root, p.Branch, content, msg); err != nil {
					problems = append(problems, root+": "+err.Error())
				}
			}
		}
	}
	if file := autodeploy.ProjectFile(kind, slug); file != "" {
		var err error
		if keepJob {
			_, err = prov.PutFile(ctx, repo, file, p.Branch, autodeploy.DisabledGitLabJob(slug), msg)
		} else {
			err = prov.DeleteFile(ctx, repo, file, p.Branch, msg)
		}
		if err != nil {
			problems = append(problems, file+": "+err.Error())
		}
	}
	if err := prov.DeleteSecrets(ctx, repo, autodeploy.SecretNames(kind, slug)); err != nil {
		problems = append(problems, "secrets: "+err.Error())
	}
	if len(problems) > 0 {
		return fmt.Sprintf("Auto-deploy is off on the server, but cleaning up %s failed (%s).", kind.Label(), strings.Join(problems, "; ")), nil
	}
	out := "Auto-deploy is off. The server key, pipeline and secrets were removed."
	if len(notes) > 0 {
		out += " " + strings.Join(notes, " ")
	}
	return out, nil
}

func (s *ProjectService) uninstallHook(p store.Project) error {
	c, err := s.dial(p.ServerID)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = collect(context.Background(), c, autodeploy.UninstallScript(slugOf(p)))
	return err
}

// AutoDeployHistory returns runs recorded by the server hook, newest first.
func (s *ProjectService) AutoDeployHistory(projectID string) (*AutoDeployRuns, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	c, err := s.dial(p.ServerID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, autodeploy.HistoryScript(slugOf(p)))
	if err != nil {
		return nil, err
	}
	runs, log := autodeploy.ParseHistory(lines)
	if runs == nil {
		runs = []autodeploy.Run{}
	}
	return &AutoDeployRuns{Runs: runs, LastLog: log}, nil
}

func (s *ProjectService) hasSuccessfulDeploy(projectID string) bool {
	deps, _ := s.store.ListDeployments(projectID)
	for _, d := range deps {
		if d.Status == "success" {
			return true
		}
	}
	return false
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
