package registry

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/retry"
)

const (
	testRepo    = "os/example"
	testAccept  = "application/vnd.oci.image.index.v1+json"
	testTag     = "latest"
	testPayload = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json"}`
)

// mustClient builds a [Client] for cfg with a retry policy that never sleeps.
func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client.retry = instantPolicy(nil)

	return client
}

// instantPolicy is a four-attempt policy whose Sleep records waits in dest
// (when dest is non-nil) and never blocks. Rand always draws zero so a
// Retry-After floor is the wait the test observes.
func instantPolicy(dest *[]time.Duration) retry.Policy {
	return retry.Policy{
		Attempts: 4,
		Base:     time.Nanosecond,
		Cap:      time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error {
			if dest != nil {
				*dest = append(*dest, d)
			}

			return ctx.Err()
		},
		Rand: func(int64) int64 { return 0 },
	}
}

// testConfig binds an httptest registry as a PlainHTTP host.
func testConfig(t *testing.T, srv *httptest.Server) Config {
	t.Helper()

	return Config{
		Host:       hostOf(t, srv.URL),
		Repository: testRepo,
		PlainHTTP:  true,
		HTTPClient: srv.Client(),
	}
}

// hostOf returns the host:port of an absolute URL.
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u.Host
}

// gzipBytes compresses p as a single gzip member.
func gzipBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// closeProbe records whether Close was called.
type closeProbe struct {
	closed bool
}

// Read is a closed stream.
func (*closeProbe) Read([]byte) (int, error) { return 0, io.EOF }

// Close records that the identity wrapper released the body.
func (c *closeProbe) Close() error {
	c.closed = true

	return nil
}

// roundTripFunc is an [http.RoundTripper] backed by a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls the function.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// okResponse is a 200 with the given encoding header and body.
func okResponse(req *http.Request, encoding string, body io.ReadCloser) *http.Response {
	header := make(http.Header)
	if encoding != "" {
		header.Set(headerContentEncoding, encoding)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       body,
		Request:    req,
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v, want %v", err, target)
	}
}

func requireDigest(t *testing.T, raw []byte, want digest.Digest) {
	t.Helper()
	got := digest.FromBytes(raw)
	if got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}
