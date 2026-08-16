package registry

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/auth"
	"github.com/imgoci/go/internal/retry"
	"github.com/imgoci/go/internal/transfer"
)

func TestGetDigestAndSize(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	want := digest.FromBytes(body)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/"+testRepo+"/manifests/"+testTag {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get(headerAccept); got != testAccept {
			t.Errorf("Accept = %q, want %q", got, testAccept)
		}
		if got := r.Header.Get(headerAcceptEncoding); got != codingIdentity {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		w.Header().Set(headerContentType, testAccept+"; charset=utf-8")
		w.Header().Set(headerDockerContentDigest, "sha256:deadbeef")
		_, _ = w.Write(body)
	}))
	t.Cleanup(registry.Close)

	client := mustClient(t, testConfig(t, registry))
	raw, contentType, err := client.Get(t.Context(), testTag, testAccept)
	requireNoError(t, err)
	if contentType != testAccept {
		t.Fatalf("content type = %q, want %q", contentType, testAccept)
	}
	if len(raw) != len(body) {
		t.Fatalf("size = %d, want %d", len(raw), len(body))
	}
	requireDigest(t, raw, want)
}

func TestGetGzippedManifestFails(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	var hits atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set(headerContentType, testAccept)
		w.Header().Set(headerContentEncoding, "gzip")
		_, _ = w.Write(gzipBytes(t, body))
	}))
	t.Cleanup(registry.Close)

	client := mustClient(t, testConfig(t, registry))
	_, _, err := client.Get(t.Context(), testTag, testAccept)
	var coding *contentCodingError
	if !errors.As(err, &coding) {
		t.Fatalf("err = %v, want contentCodingError", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (coded responses are terminal)", hits.Load())
	}
	if _, transient := retry.IsTransient(err); transient {
		t.Fatal("content-coding rejection must not be tagged transient")
	}
	if strings.Contains(err.Error(), "after") && strings.Contains(err.Error(), "attempts") {
		t.Fatalf("coded response was retried: %v", err)
	}
}

func TestGetCompressingTokenRealmSucceeds(t *testing.T) {
	t.Parallel()

	const token = "realm-token"
	body := []byte(testPayload)

	realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentEncoding, "gzip")
		w.Header().Set(headerContentType, "application/json")
		gz := gzip.NewWriter(w)
		if err := json.NewEncoder(gz).Encode(map[string]any{
			"token":      token,
			"expires_in": 120,
		}); err != nil {
			t.Errorf("encode token: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Errorf("close gzip: %v", err)
		}
	}))
	t.Cleanup(realm.Close)

	var hits atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") == "Bearer "+token {
			w.Header().Set(headerContentType, testAccept)
			_, _ = w.Write(body)

			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm.URL+
			`/token",service="fixture-registry",scope="repository:`+testRepo+`:pull"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	client := mustClient(t, testConfig(t, registry))
	raw, contentType, err := client.Get(t.Context(), testTag, testAccept)
	requireNoError(t, err)
	if contentType != testAccept {
		t.Fatalf("content type = %q, want %q", contentType, testAccept)
	}
	requireDigest(t, raw, digest.FromBytes(body))
	if hits.Load() < 2 {
		t.Fatalf("registry hits = %d, want 401 then retry", hits.Load())
	}
}

func TestGetSentinels(t *testing.T) {
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
						`/token",service="fixture",scope="repository:`+testRepo+`:pull"`)
				}
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(registry.Close)

			client := mustClient(t, testConfig(t, registry))
			_, _, err := client.Get(t.Context(), testTag, testAccept)
			requireErrorIs(t, err, tt.want)
		})
	}
}

func TestGetRetries503HonoringRetryAfter(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)
	var hits atomic.Int32
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set(headerRetryAfter, "1")
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}
		w.Header().Set(headerContentType, testAccept)
		_, _ = w.Write(body)
	}))
	t.Cleanup(registry.Close)

	var waits []time.Duration
	cfg := testConfig(t, registry)
	client, err := New(cfg)
	requireNoError(t, err)
	client.retry = instantPolicy(&waits)

	raw, _, err := client.Get(t.Context(), testTag, testAccept)
	requireNoError(t, err)
	requireDigest(t, raw, digest.FromBytes(body))
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
	if len(waits) != 1 {
		t.Fatalf("waits = %v, want one Retry-After floor", waits)
	}
	if waits[0] != time.Second {
		t.Fatalf("wait = %s, want 1s Retry-After floor", waits[0])
	}
}

func TestGetRedirectIdentityEnforced(t *testing.T) {
	t.Parallel()

	body := []byte(testPayload)

	t.Run("gzip-coded hop is rejected", func(t *testing.T) {
		t.Parallel()
		storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(headerAcceptEncoding) != codingIdentity {
				t.Errorf("Accept-Encoding = %q, want identity", r.Header.Get(headerAcceptEncoding))
			}
			w.Header().Set(headerContentType, testAccept)
			w.Header().Set(headerContentEncoding, "gzip")
			_, _ = w.Write(gzipBytes(t, body))
		}))
		t.Cleanup(storage.Close)

		registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, storage.URL+"/object", http.StatusFound)
		}))
		t.Cleanup(registry.Close)

		client := mustClient(t, testConfig(t, registry))
		_, _, err := client.Get(t.Context(), testTag, testAccept)
		var coding *contentCodingError
		if !errors.As(err, &coding) {
			t.Fatalf("err = %v, want contentCodingError", err)
		}
	})

	t.Run("identity-coded hop succeeds", func(t *testing.T) {
		t.Parallel()
		storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(headerAcceptEncoding) != codingIdentity {
				t.Errorf("Accept-Encoding = %q, want identity", r.Header.Get(headerAcceptEncoding))
			}
			w.Header().Set(headerContentType, testAccept)
			w.Header().Set(headerContentEncoding, codingIdentity)
			_, _ = w.Write(body)
		}))
		t.Cleanup(storage.Close)

		registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, storage.URL+"/object", http.StatusFound)
		}))
		t.Cleanup(registry.Close)

		client := mustClient(t, testConfig(t, registry))
		raw, contentType, err := client.Get(t.Context(), testTag, testAccept)
		requireNoError(t, err)
		if contentType != testAccept {
			t.Fatalf("content type = %q, want %q", contentType, testAccept)
		}
		requireDigest(t, raw, digest.FromBytes(body))
	})
}

func TestGetDialRefusedIsRetried(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoError(t, err)
	addr := ln.Addr().String()
	requireNoError(t, ln.Close())

	var waits []time.Duration
	client, err := New(Config{
		Host:       addr,
		Repository: testRepo,
		PlainHTTP:  true,
	})
	requireNoError(t, err)
	client.retry = instantPolicy(&waits)

	_, _, err = client.Get(t.Context(), testTag, testAccept)
	if err == nil {
		t.Fatal("expected dial error")
	}
	if len(waits) != 3 {
		t.Fatalf("waits = %d, want 3 (4 attempts)", len(waits))
	}
}

func TestGetAnonymousBasicIsUnauthorized(t *testing.T) {
	t.Parallel()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registry.Close)

	client := mustClient(t, testConfig(t, registry))
	_, _, err := client.Get(t.Context(), testTag, testAccept)
	requireErrorIs(t, err, transfer.ErrUnauthorized)
	requireErrorIs(t, err, auth.ErrAuth)
	if _, transient := retry.IsTransient(err); transient {
		t.Fatal("anonymous Basic must not be retried")
	}
}
