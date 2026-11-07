package autodeploy

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"truedeploy/internal/gitprov"
)

// Auto-deploy on providers other than GitHub. Every provider ends the same
// way: the CI job connects with the locked key and runs "deploy", which the
// server turns into the project's hook. What differs is where the pipeline
// lives and how secrets are stored.
//
//   - Gitea/Forgejo Actions: a GitHub-style workflow per project in .gitea/workflows.
//   - GitLab CI/CD: a job file per project in .gitlab/, included from
//     .gitlab-ci.yml (created and kept by TrueDeploy when the repo has none).
//   - Bitbucket Pipelines: bitbucket-pipelines.yml (created and kept by
//     TrueDeploy when the repo has none), one step per project.
//
// GitLab and Bitbucket store the SSH key and known hosts base64-encoded:
// Bitbucket variables can't hold line breaks.

// ProjectFile is the per-project pipeline file ("" when the provider has none).
func ProjectFile(kind gitprov.Kind, slug string) string {
	switch kind {
	case gitprov.GitHub:
		return WorkflowPath(slug)
	case gitprov.Gitea:
		return ".gitea/workflows/truedeploy-" + slug + ".yml"
	case gitprov.GitLab:
		return ".gitlab/truedeploy-" + slug + ".yml"
	}
	return ""
}

// RootFile is the repository's main CI file TrueDeploy may manage ("" if none).
func RootFile(kind gitprov.Kind) string {
	switch kind {
	case gitprov.GitLab:
		return ".gitlab-ci.yml"
	case gitprov.Bitbucket:
		return "bitbucket-pipelines.yml"
	}
	return ""
}

// encoded reports whether the provider gets base64-encoded key secrets.
func encoded(kind gitprov.Kind) bool { return kind == gitprov.GitLab || kind == gitprov.Bitbucket }

// ProviderSecrets are the CI secrets/variables for a provider.
func ProviderSecrets(kind gitprov.Kind, slug, user, host string, port int, privatePEM string, hostKeys []string) map[string]string {
	s := Secrets(slug, user, host, port, privatePEM, hostKeys)
	if !encoded(kind) {
		return s
	}
	p := SecretPrefix(slug)
	for _, k := range []string{"_SSH_KEY", "_KNOWN_HOSTS"} {
		s[p+k+"_B64"] = base64.StdEncoding.EncodeToString([]byte(s[p+k]))
		delete(s, p+k)
	}
	return s
}

// SecretNames lists a project's secret names on a provider (for removal).
func SecretNames(kind gitprov.Kind, slug string) []string {
	var out []string
	for k := range ProviderSecrets(kind, slug, "", "", 22, "", nil) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// deployCommands is the shell a GitLab job or Bitbucket step runs.
func deployCommands(slug string) []string {
	p := SecretPrefix(slug)
	return []string{
		"apk add --no-cache openssh-client",
		"install -m 700 -d ~/.ssh",
		fmt.Sprintf(`echo "$%s_SSH_KEY_B64" | base64 -d > ~/.ssh/truedeploy && chmod 600 ~/.ssh/truedeploy`, p),
		fmt.Sprintf(`echo "$%s_KNOWN_HOSTS_B64" | base64 -d > ~/.ssh/known_hosts`, p),
		"# The server only lets this key run the project's deploy hook.",
		fmt.Sprintf(`ssh -i ~/.ssh/truedeploy -p "$%[1]s_PORT" -o BatchMode=yes -o StrictHostKeyChecking=yes "$%[1]s_TARGET" deploy`, p),
	}
}

func cleanName(name string) string {
	return strings.TrimSpace(regexp.MustCompile(`[^A-Za-z0-9 ._()-]+`).ReplaceAllString(name, " "))
}

// ProjectPipeline is the content of ProjectFile.
func ProjectPipeline(kind gitprov.Kind, slug, name, branch string) string {
	switch kind {
	case gitprov.GitHub, gitprov.Gitea:
		return Workflow(slug, name, branch)
	case gitprov.GitLab:
		var b strings.Builder
		fmt.Fprintf(&b, `# Managed by TrueDeploy. Deploys %[1]s to your server on every push to %[2]s.
# Turn it off from the project page in TrueDeploy (that also removes the server key).
truedeploy-%[3]s:
  stage: .post
  image: alpine:3
  resource_group: truedeploy-%[3]s
  rules:
    - if: '$CI_COMMIT_BRANCH == "%[2]s"'
  script:
`, cleanName(name), branch, slug)
		for _, c := range deployCommands(slug) {
			if !strings.HasPrefix(c, "#") {
				fmt.Fprintf(&b, "    - %s\n", c)
			}
		}
		return b.String()
	}
	return ""
}

// DisabledGitLabJob replaces a job file that a hand-written .gitlab-ci.yml
// still includes: removing the file would break that pipeline.
func DisabledGitLabJob(slug string) string {
	return fmt.Sprintf(`# TrueDeploy auto-deploy for this project is off. Remove the line
# "- local: .gitlab/truedeploy-%[1]s.yml" from .gitlab-ci.yml, then delete this file.
truedeploy-%[1]s:
  stage: .post
  rules:
    - when: never
  script:
    - "true"
`, slug)
}

// Entry is one project in a managed root file.
type Entry struct{ Slug, Branch, Name string }

const managedHeader = "# Managed by TrueDeploy"

var entryRe = regexp.MustCompile(`(?m)^# truedeploy: (\S+) (\S+) ?(.*)$`)

// IsManaged reports whether TrueDeploy wrote a root file.
func IsManaged(content string) bool { return strings.HasPrefix(content, managedHeader) }

// UpdateRoot adds or removes a project in the repository's root CI file.
// It returns the new content, or remove=true when no projects are left (the
// file should be deleted). A root file written by hand is never changed:
// ok=false comes with instructions for the person instead.
func UpdateRoot(kind gitprov.Kind, current string, exists bool, e Entry, add bool) (content string, remove bool, instructions string) {
	if exists && !IsManaged(current) {
		return "", false, handWritten(kind, current, e, add)
	}
	var entries []Entry
	for _, m := range entryRe.FindAllStringSubmatch(current, -1) {
		if m[1] != e.Slug {
			entries = append(entries, Entry{Slug: m[1], Branch: m[2], Name: m[3]})
		}
	}
	if add {
		entries = append(entries, Entry{Slug: e.Slug, Branch: e.Branch, Name: cleanName(e.Name)})
	}
	if len(entries) == 0 {
		return "", exists, ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Slug < entries[j].Slug })
	return renderRoot(kind, entries), false, ""
}

