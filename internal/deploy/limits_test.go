package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeLimits(t *testing.T) {
	run, repo, home, dockerLog := env(t, map[string]string{"compose.yaml": "services: {}\n"})
	bin := filepath.Join(filepath.Dir(dockerLog), "bin", "docker")
	os.WriteFile(bin, []byte(`#!/bin/sh
echo "$*" >> '`+bashPath(dockerLog)+`'
case "$*" in
  *"config --services"*) printf 'web\ndb\n' ;;
  "info --format {{.MemoryLimit}}") echo false ;;
  "info --format {{.CPUCfsQuota}}") echo true ;;
esac
`), 0o755)

	spec := Spec{Slug: "shop", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeCompose, MemoryMB: 512, CPUs: 1.5}
	out, err := run(Script(spec))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	log, _ := os.ReadFile(dockerLog)
	if !strings.Contains(string(log), "compose -p shop -f compose.yaml -f .truedeploy.limits.yml up -d") {
		t.Errorf("up should use the limits override:\n%s", log)
	}
	lim, _ := os.ReadFile(filepath.Join(home, "apps", "shop", ".truedeploy.limits.yml"))
	want := "  web:\n    memswap_limit: 512M\n    deploy:\n      resources:\n        limits:\n          cpus: \"1.5\"\n          memory: 512M\n  db:\n"
	if !strings.Contains(string(lim), want) {
		t.Errorf("limits override:\n%s\nwant to contain:\n%s", lim, want)
	}
	if !strings.Contains(out, "can't enforce memory limits") || strings.Contains(out, "can't enforce CPU limits") {
		t.Errorf("should warn about memory limits only:\n%s", out)
	}

	// Without limits there's no override, so containers are recreated without them.
	spec.MemoryMB, spec.CPUs = 0, 0
	os.Truncate(dockerLog, 0)
	if out, err := run(Script(spec)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if l, _ := os.ReadFile(dockerLog); strings.Contains(string(l), "limits.yml") || !strings.Contains(string(l), "compose -p shop -f compose.yaml up -d") {
		t.Errorf("no limits should mean no override:\n%s", l)
	}
}

func TestDockerfileLimits(t *testing.T) {
	run, repo, _, dockerLog := env(t, map[string]string{"Dockerfile": "FROM scratch\n"})
	spec := Spec{Slug: "web", RepoURL: "file://" + bashPath(repo), Branch: "main", Runtime: RuntimeDockerfile, HostPort: 8080, ContainerPort: 80, MemoryMB: 256, CPUs: 0.5}
	if out, err := run(Script(spec)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if l, _ := os.ReadFile(dockerLog); !strings.Contains(string(l), "-p 8080:80 --memory 256m --memory-swap 256m --cpus 0.5 truedeploy/web:") {
		t.Errorf("docker run should carry the limits:\n%s", l)
	}
}

func TestValidateLimits(t *testing.T) {
	ok := Spec{Slug: "api", RepoURL: "git@github.com:me/api.git", Branch: "main", Runtime: RuntimeCompose}
	for _, c := range []struct {
		mem  int
		cpus float64
		ok   bool
	}{{0, 0, true}, {512, 1, true}, {32, 0.1, true}, {16, 0, false}, {0, 0.05, false}, {0, 300, false}, {-1, 0, false}} {
		s := ok
		s.MemoryMB, s.CPUs = c.mem, c.cpus
		if err := s.Validate(); (err == nil) != c.ok {
			t.Errorf("mem=%d cpus=%v: err=%v, want ok=%v", c.mem, c.cpus, err, c.ok)
		}
	}
}

func TestStatusScriptReportsUsage(t *testing.T) {
	run, _, _, dockerLog := env(t, map[string]string{"x": "x"})
	bin := filepath.Join(filepath.Dir(dockerLog), "bin", "docker")
	os.WriteFile(bin, []byte(`#!/bin/sh
case "$*" in
  "ps -a"*) printf 'shop-web-1\trunning\tUp 2 hours\t\n' ;;
  "ps -q --filter label=com.docker.compose.project=shop") echo abc123 ;;
  "stats --no-stream"*abc123) printf 'stat\tshop-web-1\t1.25%%\t120MiB / 512MiB\t23.44%%\n' ;;
esac
`), 0o755)
	out, err := run(StatusScript("shop"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "shop-web-1\trunning") || !strings.Contains(out, "stat\tshop-web-1\t1.25%\t120MiB / 512MiB\t23.44%") {
		t.Errorf("status output:\n%s", out)
	}
}
