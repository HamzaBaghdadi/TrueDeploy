package services

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"truedeploy/internal/deploy"
	"truedeploy/internal/proxy"
	"truedeploy/internal/store"
)

// Branches lists the repository's branches through its Git host (when connected).
func (s *ProjectService) Branches(projectID string) ([]string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if p.IsApp() {
		return nil, errNoRepo
	}
	prov, repo, err := s.git.forRepo(p.RepoURL)
	if err != nil {
		if isNotConnected(err) {
			return nil, errors.New("connect this repository's Git host in Settings to list branches, or type the branch name")
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return prov.Branches(ctx, repo)
}

// SetPreviewDomain sets the base domain previews get a subdomain of
// ("" turns preview domains off).
func (s *ProjectService) SetPreviewDomain(projectID, domain string) error {
	p, env, err := s.store.GetProject(projectID)
	if err != nil {
		return err
	}
	domain = proxy.NormalizeDomain(domain)
	if domain != "" && !proxy.ValidDomain(domain) {
		return fmt.Errorf("%q isn't a domain like preview.example.com", domain)
	}
	p.PreviewDomain = domain
	return s.store.PutProject(p, env)
}

// Previews lists the branch previews of a project.
func (s *ProjectService) Previews(projectID string) ([]ProjectView, error) {
	all, err := s.store.ListProjects()
	if err != nil {
		return nil, err
	}
	servers := s.serverMap()
	out := []ProjectView{}
	for _, p := range all {
		if p.PreviewOf == projectID {
			out = append(out, s.view(p, servers))
		}
	}
	return out, nil
}

type PreviewInput struct {
	ProjectID string `json:"projectId"`
	Branch    string `json:"branch"`
	// CopyDatabases links the same databases as the main project. Off by
	// default: a preview's migrations would run against them.
	CopyDatabases bool `json:"copyDatabases"`
}

// previewLabel turns a branch into a DNS label: feature/Login_v2 -> feature-login-v2.
func previewLabel(branch string) string {
	l := strings.ReplaceAll(deploy.Slug(strings.ReplaceAll(branch, "/", "-")), "_", "-")
	if len(l) > 40 {
		l = strings.Trim(l[:40], "-")
	}
	return l
}

// canPreview explains why a project can't have previews ("" if it can).
func canPreview(p store.Project) string {
	switch {
	case p.PreviewOf != "":
		return "This is already a preview."
	case p.IsApp():
		return "One-click apps have no branches to preview."
	case p.Runtime != deploy.RuntimeDockerfile:
		return "Previews need the Dockerfile runtime (or a framework preset): with Docker Compose, the ports are fixed in your compose file, so a second copy would collide with the main one."
	}
	return ""
}

// CreatePreview deploys a branch next to the main project: same build
// settings and environment, its own folder, port and (optionally) domain.
func (s *ProjectService) CreatePreview(in PreviewInput) (*ProjectView, error) {
	parent, env, err := s.store.GetProject(in.ProjectID)
	if err != nil {
		return nil, err
	}
	if why := canPreview(parent); why != "" {
		return nil, errors.New(why)
	}
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		return nil, errors.New("choose a branch")
	}
	if branch == parent.Branch {
		return nil, fmt.Errorf("%s is the branch the main project already deploys", branch)
	}
	label := previewLabel(branch)
	if label == "" {
		return nil, fmt.Errorf("%q can't be used in a folder or domain name", branch)
	}
	existing, _ := s.Previews(parent.ID)
	for _, e := range existing {
		if e.Branch == branch {
			return nil, fmt.Errorf("%s already has a preview: %s", branch, e.Name)
		}
	}
	port, err := s.freePort(parent.ServerID, parent.HostPort+1)
	if err != nil {
		return nil, err
	}
	in2 := ProjectInput{
		Name: parent.Name + " · " + branch, ServerID: parent.ServerID, RepoURL: parent.RepoURL, Branch: branch,
		Runtime: parent.Runtime, HostPort: port, ContainerPort: parent.ContainerPort,
		Folder: parent.Folder, Preset: parent.Preset, PresetOptions: maps.Clone(parent.PresetOptions),
		Env: maps.Clone(env), StripPath: parent.StripPath, EnvGroups: slices.Clone(parent.EnvGroups),
		NoHealthCheck: parent.NoHealthCheck, HealthPath: parent.HealthPath, HealthTimeout: parent.HealthTimeout,
		MemoryMB: parent.MemoryMB, CPUs: parent.CPUs,
	}
	if in.CopyDatabases {
		in2.Links = slices.Clone(parent.Links)
	}
	if parent.PreviewDomain != "" {
		in2.Domains = []string{label + "." + parent.PreviewDomain}
	}
	v, err := s.SaveProject(in2)
	if err != nil {
		return nil, err
	}
	p, penv, err := s.store.GetProject(v.ID)
	if err != nil {
		return nil, err
	}
	p.PreviewOf = parent.ID
	if err := s.store.PutProject(p, penv); err != nil {
		return nil, err
	}
	out := s.view(p, s.serverMap())
	return &out, nil
}

// DeletePreview stops a preview's container, removes its HTTPS route and
// forgets it. The main project isn't touched.
func (s *ProjectService) DeletePreview(previewID string) (string, error) {
	p, _, err := s.store.GetProject(previewID)
	if err != nil {
		return "", err
	}
	if p.PreviewOf == "" {
		return "", errors.New("this isn't a preview")
	}
	var notes []string
	if _, err := s.StopProject(previewID); err != nil {
		notes = append(notes, "Its container couldn't be stopped ("+err.Error()+"); remove td-"+slugOf(p)+" on the server.")
	}
	if err := s.store.DeleteProject(previewID); err != nil {
		return "", err
	}
	if len(p.Domains) > 0 {
		if _, err := s.applyServerDomains(p.ServerID); err != nil {
			notes = append(notes, "Its domain is still in the proxy config ("+err.Error()+"); apply domains on another project to remove it.")
		}
	}
	return strings.TrimSpace("Preview of " + p.Branch + " deleted. " + strings.Join(notes, " ")), nil
}
