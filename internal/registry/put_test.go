package registry

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/transfer"
)

func TestPutSuccessStatuses(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	tests := []struct {
		name   string
		status int
	}{
		{name: "201 created", status: http.StatusCreated},
		{name: "200 ok", status: http.StatusOK},
		{name: "202 accepted", status: http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertPutRequest(t, r, body)
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(registry.Close)

			err := mustClient(t, testConfig(t, registry)).Put(t.Context(), testTag, testAccept, body)
			requireNoError(t, err)
		})
	}
}

// assertPutRequest checks the wire shape of one manifest PUT.
func assertPutRequest(t *testing.T, r *http.Request, body []byte) {
	t.Helper()
	if r.Method != http.MethodPut {
		t.Errorf("method = %s, want PUT", r.Method)
	}
	if r.URL.Path != "/v2/"+testRepo+"/manifests/"+testTag {
		t.Errorf("path = %q", r.URL.Path)
	}
	if got := r.Header.Get(headerContentType); got != testAccept {
		t.Errorf("Content-Type = %q, want %q", got, testAccept)
	}
	if got := r.Header.Get(headerAcceptEncoding); got != codingIdentity {
		t.Errorf("Accept-Encoding = %q, want identity", got)
	}
	got, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read body: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}
}

func TestPutSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "404 is not found", status: http.StatusNotFound, want: transfer.ErrNotFound},
		{name: "401 after bearer is unauthorized", status: http.StatusUnauthorized, want: transfer.ErrUnauthorized},
		{name: "403 is unauthorized", status: http.StatusForbidden, want: transfer.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.Header().Set(headerContentType, "application/json")
					_, _ = io.WriteString(w, `{"token":"t","expires_in":120}`)

					return
				}
				if tt.status == http.StatusUnauthorized && r.Header.Get("Authorization") == "" {
					w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+
						`/token",service="fixture",scope="repository:`+testRepo+`:push"`)
				}
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(registry.Close)

			err := mustClient(t, testConfig(t, registry)).Put(t.Context(), testTag, testAccept, []byte(testPayload))
			requireErrorIs(t, err, tt.want)
		})
	}
}

func TestPutRetries503HonoringRetryAfter(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	var (
		hits   atomic.Int32
		bodies [][]byte
	)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		bodies = append(bodies, got)
		if hits.Add(1) == 1 {
			w.Header().Set(headerRetryAfter, "1")
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(registry.Close)

	var waits []time.Duration
	cfg := testConfig(t, registry)
	client, err := New(cfg)
	requireNoError(t, err)
	client.retry = instantPolicy(&waits)

	err = client.Put(t.Context(), testTag, testAccept, body)
	requireNoError(t, err)
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
	if len(waits) != 1 {
		t.Fatalf("waits = %v, want one Retry-After floor", waits)
	}
	if waits[0] != time.Second {
		t.Fatalf("wait = %s, want 1s Retry-After floor", waits[0])
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies = %d, want 2", len(bodies))
	}
	for i, got := range bodies {
		if !bytes.Equal(got, body) {
			t.Fatalf("attempt %d body = %q, want %q", i+1, got, body)
		}
	}
}

func TestPutDigestAndTagURLForms(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	dgst := digest.FromBytes(body)
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "tag", ref: testTag, want: "/v2/" + testRepo + "/manifests/" + testTag},
		{name: "digest", ref: dgst.String(), want: "/v2/" + testRepo + "/manifests/" + dgst.String()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.want {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.want)
				}
				if got := r.Header.Get(headerContentType); got != testAccept {
					t.Errorf("Content-Type = %q, want %q", got, testAccept)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(registry.Close)

			err := mustClient(t, testConfig(t, registry)).Put(t.Context(), tt.ref, testAccept, body)
			requireNoError(t, err)
		})
	}
}
