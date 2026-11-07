package autodeploy

import (
	"encoding/base64"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"truedeploy/internal/gitprov"
)

func TestGitLabRootAndJobs(t *testing.T) {
	root, remove, note := UpdateRoot(gitprov.GitLab, "", false, Entry{Slug: "web", Branch: "main", Name: "Shop web"}, true)
	if remove || note != "" || !IsManaged(root) {
		t.Fatalf("%q %v %q", root, remove, note)
	}
	root, _, _ = UpdateRoot(gitprov.GitLab, root, true, Entry{Slug: "api", Branch: "release/v2", Name: "Shop API"}, true)
	var parsed struct {
		Include []struct {
			Local string `yaml:"local"`
		} `yaml:"include"`
	}
	if err := yaml.Unmarshal([]byte(root), &parsed); err != nil {
		t.Fatalf("%v\n%s", err, root)
	}
	if len(parsed.Include) != 2 || parsed.Include[0].Local != ".gitlab/truedeploy-api.yml" || parsed.Include[1].Local != ".gitlab/truedeploy-web.yml" {
		t.Errorf("includes: %+v", parsed.Include)
	}

	job := ProjectPipeline(gitprov.GitLab, "api", "Shop API", "release/v2")
	var jobs map[string]struct {
		Stage  string   `yaml:"stage"`
		Script []string `yaml:"script"`
		Rules  []struct {
			If string `yaml:"if"`
		} `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(job), &jobs); err != nil {
		t.Fatalf("%v\n%s", err, job)
	}
	j := jobs["truedeploy-api"]
	if j.Stage != ".post" || len(j.Rules) != 1 || j.Rules[0].If != `$CI_COMMIT_BRANCH == "release/v2"` {
		t.Errorf("job: %+v", j)
	}
	if last := j.Script[len(j.Script)-1]; !strings.Contains(last, `"$TRUEDEPLOY_API_TARGET" deploy`) {
		t.Errorf("last command: %s", last)
	}
	if err := yaml.Unmarshal([]byte(DisabledGitLabJob("api")), &jobs); err != nil {
		t.Error(err)
	}

	// Removing projects: the file goes away with the last one.
	root, remove, _ = UpdateRoot(gitprov.GitLab, root, true, Entry{Slug: "web"}, false)
	if remove || strings.Contains(root, "truedeploy-web") || !strings.Contains(root, "truedeploy-api") {
		t.Fatalf("after removing web: %s", root)
	}
	if _, remove, _ = UpdateRoot(gitprov.GitLab, root, true, Entry{Slug: "api"}, false); !remove {
		t.Error("the managed file should be deleted with its last project")
	}
}

func TestHandWrittenCIFilesAreLeftAlone(t *testing.T) {
	mine := "stages: [test]\ntest:\n  script: [make test]\n"
	_, _, note := UpdateRoot(gitprov.GitLab, mine, true, Entry{Slug: "api", Branch: "main"}, true)
	if !strings.Contains(note, "- local: .gitlab/truedeploy-api.yml") {
		t.Errorf("should explain the include line: %s", note)
	}
	withInclude := mine + "include:\n  - local: .gitlab/truedeploy-api.yml\n"
	if content, remove, note := UpdateRoot(gitprov.GitLab, withInclude, true, Entry{Slug: "api", Branch: "main"}, true); content != "" || remove || note != "" {
		t.Error("already included: nothing to change")
	}
	if _, _, note := UpdateRoot(gitprov.GitLab, withInclude, true, Entry{Slug: "api"}, false); !strings.Contains(note, "Remove the line") {
		t.Error(note)
	}
	_, _, note = UpdateRoot(gitprov.Bitbucket, "pipelines:\n  default: []\n", true, Entry{Slug: "shop", Branch: "main", Name: "Shop"}, true)
	if !strings.Contains(note, "- step:") || !strings.Contains(note, "TRUEDEPLOY_SHOP_TARGET") {
		t.Errorf("should include the step to paste: %s", note)
	}
}

func TestBitbucketPipelines(t *testing.T) {
	root, _, _ := UpdateRoot(gitprov.Bitbucket, "", false, Entry{Slug: "web", Branch: "main", Name: "Web's site"}, true)
	root, _, _ = UpdateRoot(gitprov.Bitbucket, root, true, Entry{Slug: "api", Branch: "main", Name: "API"}, true)
	root, _, _ = UpdateRoot(gitprov.Bitbucket, root, true, Entry{Slug: "docs", Branch: "docs", Name: "Docs"}, true)
	var p struct {
		Image     string `yaml:"image"`
		Pipelines struct {
			Branches map[string][]struct {
				Step struct {
					Name   string   `yaml:"name"`
					Script []string `yaml:"script"`
				} `yaml:"step"`
			} `yaml:"branches"`
		} `yaml:"pipelines"`
	}
	if err := yaml.Unmarshal([]byte(root), &p); err != nil {
		t.Fatalf("%v\n%s", err, root)
	}
	main := p.Pipelines.Branches["main"]
	if len(main) != 2 || main[0].Step.Name != "Deploy API (TrueDeploy)" || main[1].Step.Name != "Deploy Web s site (TrueDeploy)" || len(p.Pipelines.Branches["docs"]) != 1 {
		t.Fatalf("branches: %+v\n%s", p.Pipelines.Branches, root)
	}
	if !strings.Contains(main[0].Step.Script[2], "$TRUEDEPLOY_API_SSH_KEY_B64") {
		t.Error(main[0].Step.Script)
	}
}

func TestProviderSecrets(t *testing.T) {
	gh := ProviderSecrets(gitprov.GitHub, "shop", "u", "h", 22, "KEY\nLINES", []string{"ssh-ed25519 AAA"})
	if gh["TRUEDEPLOY_SHOP_SSH_KEY"] != "KEY\nLINES" {
		t.Error("GitHub keeps plain secrets")
	}
	bb := ProviderSecrets(gitprov.Bitbucket, "shop", "u", "h", 22, "KEY\nLINES", []string{"ssh-ed25519 AAA"})
	if _, plain := bb["TRUEDEPLOY_SHOP_SSH_KEY"]; plain {
		t.Error("Bitbucket gets base64 only")
	}
	if raw, _ := base64.StdEncoding.DecodeString(bb["TRUEDEPLOY_SHOP_SSH_KEY_B64"]); string(raw) != "KEY\nLINES" {
		t.Error("bad base64")
	}
	for _, v := range bb {
		if strings.Contains(v, "\n") {
			t.Error("Bitbucket variables can't contain line breaks")
		}
	}
	if names := SecretNames(gitprov.GitLab, "shop"); len(names) != 4 || names[0] != "TRUEDEPLOY_SHOP_KNOWN_HOSTS_B64" {
		t.Error(names)
	}
	if ProjectFile(gitprov.Gitea, "shop") != ".gitea/workflows/truedeploy-shop.yml" || ProjectFile(gitprov.Bitbucket, "shop") != "" {
		t.Error("project files")
	}
}
