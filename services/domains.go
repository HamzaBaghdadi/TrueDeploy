package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"truedeploy/internal/deploy"
	"truedeploy/internal/proxy"
	"truedeploy/internal/sitecheck"
	"truedeploy/internal/store"
)

// proxyPort is the local port a project's domains forward to.
func proxyPort(p store.Project) int {
	if p.Runtime == deploy.RuntimeDockerfile {
		return p.HostPort
	}
	return p.AppPort
}

// sites builds the proxy routes for every project on a server. If changed is
// set it replaces the stored copy of that project (for validating edits).
func (s *ProjectService) sites(serverID string, changed *store.Project, all []store.Project) []proxy.Site {
	var out []proxy.Site
	add := func(p store.Project) {
		if p.ServerID == serverID && len(p.Domains) > 0 {
			out = append(out, proxy.Site{Project: p.Name, Domains: p.Domains, Port: proxyPort(p), StripPath: p.StripPath,
				AuthUser: p.AuthUser, AuthHash: p.AuthHash, Maintenance: p.Maintenance, MaintenanceMessage: p.MaintenanceMessage,
				Redirects: p.Redirects})
		}
	}
	for _, p := range all {
		if changed == nil || p.ID != changed.ID {
			add(p)
		}
	}
	if changed != nil {
		add(*changed)
	}
	return out
}

// ApplyDomains rewrites the server's proxy config from all its projects and
// starts or reloads the proxy. HTTPS certificates are issued automatically.
func (s *ProjectService) ApplyDomains(projectID string) (string, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	return s.applyServerDomains(p.ServerID)
}

// applyServerDomains writes the proxy config for every project on a server.
func (s *ProjectService) applyServerDomains(serverID string) (string, error) {
	all, err := s.store.ListProjects()
	if err != nil {
		return "", err
	}
	sites := s.sites(serverID, nil, all)
	if err := proxy.Validate(sites); err != nil {
		return "", err
	}
	c, err := s.dial(serverID)
	if err != nil {
		return "", err
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, proxy.ApplyScript(proxy.Caddyfile(sites)))
	if err != nil {
		return "", err
	}
	n := 0
	for _, st := range sites {
		n += len(st.Domains)
	}
	return fmt.Sprintf("%s Serving %d domain%s on this server. Certificates are requested automatically on first visit.",
		lastLine(lines), n, plural(n)), nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type DomainCheck struct {
	Domain string `json:"domain"`
	// DNS: "ok" (points at the server), "wrong" (points elsewhere), "missing".
	DNS        string   `json:"dns"`
	ResolvesTo []string `json:"resolvesTo"`
	// HTTPS: "ok", "error" (with Detail), or "skipped" when DNS isn't right.
	HTTPS      string `json:"https"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	LatencyMs  int64  `json:"latencyMs,omitempty"`
	// CertDays is how many days the HTTPS certificate stays valid (-1: unknown).
	CertDays int    `json:"certDays"`
	Detail   string `json:"detail,omitempty"`
	// Redirect marks a host that redirects to the project's first domain.
	Redirect bool `json:"redirect,omitempty"`
}

type DomainsStatus struct {
	Proxy     string        `json:"proxy"` // "running caddy vX" | "stopped" | error text
	ServerIPs []string      `json:"serverIps"`
	Checks    []DomainCheck `json:"checks"`
}

// lookup is swapped in tests.
var lookup = func(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// siteClient is shared by the Domains panel and background site checks.
var siteClient = sitecheck.Client(10 * time.Second)

// httpsGet is swapped in tests.
var httpsGet = func(ctx context.Context, url string) sitecheck.Result {
	return sitecheck.Check(ctx, siteClient, url)
}

// CheckDomains reports, per domain, whether DNS points at the server and
// whether HTTPS answers with a valid certificate.
func (s *ProjectService) CheckDomains(projectID string) (*DomainsStatus, error) {
	p, _, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	srv, _, err := s.store.Get(p.ServerID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	hosts := append(slices.Clone(p.Domains), p.Redirects...)
	st := &DomainsStatus{Checks: make([]DomainCheck, len(hosts))}
	if ip := net.ParseIP(srv.Host); ip != nil {
		st.ServerIPs = []string{ip.String()}
	} else if ips, err := lookup(ctx, srv.Host); err == nil {
		st.ServerIPs = ips
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		st.Proxy = s.proxyStatus(p.ServerID)
	}()
	for i, d := range hosts {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			st.Checks[i] = checkDomain(ctx, d, st.ServerIPs)
			st.Checks[i].Redirect = i >= len(p.Domains)
		}(i, d)
	}
	wg.Wait()
	return st, nil
}

func checkDomain(ctx context.Context, domain string, serverIPs []string) DomainCheck {
	c := DomainCheck{Domain: domain, HTTPS: "skipped", CertDays: -1}
	host, _ := proxy.SplitRoute(domain) // DNS is per host; the path only matters for HTTP
	ips, err := lookup(ctx, host)
	sort.Strings(ips)
	c.ResolvesTo = ips
	switch {
	case err != nil || len(ips) == 0:
		c.DNS = "missing"
		c.Detail = fmt.Sprintf("No DNS record yet. Add an A record for %s pointing to %s.", host, firstOr(serverIPs, "your server's IP"))
		return c
	case !overlaps(ips, serverIPs):
		c.DNS = "wrong"
		c.Detail = fmt.Sprintf("%s points to %s, not this server (%s). Update its A record.", host, strings.Join(ips, ", "), firstOr(serverIPs, "unknown"))
		return c
	}
	c.DNS = "ok"
	r := httpsGet(ctx, "https://"+domain+"/")
	if r.Status == 0 {
		c.HTTPS = "error"
		if err := r.Cause(); err != nil {
			c.Detail = httpsHint(err)
		} else {
			c.Detail = r.Err
		}
		return c
	}
	c.HTTPS, c.HTTPStatus, c.LatencyMs = "ok", r.Status, r.Latency
	c.CertDays = r.CertDaysLeft(time.Now())
	if r.Status == http.StatusBadGateway {
		c.Detail = "HTTPS works, but the app isn't answering on its port (502). Is it deployed and listening on the app port?"
	}
	return c
}

func httpsHint(err error) string {
	msg := err.Error()
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.As(err, &certErr), strings.Contains(msg, "certificate"):
		return "HTTPS answers but the certificate isn't valid yet. Apply the domains, then give Caddy a minute to get a certificate."
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"):
		return "Nothing is listening on port 443. Apply the domains to start the proxy, and make sure the firewall allows 80 and 443."
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "Timed out connecting on port 443. Check that the firewall (and your cloud provider's firewall) allows ports 80 and 443."
	case strings.Contains(msg, "tls: internal error"), strings.Contains(msg, "handshake"):
		return "The proxy is running but has no certificate for this domain yet. Apply the domains and wait a minute."
	}
	return msg
}

func (s *ProjectService) proxyStatus(serverID string) string {
	c, err := s.dial(serverID)
	if err != nil {
		return err.Error()
	}
	defer c.Close()
	lines, err := collect(context.Background(), c, proxy.StatusScript())
	if err != nil {
		return err.Error()
	}
	return lastLine(lines)
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}
	return def
}
