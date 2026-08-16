package registry

import (
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

	"github.com/imgoci/go/internal/retry"
)

func TestBlobsStorageTransportRejectsGzip(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-blob-bytes")
	dgst := digest.FromBytes(payload)
	var hits atomic.Int32

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get(headerAcceptEncoding) != codingIdentity {
			t.Errorf("storage Accept-Encoding = %q, want identity", r.Header.Get(headerAcceptEncoding))
		}
		w.Header().Set(headerContentEncoding, "gzip")
		_, _ = w.Write(gzipBytes(t, payload))
	}))
	t.Cleanup(storage.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/v2/" + testRepo + "/blobs/" + dgst.String()
		if r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		http.Redirect(w, r, storage.URL+"/object/"+dgst.String(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(registry.Close)

	client := mustClient(t, testConfig(t, registry))
	rc, err := client.Blobs().Pull(t.Context(), dgst)
	if err == nil {
		t.Cleanup(func() { _ = rc.Close() })
		_, err = io.ReadAll(rc)
	}
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

func TestBlobsOpaqueAuthorizedStillEnforcesIdentity(t *testing.T) {
	t.Parallel()

	payload := []byte("tls-blob-bytes")
	dgst := digest.FromBytes(payload)

	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentEncoding, "gzip")
		_, _ = w.Write(gzipBytes(t, payload))
	}))
	t.Cleanup(storage.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/object/"+dgst.String(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(registry.Close)

	inner := storage.Client().Transport
	opaque := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return inner.RoundTrip(req)
	})
	cfg := Config{
		Host:                        hostOf(t, registry.URL),
		Repository:                  testRepo,
		PlainHTTP:                   true,
		HTTPClient:                  &http.Client{Transport: opaque},
		UnverifiedExternalTransport: true,
	}
	client := mustClient(t, cfg)
	rc, err := client.Blobs().Pull(t.Context(), dgst)
	if err == nil {
		t.Cleanup(func() { _ = rc.Close() })
		_, err = io.ReadAll(rc)
	}
	var coding *contentCodingError
	if !errors.As(err, &coding) {
		t.Fatalf("authorized opaque storage still has to refuse gzip: err = %v", err)
	}
}

func TestBlobsConcreteTLSSucceedsOnIdentityStorage(t *testing.T) {
	t.Parallel()

	payload := []byte("tls-blob-ok")
	dgst := digest.FromBytes(payload)

	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(storage.Close)

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/object/"+dgst.String(), http.StatusTemporaryRedirect)
	}))
	t.Cleanup(registry.Close)

	untrusted := Config{
		Host:       hostOf(t, registry.URL),
		Repository: testRepo,
		PlainHTTP:  true,
	}
	if _, err := mustClient(t, untrusted).Blobs().Pull(t.Context(), dgst); err == nil {
		t.Fatal("expected TLS verification failure with the default transport")
	}

	cfg := Config{
		Host:       hostOf(t, registry.URL),
		Repository: testRepo,
		PlainHTTP:  true,
		HTTPClient: storage.Client(),
	}
	client := mustClient(t, cfg)
	rc, err := client.Blobs().Pull(t.Context(), dgst)
	requireNoError(t, err)
	t.Cleanup(func() { _ = rc.Close() })
	got, err := io.ReadAll(rc)
	requireNoError(t, err)
	if string(got) != string(payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

func TestBlobsDialRefusedIsRetried(t *testing.T) {
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

	_, err = client.Blobs().Exists(t.Context(), digest.FromBytes([]byte("missing")))
	if err == nil {
		t.Fatal("expected dial error")
	}
	if len(waits) != 3 {
		t.Fatalf("waits = %d, want 3 (4 attempts)", len(waits))
	}
}

func TestBlobsExistsAbsentIsFalse(t *testing.T) {
	t.Parallel()

	dgst := digest.FromBytes([]byte("missing"))
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(registry.Close)

	ok, err := mustClient(t, testConfig(t, registry)).Blobs().Exists(t.Context(), dgst)
	requireNoError(t, err)
	if ok {
		t.Fatal("missing blob reported present")
	}
}
