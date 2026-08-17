package imgoci

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/adapters"
	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/transfer"
)

// stubManifests is a test Manifests that returns a fixed body or error.
type stubManifests struct {
	// raw is the index body Get returns.
	raw []byte
	// contentType is the Content-Type Get reports.
	contentType string
	// err is returned from Get when set.
	err error
	// refs records every Get ref.
	refs []string
}

// Get returns the stub body or err and records ref.
func (s *stubManifests) Get(_ context.Context, ref, _ string) ([]byte, string, error) {
	s.refs = append(s.refs, ref)
	if s.err != nil {
		return nil, "", s.err
	}

	return s.raw, s.contentType, nil
}

// Put is unused on the fetch path.
func (s *stubManifests) Put(context.Context, string, string, []byte) error {
	return errors.New("put not implemented")
}

// stubBlobs is a test Blobs that is never called on the Fetch path.
type stubBlobs struct{}

// Exists is unused on the fetch path.
func (stubBlobs) Exists(context.Context, digest.Digest) (bool, error) {
	return false, errors.New("exists not implemented")
}

// Push is unused on the fetch path.
func (stubBlobs) Push(context.Context, digest.Digest, int64, io.Reader) error {
	return errors.New("push not implemented")
}

// Pull is unused on the fetch path.
func (stubBlobs) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	return nil, errors.New("pull not implemented")
}

func mustCanonicalIndex(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/canonical/pass/minimal.json")
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func clientWithPorts(t *testing.T, manifests transfer.Manifests, constructed *int) *Client {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.pool = adapters.NewPool(func(context.Context, string, string, adapters.Config) (adapters.Ports, error) {
		if constructed != nil {
			*constructed++
		}

		return adapters.Ports{Manifests: manifests, Blobs: stubBlobs{}}, nil
	})

	return c
}

func TestFetchAgainstStubPorts(t *testing.T) {
	t.Parallel()
	raw := mustCanonicalIndex(t)
	manifests := &stubManifests{raw: raw, contentType: index.MediaTypeIndex}
	var constructed int
	c := clientWithPorts(t, manifests, &constructed)

	rel, err := c.Fetch(t.Context(), "example.com/os/example:v1")
	if err != nil {
		t.Fatal(err)
	}
	if constructed != 1 {
		t.Fatalf("adapter constructions = %d, want 1", constructed)
	}
	if len(manifests.refs) != 1 || manifests.refs[0] != "v1" {
		t.Fatalf("Get refs = %v, want [v1]", manifests.refs)
	}
	if rel.Index().Name() != "example" {
		t.Fatalf("name = %q", rel.Index().Name())
	}
	if rel.Digest() != rel.Index().Digest() {
		t.Fatal("Release.Digest must equal Index.Digest")
	}
	if rel.host != testHost || rel.repository != testRepository {
		t.Fatalf("origin = %s/%s", rel.host, rel.repository)
	}
}

func TestFetchDigestPinAndAddressing(t *testing.T) {
	t.Parallel()
	raw := mustCanonicalIndex(t)
	pinned := digest.FromBytes(raw)
	manifests := &stubManifests{raw: raw, contentType: index.MediaTypeIndex}
	c := clientWithPorts(t, manifests, nil)

	ref := Reference("example.com/os/example:v1@" + pinned.String())
	rel, err := c.Fetch(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests.refs) != 1 || manifests.refs[0] != pinned.String() {
		t.Fatalf("digest ref must win over tag, got %v", manifests.refs)
	}
	if rel.Digest() != pinned {
		t.Fatalf("digest = %s, want %s", rel.Digest(), pinned)
	}
}

func TestFetchNameOnlyDoesNotConstructAdapter(t *testing.T) {
	t.Parallel()
	var constructed int
	c := clientWithPorts(t, &stubManifests{}, &constructed)
	_, err := c.Fetch(t.Context(), "example.com/os/example")
	if err == nil {
		t.Fatal("name-only fetch must fail")
	}
	if errors.Is(err, ErrInvalidSpec) || errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("name-only must not wrap a public sentinel: %v", err)
	}
	if constructed != 0 {
		t.Fatal("adapter must not be constructed for a name-only reference")
	}
}

func TestFetchMapsNotFound(t *testing.T) {
	t.Parallel()
	manifests := &stubManifests{err: transfer.ErrNotFound}
	c := clientWithPorts(t, manifests, nil)
	_, err := c.Fetch(t.Context(), "example.com/os/example:v1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFetchWrongContentType(t *testing.T) {
	t.Parallel()
	manifests := &stubManifests{raw: mustCanonicalIndex(t), contentType: "application/json"}
	c := clientWithPorts(t, manifests, nil)
	_, err := c.Fetch(t.Context(), "example.com/os/example:v1")
	if !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("err = %v, want ErrInvalidIndex", err)
	}
}

func TestClientResolveDefaultsCapabilities(t *testing.T) {
	t.Parallel()
	idx := testIndex(testEntry("amd64", "x-test-target", "x-test-format", "x-test-file", "none", standardFileMediaType))
	rel := &Release{digest: idx.Digest(), index: idx, host: testHost, repository: testRepository}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	sel, err := c.Resolve(rel, ResolveQuery{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Compressions:   []string{"none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sel.IndexDigest() != rel.Digest() {
		t.Fatal("resolved digest must bind to the release")
	}
}
