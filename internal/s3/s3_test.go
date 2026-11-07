package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The "GET Object" example from the AWS Signature Version 4 documentation.
func TestSignMatchesAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	cfg := Config{Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	sign(req, cfg, nil, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization:\n%s\nwant:\n%s", got, want)
	}
}

func TestURIEncode(t *testing.T) {
	if got := uriEncode("/td/a b+c~ü.gz", true); got != "/td/a%20b%2Bc~%C3%BC.gz" {
		t.Error(got)
	}
	if got := uriEncode("a/b", false); got != "a%2Fb" {
		t.Error(got)
	}
}

// fakeS3 is an in-memory, path-style bucket that checks every request is signed.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	pages   int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=key/") {
		w.WriteHeader(403)
		fmt.Fprint(w, `<Error><Code>SignatureDoesNotMatch</Code></Error>`)
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if bucket != "backups" {
		w.WriteHeader(404)
		fmt.Fprint(w, `<Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message></Error>`)
		return
	}
	switch {
	case r.Method == http.MethodGet && key == "":
		f.pages++
		prefix := r.URL.Query().Get("prefix")
		// Two objects per page to exercise continuation.
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sortStrings(keys)
		start := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			fmt.Sscan(tok, &start)
		}
		end := min(start+2, len(keys))
		fmt.Fprint(w, `<ListBucketResult>`)
		for i, k := range keys[start:end] {
			fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>%d</Size><LastModified>2026-11-0%dT10:00:00.000Z</LastModified></Contents>`, k, len(f.objects[k]), start+i+1)
		}
		if end < len(keys) {
			fmt.Fprintf(w, `<IsTruncated>true</IsTruncated><NextContinuationToken>%d</NextContinuationToken>`, end)
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	case r.Method == http.MethodGet:
		b, ok := f.objects[key]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		w.Write(b)
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[key] = b
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(204)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestClientAgainstFakeBucket(t *testing.T) {
	f := &fakeS3{objects: map[string][]byte{
		"td/s1/db/shop-20261101-000000.sql.gz": []byte("one"),
		"td/s1/db/shop-20261102-000000.sql.gz": []byte("two!"),
		"td/s1/db/shop-20261103-000000.sql.gz": []byte("three"),
		"td/s2/db/other.sql.gz":                []byte("x"),
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(Config{Endpoint: srv.URL, Bucket: "backups", AccessKey: "key", SecretKey: "secret"}, srv.Client())
	ctx := context.Background()

	list, err := c.List(ctx, "td/s1/", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || f.pages != 2 || list[0].Key != "td/s1/db/shop-20261103-000000.sql.gz" || list[0].Size != 5 {
		t.Fatalf("list (newest first, both pages): %+v pages=%d", list, f.pages)
	}

	var buf bytes.Buffer
	if n, err := c.Get(ctx, list[1].Key, &buf); err != nil || n != 4 || buf.String() != "two!" {
		t.Fatalf("get: %d %q %v", n, buf.String(), err)
	}
	if err := c.Put(ctx, "td/probe", []byte("hi")); err != nil || string(f.objects["td/probe"]) != "hi" {
		t.Fatalf("put: %v", err)
	}
	if err := c.Delete(ctx, "td/probe"); err != nil || f.objects["td/probe"] != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.Get(ctx, "td/missing", io.Discard); !IsNotFound(err) {
		t.Errorf("missing object: %v", err)
	}

	bad := New(Config{Endpoint: srv.URL, Bucket: "nope", AccessKey: "key", SecretKey: "s"}, srv.Client())
	if _, err := bad.List(ctx, "", 10); err == nil || !strings.Contains(err.Error(), "bucket doesn't exist") {
		t.Errorf("wrong bucket: %v", err)
	}
	wrongKey := New(Config{Endpoint: srv.URL, Bucket: "backups", AccessKey: "other", SecretKey: "s"}, srv.Client())
	if _, err := wrongKey.List(ctx, "", 10); err == nil || !strings.Contains(err.Error(), "access key or secret key is wrong") {
		t.Errorf("wrong key: %v", err)
	}
}

func TestURLs(t *testing.T) {
	aws := New(Config{Region: "eu-west-1", Bucket: "b"}, nil)
	if u, _ := aws.objectURL("td/x.gz"); u.String() != "https://b.s3.eu-west-1.amazonaws.com/td/x.gz" {
		t.Error(u)
	}
	r2 := New(Config{Endpoint: "https://acc.r2.cloudflarestorage.com/", Region: "auto", Bucket: "b"}, nil)
	if u, _ := r2.objectURL("td/x.gz"); u.String() != "https://acc.r2.cloudflarestorage.com/b/td/x.gz" {
		t.Error(u)
	}
	if _, err := New(Config{Endpoint: "s3.example.com", Bucket: "b"}, nil).objectURL(""); err == nil {
		t.Error("an endpoint without https:// should be rejected")
	}
}
