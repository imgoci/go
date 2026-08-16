package registry

import (
	"bytes"
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
	"github.com/imgoci/go/internal/transfer"
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

func TestBlobsPushVerifiesStreamedBytes(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-blob-bytes")
	dgst := digest.FromBytes(payload)
	mutated := []byte("stored-blob-XXXXX")
	if len(mutated) != len(payload) {
		t.Fatalf("mutated length %d != payload length %d", len(mutated), len(payload))
	}

	tests := []struct {
		name    string
		size    int64
		reader  func() io.Reader
		wantErr bool
	}{
		{
			name:   "matching bytes succeed",
			size:   int64(len(payload)),
			reader: func() io.Reader { return bytes.NewReader(payload) },
		},
		{
			name:    "same-length mutation diverges",
			size:    int64(len(payload)),
			reader:  func() io.Reader { return bytes.NewReader(mutated) },
			wantErr: true,
		},
		{
			name:    "short count diverges",
			size:    int64(len(payload)),
			reader:  func() io.Reader { return io.LimitReader(bytes.NewReader(payload), int64(len(payload)-1)) },
			wantErr: true,
		},
		{
			name: "long count diverges",
			size: int64(len(payload)),
			reader: func() io.Reader {
				return io.MultiReader(bytes.NewReader(payload), bytes.NewReader([]byte("x")))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := blobUploadServer(t)
			err := mustClient(t, testConfig(t, registry)).Blobs().Push(
				t.Context(), dgst, tt.size, tt.reader(),
			)
			if !tt.wantErr {
				requireNoError(t, err)
				return
			}
			requireErrorIs(t, err, transfer.ErrDigestMismatch)
			if !strings.Contains(err.Error(), "bytes streamed diverged from pass-1 digest") {
				t.Fatalf("error = %v, want source-mutation wording", err)
			}
		})
	}
}

// blobUploadServer is a one-shot OCI upload session: POST opens it with a
// relative Location so go-oci-blob does not treat the commit as a write
// redirect; PUT commits after draining the body; DELETE is best-effort
// session cleanup.
func blobUploadServer(t *testing.T) *httptest.Server {
	t.Helper()
	const sessionPath = "/v2/" + testRepo + "/blobs/uploads/session"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/"+testRepo+"/blobs/uploads/":
			w.Header().Set("Location", sessionPath)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == sessionPath:
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && r.URL.Path == sessionPath:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVerifyingReaderSeekRewindAndReject(t *testing.T) {
	t.Parallel()
	payload := []byte("stored-blob-bytes")
	dgst := digest.FromBytes(payload)
	wrapped := newVerifyingReader(bytes.NewReader(payload), dgst, int64(len(payload)))
	seeker, ok := wrapped.(io.Seeker)
	if !ok {
		t.Fatal("seekable source must produce an io.Seeker wrapper")
	}
	buf := make([]byte, 4)
	n, err := wrapped.Read(buf)
	if err != nil || n != 4 {
		t.Fatalf("Read = %d, %v", n, err)
	}
	_, err = seeker.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("rewind: %v", err)
	}
	got, err := io.ReadAll(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("after rewind got %q, want %q", got, payload)
	}
	if _, err := seeker.Seek(1, io.SeekStart); err == nil {
		t.Fatal("non-zero Seek must fail")
	}

	src := io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
	limited := newVerifyingReader(src, dgst, int64(len(payload)))
	if _, ok := limited.(io.Seeker); ok {
		t.Fatal("non-seekable source must not implement Seek")
	}
}

func TestBlobsPushCommit401Reauthenticates(t *testing.T) {
	t.Parallel()
	payload := []byte("stored-blob-bytes")
	dgst := digest.FromBytes(payload)
	puts := blobCommitChallengeServer(t)
	err := mustClient(t, testConfig(t, puts.srv)).Blobs().Push(
		t.Context(), dgst, int64(len(payload)), bytes.NewReader(payload),
	)
	requireNoError(t, err)
	if puts.n.Load() < 2 {
		t.Fatalf("commit PUTs = %d, want at least 2 (401 then retry)", puts.n.Load())
	}
}

func TestBlobsPushReplayStillDetectsMutation(t *testing.T) {
	t.Parallel()
	payload := []byte("stored-blob-bytes")
	mutated := []byte("stored-blob-XXXXX")
	if len(mutated) != len(payload) {
		t.Fatalf("mutated length %d != payload length %d", len(mutated), len(payload))
	}
	dgst := digest.FromBytes(payload)
	puts := blobCommitChallengeServer(t)
	err := mustClient(t, testConfig(t, puts.srv)).Blobs().Push(
		t.Context(), dgst, int64(len(payload)), &rewindMutator{first: payload, next: mutated},
	)
	requireErrorIs(t, err, transfer.ErrDigestMismatch)
	if !strings.Contains(err.Error(), "bytes streamed diverged from pass-1 digest") {
		t.Fatalf("error = %v, want source-mutation wording", err)
	}
}

// commitPuts is a blob upload server that 401s the first unauthenticated
// commit PUT with a bearer challenge, then accepts a replay with a token.
type commitPuts struct {
	srv *httptest.Server
	n   atomic.Int32
}

func blobCommitChallengeServer(t *testing.T) *commitPuts {
	t.Helper()
	const sessionPath = "/v2/" + testRepo + "/blobs/uploads/session"
	out := &commitPuts{}
	out.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set(headerContentType, "application/json")
			_, _ = io.WriteString(w, `{"token":"t","expires_in":120}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/"+testRepo+"/blobs/uploads/":
			w.Header().Set("Location", sessionPath)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == sessionPath:
			_, _ = io.Copy(io.Discard, r.Body)
			out.n.Add(1)
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+
					`/token",service="fixture",scope="repository:`+testRepo+`:push"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && r.URL.Path == sessionPath:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(out.srv.Close)
	return out
}

// rewindMutator yields first until a rewind to offset 0, then next.
type rewindMutator struct {
	first   []byte
	next    []byte
	pos     int64
	rewound bool
}

func (r *rewindMutator) Read(p []byte) (int, error) {
	src := r.first
	if r.rewound {
		src = r.next
	}
	if r.pos >= int64(len(src)) {
		return 0, io.EOF
	}
	n := copy(p, src[r.pos:])
	r.pos += int64(n)
	return n, nil
}

func (r *rewindMutator) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = int64(len(r.first)) + offset
	default:
		return 0, errors.New("invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("negative position")
	}
	if abs == 0 && r.pos != 0 {
		r.rewound = true
	}
	r.pos = abs
	return abs, nil
}
