// Package s3 is a small client for S3-compatible storage (AWS S3, Cloudflare
// R2, Backblaze B2, Wasabi, DigitalOcean Spaces, MinIO, SeaweedFS). It covers
// what offsite backups need: list, download, upload a small object, delete.
// Requests are signed with AWS Signature Version 4.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Config is where the bucket lives. An empty Endpoint means AWS S3.
type Config struct {
	Endpoint  string // e.g. https://<account>.r2.cloudflarestorage.com
	Region    string // e.g. us-east-1, auto (R2)
	Bucket    string
	AccessKey string
	SecretKey string
}

type Object struct {
	Key      string    `json:"key"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type Client struct {
	cfg  Config
	http *http.Client
	now  func() time.Time
}

func New(cfg Config, hc *http.Client) *Client {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{cfg: cfg, http: hc, now: time.Now}
}

// objectURL builds the URL for a key ("" = the bucket). AWS uses
// virtual-hosted URLs; other services use path-style, which all of them accept.
func (c *Client) objectURL(key string) (*url.URL, error) {
	var u *url.URL
	if c.cfg.Endpoint == "" {
		u = &url.URL{Scheme: "https", Host: fmt.Sprintf("%s.s3.%s.amazonaws.com", c.cfg.Bucket, c.cfg.Region), Path: "/" + key}
	} else {
		base, err := url.Parse(strings.TrimRight(c.cfg.Endpoint, "/"))
		if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
			return nil, fmt.Errorf("%q isn't a storage URL like https://s3.example.com", c.cfg.Endpoint)
		}
		u = &url.URL{Scheme: base.Scheme, Host: base.Host, Path: base.Path + "/" + c.cfg.Bucket + "/" + key}
	}
	return u, nil
}

// Error is an error response from the storage service.
type Error struct {
	Status  int
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func (e *Error) Error() string {
	switch {
	case e.Code == "NoSuchBucket":
		return "the bucket doesn't exist (check its name and region)"
	case e.Code == "InvalidAccessKeyId", e.Code == "SignatureDoesNotMatch", e.Status == http.StatusUnauthorized:
		return "the access key or secret key is wrong"
	case e.Code == "AccessDenied", e.Status == http.StatusForbidden:
		return "access denied: the key needs permission to list, read, write and delete in this bucket"
	case e.Code == "NoSuchKey", e.Status == http.StatusNotFound:
		return "not found"
	case e.Message != "":
		return e.Message
	case e.Code != "":
		return e.Code
	}
	return fmt.Sprintf("storage error %d", e.Status)
}

// IsNotFound reports whether err is a missing object or bucket.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Status == http.StatusNotFound || e.Code == "NoSuchKey")
}

func (c *Client) do(ctx context.Context, method, key string, query url.Values, body []byte) (*http.Response, error) {
	u, err := c.objectURL(key)
	if err != nil {
		return nil, err
	}
	u.RawQuery = canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body == nil {
		req.Body = http.NoBody
	}
	req.ContentLength = int64(len(body))
	sign(req, c.cfg, body, c.now().UTC())
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		e := &Error{Status: res.StatusCode}
		data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		_ = xml.Unmarshal(data, e)
		return nil, e
	}
	return res, nil
}

// List returns the objects under prefix (up to max), newest first.
func (c *Client) List(ctx context.Context, prefix string, max int) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		res, err := c.do(ctx, http.MethodGet, "", q, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Contents []struct {
				Key          string `xml:"Key"`
				Size         int64  `xml:"Size"`
				LastModified string `xml:"LastModified"`
			} `xml:"Contents"`
			IsTruncated           bool   `xml:"IsTruncated"`
			NextContinuationToken string `xml:"NextContinuationToken"`
		}
		err = xml.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("unexpected answer from the storage service: %w", err)
		}
		for _, o := range page.Contents {
			t, _ := time.Parse(time.RFC3339, o.LastModified)
			out = append(out, Object{Key: o.Key, Size: o.Size, Modified: t})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" || len(out) >= max {
			break
		}
		token = page.NextContinuationToken
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Key > out[j].Key
	})
	if len(out) > max {
		out = out[:max]
	}
	return out, nil
}

// Get streams an object into w.
func (c *Client) Get(ctx context.Context, key string, w io.Writer) (int64, error) {
	res, err := c.do(ctx, http.MethodGet, key, nil, nil)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	return io.Copy(w, res.Body)
}

// Put uploads a small object (kept in memory).
func (c *Client) Put(ctx context.Context, key string, body []byte) error {
	res, err := c.do(ctx, http.MethodPut, key, nil, body)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	res, err := c.do(ctx, http.MethodDelete, key, nil, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

// --- Signature Version 4 ---

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// uriEncode escapes everything but unreserved characters (RFC 3986), as S3 expects.
func uriEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case 'A' <= ch && ch <= 'Z', 'a' <= ch && ch <= 'z', '0' <= ch && ch <= '9', ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && keepSlash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, false)+"="+uriEncode(v, false))
		}
	}
	return strings.Join(parts, "&")
}

// sign adds the SigV4 Authorization header. Every header already on the
// request (plus host) is signed.
func sign(req *http.Request, cfg Config, body []byte, t time.Time) {
	payload := emptySHA256
	if len(body) > 0 {
		payload = hexSHA256(body)
	}
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payload)

	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		headers[strings.ToLower(k)] = strings.Join(v, ",")
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed := strings.Join(names, ";")

	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, uriEncode(path, true), req.URL.RawQuery, canonHeaders.String(), signed, payload}, "\n")
	scope := day + "/" + cfg.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))
	key := hmacSHA256([]byte("AWS4"+cfg.SecretKey), day)
	key = hmacSHA256(key, cfg.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", cfg.AccessKey, scope, signed, sig))
}
