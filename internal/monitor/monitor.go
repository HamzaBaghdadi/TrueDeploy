// Package monitor reads server health and Docker state over SSH using only
// the login account: /proc, df, and the docker CLI. No agent is installed.
package monitor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MetricsScript prints key=value lines. CPU usage is sampled from /proc/stat
// over one second.
const MetricsScript = `cpu() { awk '/^cpu /{print $2+$3+$4+$5+$6+$7+$8+$9, $5+$6}' /proc/stat; }
read t1 i1 <<<"$(cpu)"; sleep 1; read t2 i2 <<<"$(cpu)"
echo "cpu_total=$((t2-t1))"
echo "cpu_idle=$((i2-i1))"
echo "cpus=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo)"
awk '/^MemTotal:/{print "mem_total_kb="$2} /^MemAvailable:/{print "mem_avail_kb="$2} /^(MemFree|Buffers|Cached):/{free+=$2} /^SwapTotal:/{print "swap_total_kb="$2} /^SwapFree:/{print "swap_free_kb="$2} END{print "mem_free_kb="free}' /proc/meminfo
df -Pk / | awk 'NR==2{print "disk_total_kb="$2; print "disk_used_kb="$3}'
echo "load=$(cut -d' ' -f1-3 /proc/loadavg)"
echo "uptime_s=$(cut -d. -f1 /proc/uptime)"
. /etc/os-release 2>/dev/null && echo "os=$PRETTY_NAME"
echo "hostname=$(hostname)"
if docker info >/dev/null 2>&1; then echo "docker=yes"; fi`

type Metrics struct {
	CPUPercent  float64    `json:"cpuPercent"`
	CPUs        int        `json:"cpus"`
	MemTotal    int64      `json:"memTotal"` // bytes
	MemUsed     int64      `json:"memUsed"`
	SwapTotal   int64      `json:"swapTotal"`
	SwapUsed    int64      `json:"swapUsed"`
	DiskTotal   int64      `json:"diskTotal"`
	DiskUsed    int64      `json:"diskUsed"`
	Load        [3]float64 `json:"load"`
	UptimeSec   int64      `json:"uptimeSec"`
	OS          string     `json:"os"`
	Hostname    string     `json:"hostname"`
	DockerReady bool       `json:"dockerReady"`
	// Warnings are short, actionable notes (low disk, high memory...).
	Warnings []string `json:"warnings"`
}

func ParseMetrics(lines []string) Metrics {
	kv := map[string]string{}
	for _, l := range lines {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			kv[k] = v
		}
	}
	num := func(k string) int64 { n, _ := strconv.ParseInt(kv[k], 10, 64); return n }

	m := Metrics{OS: kv["os"], Hostname: kv["hostname"], DockerReady: kv["docker"] == "yes", UptimeSec: num("uptime_s"), CPUs: int(num("cpus"))}
	if total := num("cpu_total"); total > 0 {
		m.CPUPercent = round1(100 * float64(total-num("cpu_idle")) / float64(total))
	}
	m.MemTotal = num("mem_total_kb") * 1024
	avail := num("mem_avail_kb")
	if _, ok := kv["mem_avail_kb"]; !ok {
		avail = num("mem_free_kb") // kernels before 3.14 lack MemAvailable
	}
	m.MemUsed = m.MemTotal - avail*1024
	m.SwapTotal = num("swap_total_kb") * 1024
	m.SwapUsed = m.SwapTotal - num("swap_free_kb")*1024
	m.DiskTotal = num("disk_total_kb") * 1024
	m.DiskUsed = num("disk_used_kb") * 1024
	for i, f := range strings.Fields(kv["load"]) {
		if i < 3 {
			m.Load[i], _ = strconv.ParseFloat(f, 64)
		}
	}

	m.Warnings = []string{}
	if p := pct(m.DiskUsed, m.DiskTotal); p >= 90 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("Disk is %d%% full. Clean up unused Docker images below, or grow the disk.", p))
	}
	if p := pct(m.MemUsed, m.MemTotal); p >= 90 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("Memory is %d%% used. Apps may be killed when it runs out.", p))
	}
	if m.SwapTotal == 0 && m.MemTotal > 0 && m.MemTotal < 2<<30 {
		m.Warnings = append(m.Warnings, "Less than 2 GB of RAM and no swap: Docker builds may fail with out-of-memory errors.")
	}
	if m.CPUs > 0 && m.Load[1] > float64(m.CPUs)*1.5 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("Load (%.1f) is well above the %d CPU cores: the server is overloaded.", m.Load[1], m.CPUs))
	}
	return m
}