func renderRoot(kind gitprov.Kind, entries []Entry) string {
	var b strings.Builder
	b.WriteString(managedHeader + ": auto-deploys from this repository to your servers.\n")
	b.WriteString("# TrueDeploy rewrites this file when auto-deploy is turned on or off for a project,\n")
	b.WriteString("# using the lines below. To add your own CI, see the TrueDeploy docs first.\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "# truedeploy: %s %s %s\n", e.Slug, e.Branch, e.Name)
	}
	switch kind {
	case gitprov.GitLab:
		b.WriteString("include:\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "  - local: %s\n", ProjectFile(gitprov.GitLab, e.Slug))
		}
	case gitprov.Bitbucket:
		b.WriteString("image: alpine:3\n\npipelines:\n  branches:\n")
		byBranch := map[string][]Entry{}
		var branches []string
		for _, e := range entries {
			if _, ok := byBranch[e.Branch]; !ok {
				branches = append(branches, e.Branch)
			}
			byBranch[e.Branch] = append(byBranch[e.Branch], e)
		}
		sort.Strings(branches)
		for _, br := range branches {
			fmt.Fprintf(&b, "    '%s':\n", br)
			for _, e := range byBranch[br] {
				b.WriteString(bitbucketStep(e))
			}
		}
	}
	return b.String()
}

func bitbucketStep(e Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "      - step:\n          name: 'Deploy %s (TrueDeploy)'\n          script:\n", strings.ReplaceAll(cleanName(e.Name), "'", ""))
	for _, c := range deployCommands(e.Slug) {
		if !strings.HasPrefix(c, "#") {
			fmt.Fprintf(&b, "            - %s\n", c)
		}
	}
	return b.String()
}

// handWritten explains what to change in a CI file TrueDeploy didn't write.
func handWritten(kind gitprov.Kind, current string, e Entry, add bool) string {
	file := RootFile(kind)
	switch kind {
	case gitprov.GitLab:
		line := "- local: " + ProjectFile(gitprov.GitLab, e.Slug)
		has := strings.Contains(current, ProjectFile(gitprov.GitLab, e.Slug))
		if add && !has {
			return fmt.Sprintf("This repository already has its own %s, so TrueDeploy won't change it. Add these lines to it, then turn auto-deploy on again:\n\ninclude:\n  %s", file, line)
		}
		if !add && has {
			return fmt.Sprintf("Remove the line \"%s\" from %s; the job is switched off until you do.", line, file)
		}
	case gitprov.Bitbucket:
		has := strings.Contains(current, SecretPrefix(e.Slug)+"_TARGET")
		if add && !has {
			return fmt.Sprintf("This repository already has its own %s, so TrueDeploy won't change it. Add this step under pipelines → branches → '%s', then turn auto-deploy on again:\n\n%s", file, e.Branch, bitbucketStep(e))
		}
		if !add && has {
			return fmt.Sprintf("Remove the \"Deploy %s (TrueDeploy)\" step from %s; it fails without the removed variables.", cleanName(e.Name), file)
		}
	}
	return ""
}
