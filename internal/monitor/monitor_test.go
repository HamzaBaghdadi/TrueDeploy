package monitor

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestParseMetrics(t *testing.T) {
	m := ParseMetrics(strings.Split(`cpu_total=400
cpu_idle=300
cpus=2
mem_total_kb=2000000
mem_avail_kb=150000
swap_total_kb=0
swap_free_kb=0
disk_total_kb=1000000
disk_used_kb=950000
load=3.50 4.20 2.00
uptime_s=90061
os=Ubuntu 24.04.4 LTS
hostname=vps
docker=yes`, "\n"))

	if m.CPUPercent != 25 || m.CPUs != 2 || !m.DockerReady || m.Hostname != "vps" {
		t.Fatalf("basic fields: %+v", m)
	}
	if m.MemTotal != 2000000*1024 || m.MemUsed != (2000000-150000)*1024 {
		t.Fatalf("memory: %+v", m)
	}
	if m.Load != [3]float64{3.5, 4.2, 2} || m.UptimeSec != 90061 {
		t.Fatalf("load/uptime: %+v", m)
	}
	all := strings.Join(m.Warnings, "\n")
	for _, want := range []string{"Disk is 95% full", "Memory is 92% used", "no swap", "overloaded"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing warning %q in:\n%s", want, all)
		}
	}

	old := ParseMetrics([]string{"mem_total_kb=1000", "mem_free_kb=600"})
	if old.MemUsed != 400*1024 {
		t.Errorf("without MemAvailable, fall back to free+buffers+cached: used=%d", old.MemUsed)
	}

	quiet := ParseMetrics([]string{"mem_total_kb=8000000", "mem_avail_kb=6000000", "swap_total_kb=1000", "disk_total_kb=100", "disk_used_kb=10", "cpus=4", "load=0.1 0.2 0.3"})
	if len(quiet.Warnings) != 0 {
		t.Fatalf("healthy server should have no warnings: %v", quiet.Warnings)
	}
}

func TestParseContainers(t *testing.T) {
	lines := []string{
		"stat\tapi-web-1\t1.25%\t120MiB / 1.9GiB",
		"stat\told-thing\t0.00%\t0B / 0B",
		"ps\tapi-web-1\tapi-web\trunning\tUp 2 hours\t127.0.0.1:3000->3000/tcp\tapi",
		"ps\told-thing\tnginx\texited\tExited (0) 3 days ago\t\t",
		"junk line",
	}
	cs := ParseContainers(lines)
	if len(cs) != 2 {
		t.Fatalf("got %+v", cs)
	}
	if cs[0].CPU != "1.25%" || cs[0].Memory != "120MiB / 1.9GiB" || cs[0].Project != "api" {
		t.Errorf("running container: %+v", cs[0])
	}
	if cs[1].CPU != "" || cs[1].State != "exited" {
		t.Errorf("stopped containers shouldn't show stats: %+v", cs[1])
	}
	if ParseContainers(nil) == nil {
		t.Error("empty list must not be nil (JSON null)")
	}
}

func TestActionScriptsRejectBadInput(t *testing.T) {
	if _, err := ActionScript("web; rm -rf /", "restart"); err == nil {
		t.Error("container names with shell characters must be rejected")
	}
	if _, err := ActionScript("web", "rm"); err == nil {
		t.Error("only restart/stop/start are allowed")
	}
	if s, err := ActionScript("api-web-1", "restart"); err != nil || !strings.Contains(s, "docker restart 'api-web-1'") {
		t.Errorf("ActionScript = %q, %v", s, err)
	}
	if _, err := ContainerLogsScript("$(id)", 10); err == nil {
		t.Error("logs: bad names must be rejected")
	}
}

func TestScriptsParseAndMetricsRunLocally(t *testing.T) {
	bash := `C:\Program Files\Git\bin\bash.exe`
	if p, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		bash = p
	}
	if exec.Command(bash, "-c", "true").Run() != nil {
		t.Skip("no bash")
	}
	for name, s := range map[string]string{"metrics": MetricsScript, "containers": ContainersScript, "disk": DiskUsageScript, "prune": PruneScript} {
		if out, err := exec.Command(bash, "-n", "-c", s).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}