func pct(a, b int64) int {
	if b <= 0 {
		return 0
	}
	return int(100 * a / b)
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// ContainersScript lists all containers joined with live CPU/memory stats.
// Output: one tab-separated line per container.
const ContainersScript = `docker info >/dev/null 2>&1 || { echo "Can't reach Docker as $(id -un)." >&2; exit 1; }
docker stats --no-stream --format '{{.Name}}	{{.CPUPerc}}	{{.MemUsage}}' 2>/dev/null | sed 's/^/stat	/'
docker ps -a --format '{{.Names}}	{{.Image}}	{{.State}}	{{.Status}}	{{.Ports}}	{{.Label "com.docker.compose.project"}}{{.Label "truedeploy.project"}}' | sed 's/^/ps	/'`

type Container struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"` // running, exited, restarting, paused...
	Status  string `json:"status"`
	Ports   string `json:"ports"`
	Project string `json:"project"` // compose project or TrueDeploy project
	CPU     string `json:"cpu"`     // e.g. "1.25%"
	Memory  string `json:"memory"`  // e.g. "120MiB / 1.9GiB"
}

func ParseContainers(lines []string) []Container {
	stats := map[string][2]string{}
	var out []Container
	for _, l := range lines {
		f := strings.Split(l, "\t")
		switch {
		case len(f) >= 4 && f[0] == "stat":
			stats[f[1]] = [2]string{f[2], f[3]}
		case len(f) >= 6 && f[0] == "ps":
			c := Container{Name: f[1], Image: f[2], State: f[3], Status: f[4], Ports: f[5]}
			if len(f) > 6 {
				c.Project = f[6]
			}
			out = append(out, c)
		}
	}
	for i := range out {
		if s, ok := stats[out[i].Name]; ok && out[i].State == "running" {
			out[i].CPU, out[i].Memory = s[0], s[1]
		}
	}
	if out == nil {
		out = []Container{}
	}
	return out
}

var containerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,254}$`)

// ActionScript runs restart/stop/start on a container.
func ActionScript(name, action string) (string, error) {
	if !containerNameRe.MatchString(name) {
		return "", fmt.Errorf("%q is not a valid container name", name)
	}
	switch action {
	case "restart", "stop", "start":
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
	return fmt.Sprintf(`docker %s '%s' >/dev/null && echo "%s: %s done."`, action, name, name, action), nil
}

// ContainerLogsScript prints the last lines of one container's logs.
func ContainerLogsScript(name string, tail int) (string, error) {
	if !containerNameRe.MatchString(name) {
		return "", fmt.Errorf("%q is not a valid container name", name)
	}
	if tail <= 0 || tail > 5000 {
		tail = 300
	}
	return fmt.Sprintf(`docker logs --tail %d --timestamps '%s' 2>&1`, tail, name), nil
}

// DiskUsageScript summarises Docker's disk usage by type.
const DiskUsageScript = `docker system df --format '{{.Type}}	{{.TotalCount}}	{{.Size}}	{{.Reclaimable}}'`

type DiskUsage struct {
	Type        string `json:"type"` // Images, Containers, Local Volumes, Build Cache
	Count       string `json:"count"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

func ParseDiskUsage(lines []string) []DiskUsage {
	out := []DiskUsage{}
	for _, l := range lines {
		f := strings.Split(l, "\t")
		if len(f) >= 4 {
			out = append(out, DiskUsage{Type: f[0], Count: f[1], Size: f[2], Reclaimable: f[3]})
		}
	}
	return out
}

// PruneScript removes images no container uses, stopped containers are kept,
// and build cache. Volumes (app data) are never touched.
const PruneScript = `before=$(df -Pk / | awk 'NR==2{print $3}')
docker image prune -af >/dev/null
docker builder prune -af >/dev/null 2>&1 || true
after=$(df -Pk / | awk 'NR==2{print $3}')
freed=$(( (before - after) / 1024 ))
[ "$freed" -lt 0 ] && freed=0
echo "Freed about ${freed} MB (unused images and build cache). Volumes were not touched."`
