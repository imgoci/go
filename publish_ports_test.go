package imgoci

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/transfer"
)

// TestPublishTwoMemberGzipWritesNothing fails at pass 1 with ErrDecode and
// writes nothing to the registry. A two-member gzip is valid stored bytes a
// conforming producer still refuses; the hit-counting stub proves the refusal
// happens before Exists, Push, or Put.
func TestPublishTwoMemberGzipWritesNothing(t *testing.T) {
	t.Parallel()
	ports := &countingPorts{}
	var constructed int
	client := clientWithTransferPorts(t, ports, ports, &constructed)
	_, err := client.Publish(t.Context(), "example.com/ports/strict:v1", gzipTwoMemberSpec(t))
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
	if constructed != 1 {
		t.Fatalf("adapter constructions = %d, want 1", constructed)
	}
	if n := ports.n(); n != 0 {
		t.Fatalf("registry writes = %d, want 0", n)
	}
}

// TestPublishIndexPutFailureLeavesNoTag lands every blob and file manifest,
// then fails the index PUT so the tag does not resolve. Fetch of the tag is
// ErrNotFound: there is no broken artifact.
func TestPublishIndexPutFailureLeavesNoTag(t *testing.T) {
	t.Parallel()
	reg := &interruptRegistry{}
	client := clientWithTransferPorts(t, reg, reg, nil)
	_, err := client.Publish(
		t.Context(),
		"example.com/ports/interrupt:v1",
		validReleaseSpec(t, []byte("payload")),
	)
	if err == nil {
		t.Fatal("expected index put failure")
	}

	pushed, putRefs := reg.snapshot()
	if len(pushed) == 0 {
		t.Fatal("blob did not land before index put failure")
	}
	digestPuts := 0
	tagPuts := 0
	for _, ref := range putRefs {
		if strings.HasPrefix(ref, "sha256:") {
			digestPuts++
			continue
		}
		tagPuts++
	}
	if digestPuts == 0 {
		t.Fatal("file manifest did not land before index put failure")
	}
	if tagPuts == 0 {
		t.Fatal("index put was not attempted")
	}

	_, err = client.Fetch(t.Context(), "example.com/ports/interrupt:v1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// countingPorts is a transfer port pair that counts every registry call and
// stores nothing, so a test can assert that no traffic happened at all.
type countingPorts struct {
	mu   sync.Mutex
	hits int
}

// n returns the number of registry calls seen so far.
func (c *countingPorts) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// hit records one registry call.
func (c *countingPorts) hit() {
	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
}

// Get counts the call and reports the manifest as missing.
func (c *countingPorts) Get(context.Context, string, string) ([]byte, string, error) {
	c.hit()
	return nil, "", transfer.ErrNotFound
}

// Put counts the call and accepts the manifest.
func (c *countingPorts) Put(context.Context, string, string, []byte) error {
	c.hit()
	return nil
}

// Exists counts the call and reports the blob as absent.
func (c *countingPorts) Exists(context.Context, digest.Digest) (bool, error) {
	c.hit()
	return false, nil
}

// Push counts the call and discards the blob.
func (c *countingPorts) Push(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
	c.hit()
	_, _ = io.Copy(io.Discard, r)
	return nil
}

// Pull counts the call and reports the blob as missing.
func (c *countingPorts) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	c.hit()
	return nil, transfer.ErrNotFound
}

// interruptRegistry accepts every blob and digest-addressed manifest but
// refuses the tag PUT, which is the last write a publish performs.
type interruptRegistry struct {
	mu      sync.Mutex
	pushed  []digest.Digest
	putRefs []string
	// stored holds bytes from successful Puts, keyed by ref.
	stored map[string][]byte
}

// snapshot returns copies of the pushed blob digests and the attempted PUT
// references.
func (s *interruptRegistry) snapshot() ([]digest.Digest, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushed := make([]digest.Digest, len(s.pushed))
	copy(pushed, s.pushed)
	putRefs := make([]string, len(s.putRefs))
	copy(putRefs, s.putRefs)
	return pushed, putRefs
}

// Get returns a previously stored manifest, or reports it as missing.
func (s *interruptRegistry) Get(_ context.Context, ref, _ string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.stored[ref]
	if !ok {
		return nil, "", transfer.ErrNotFound
	}
	return bytes.Clone(raw), index.MediaTypeIndex, nil
}

// Put records the reference, storing digest-addressed manifests and refusing
// anything addressed by tag.
func (s *interruptRegistry) Put(_ context.Context, ref, _ string, raw []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putRefs = append(s.putRefs, ref)
	if !strings.HasPrefix(ref, "sha256:") {
		return errors.New("index put refused")
	}
	if s.stored == nil {
		s.stored = make(map[string][]byte)
	}
	s.stored[ref] = bytes.Clone(raw)
	return nil
}

// Exists always reports the blob as absent so every blob is pushed.
func (s *interruptRegistry) Exists(context.Context, digest.Digest) (bool, error) {
	return false, nil
}

// Push records the blob digest and discards the bytes.
func (s *interruptRegistry) Push(_ context.Context, dgst digest.Digest, _ int64, r io.Reader) error {
	_, _ = io.Copy(io.Discard, r)
	s.mu.Lock()
	s.pushed = append(s.pushed, dgst)
	s.mu.Unlock()
	return nil
}

// Pull always reports the blob as missing.
func (s *interruptRegistry) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	return nil, transfer.ErrNotFound
}
