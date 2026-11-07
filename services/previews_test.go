package services

import (
	"strings"
	"testing"
)

func TestPreviews(t *testing.T) {
	st := newTestStore(t)
	svc := NewProjectService(st, nil)
	web, err := svc.SaveProject(ProjectInput{Name: "web", ServerID: "s1", RepoURL: "git@github.com:me/web.git", Runtime: "dockerfile",
		HostPort: 8080, ContainerPort: 80, Preset: "react-vite", Domains: []string{"shop.io"}, Env: map[string]string{"VITE_API": "/api"},
		Links: nil})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPreviewDomain(web.ID, "https://Preview.Shop.io/"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPreviewDomain(web.ID, "not a domain"); err == nil {
		t.Error("bad preview domain accepted")
	}

	p, err := svc.CreatePreview(PreviewInput{ProjectID: web.ID, Branch: "feature/Login_v2"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch != "feature/Login_v2" || p.PreviewOf != web.ID || p.HostPort == 8080 || p.HostPort == 0 || p.Preset != "react-vite" {
		t.Errorf("preview: %+v", p.Project)
	}
	if len(p.Domains) != 1 || p.Domains[0] != "feature-login-v2.preview.shop.io" {
		t.Errorf("preview domain: %v", p.Domains)
	}
	if !strings.HasPrefix(p.Slug, "web-feature-login") {
		t.Errorf("slug: %s", p.Slug)
	}
	_, env, _ := st.GetProject(p.ID)
	if env["VITE_API"] != "/api" {
		t.Errorf("env should be copied: %v", env)
	}
	list, _ := svc.Previews(web.ID)
	if len(list) != 1 || list[0].ID != p.ID {
		t.Errorf("previews: %+v", list)
	}

	// Guards.
	for _, c := range []struct {
		in   PreviewInput
		want string
	}{
		{PreviewInput{ProjectID: web.ID, Branch: "feature/Login_v2"}, "already has a preview"},
		{PreviewInput{ProjectID: web.ID, Branch: "main"}, "already deploys"},
		{PreviewInput{ProjectID: web.ID, Branch: " "}, "choose a branch"},
		{PreviewInput{ProjectID: p.ID, Branch: "x"}, "already a preview"},
	} {
		if _, err := svc.CreatePreview(c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.in, err, c.want)
		}
	}
	compose, _ := svc.SaveProject(ProjectInput{Name: "api", ServerID: "s1", RepoURL: "git@github.com:me/api.git", Runtime: "compose"})
	if _, err := svc.CreatePreview(PreviewInput{ProjectID: compose.ID, Branch: "dev"}); err == nil || !strings.Contains(err.Error(), "Dockerfile runtime") {
		t.Errorf("compose: %v", err)
	}
	if _, err := svc.DeletePreview(web.ID); err == nil {
		t.Error("deleting the main project as a preview")
	}

	// An edit of the preview keeps it a preview.
	in := ProjectInput{ID: p.ID, Name: "renamed", ServerID: "s1", RepoURL: p.RepoURL, Branch: p.Branch, Runtime: "dockerfile", HostPort: p.HostPort, ContainerPort: 80}
	if _, err := svc.SaveProject(in); err != nil {
		t.Fatal(err)
	}
	if again, _, _ := st.GetProject(p.ID); again.PreviewOf != web.ID {
		t.Error("edit dropped PreviewOf")
	}
}

func TestMainProjectWithPreviewsCantBeDeleted(t *testing.T) {
	svc := NewProjectService(newTestStore(t), nil)
	web, _ := svc.SaveProject(ProjectInput{Name: "web", ServerID: "s1", RepoURL: "git@github.com:me/web.git", Runtime: "dockerfile", HostPort: 8080, ContainerPort: 80})
	if _, err := svc.CreatePreview(PreviewInput{ProjectID: web.ID, Branch: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteProject(web.ID); err == nil || !strings.Contains(err.Error(), "preview") {
		t.Errorf("got %v", err)
	}
}
