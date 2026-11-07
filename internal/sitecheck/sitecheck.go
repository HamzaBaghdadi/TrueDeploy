// Package sitecheck requests a site over HTTPS the way a visitor would and
// reports the status, response time and certificate expiry.
package sitecheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Result struct {
	URL     string `json:"url"`
	Status  int    `json:"status,omitempty"` // 0 when there was no answer
	Latency int64  `json:"latencyMs"`
	// CertExpires is the leaf certificate's NotAfter (zero without TLS).
	CertExpires time.Time `json:"certExpires,omitempty"`
	Err         string    `json:"error,omitempty"`

	cause error // the original error, for callers that explain it their own way
}

// Up: the site answered and it isn't a server-side failure. 4xx still means
// the server and app are working (a login wall, no page at /).
func (r Result) Up() bool { return r.Err == "" && r.Status > 0 && r.Status < 500 }

// Cause is the underlying error of a failed check (nil if it answered).
func (r Result) Cause() error { return r.cause }

// Failed builds the result of a check that got no answer (used by fakes in tests).
func Failed(url string, err error) Result { return Result{URL: url, Err: Explain(err), cause: err} }

// CertDaysLeft is the number of whole days until the certificate expires,
// or -1 when unknown.
func (r Result) CertDaysLeft(now time.Time) int {
	if r.CertExpires.IsZero() {
		return -1
	}
	d := r.CertExpires.Sub(now)
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}

// Client returns an HTTP client for checks: no cookies, a short timeout and
// a recognizable user agent (set per request).
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// Check requests url and follows redirects. The certificate reported is the
// one from the first HTTPS hop (the checked domain's own).
func Check(ctx context.Context, client *http.Client, url string) Result {
	r := Result{URL: url}
	var first *tls.ConnectionState
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if first == nil && via[0].Response != nil {
			first = via[0].Response.TLS
		}
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		r.Err = err.Error()
		return r
	}
	req.Header.Set("User-Agent", "TrueDeploy-SiteCheck/1")
	start := time.Now()
	resp, err := c.Do(req)
	r.Latency = time.Since(start).Milliseconds()
	if err != nil {
		r.cause = err
		r.Err = Explain(err)
		return r
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	r.Status = resp.StatusCode
	state := first
	if state == nil {
		state = resp.TLS
	}
	if state != nil && len(state.PeerCertificates) > 0 {
		r.CertExpires = state.PeerCertificates[0].NotAfter
	}
	return r
}

// Explain turns connection errors into short, plain reasons.
func Explain(err error) string {
	msg := err.Error()
	var certErr *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "the HTTPS certificate has expired"
	case errors.As(err, &hostErr):
		return "the HTTPS certificate is for a different domain"
	case errors.As(err, &unknown), errors.As(err, &certErr), strings.Contains(msg, "certificate"):
		return "the HTTPS certificate isn't valid"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"):
		return "nothing is listening on port 443"
	case strings.Contains(msg, "Client.Timeout"), strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "no answer within the time limit"
	case strings.Contains(msg, "no such host"):
		return "the domain doesn't resolve"
	case strings.Contains(msg, "tls: internal error"), strings.Contains(msg, "handshake"):
		return "the HTTPS handshake failed (no certificate for this domain?)"
	case strings.Contains(msg, "too many redirects"):
		return "it redirects in a loop"
	}
	return fmt.Sprintf("request failed: %s", msg)
}
