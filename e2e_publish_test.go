//go:build e2e

package imgoci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/transfer"
)

// TestE2EPublishTwoMemberGzip fails at pass 1 with ErrDecode and writes
// nothing to the registry. A two-member gzip is valid stored bytes a
// conforming producer still refuses; the hit-counting stub proves the
// refusal happens before Exists, Push, or Put.
func TestE2EPublishTwoMemberGzip(t *testing.T) {
	t.Parallel()
	ports := &countingPorts{}
	var constructed int
	client := clientWithTransferPorts(t, ports, ports, &constructed)
	_, err := client.Publish(t.Context(), "example.com/e2e/strict:v1", gzipTwoMemberSpec(t))
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

// TestE2EPublishNonCanonicalIndexRejected fails Fetch with ErrInvalidIndex
// when the tag names pretty-printed index bytes.
//
// Publish cannot emit this fixture: it always RFC 8785-encodes the index,
// so a conforming producer cannot store indented JSON at the tag.
func TestE2EPublishNonCanonicalIndexRejected(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(
		t.Context(),
		tagRef(host, repo),
		validReleaseSpec(t, []byte("noncanonical\n")),
	); err != nil {
		t.Fatal(err)
	}
	canonical := getIndexRaw(t, host, repo, e2eTag, e2eCreds{})
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, canonical, "", "  "); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pretty.Bytes(), canonical) {
		t.Fatal("indented index matched canonical bytes")
	}
	seedManifest(t, host, repo, e2eTag, index.MediaTypeIndex, pretty.Bytes(), e2eCreds{})

	_, err := client.Fetch(t.Context(), tagRef(host, repo))
	if !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("err = %v, want ErrInvalidIndex", err)
	}
}

// TestE2EPublishWrongSizeDescriptorRejected fails FetchFiles when the index
// entry declares a file-manifest size that is not the retrieved byte length.
//
// Publish cannot emit this fixture: it records the true file-manifest size
// produced by filemanifest.BuildStandard.
func TestE2EPublishWrongSizeDescriptorRejected(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	client := newE2EClient(t, e2eCreds{})
	spec := validReleaseSpec(t, []byte("wrong-size-payload\n"))
	if _, err := client.Publish(t.Context(), tagRef(host, repo), spec); err != nil {
		t.Fatal(err)
	}

	rel := mustFetch(t, client, tagRef(host, repo))
	entries := rel.Index().Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	model := []index.ModelEntry{{
		Digest:        entries[0].Digest,
		Size:          entries[0].Size + 1,
		Selector:      toIndexSelector(entries[0].Selector),
		ContentDigest: entries[0].ContentDigest,
		ContentSize:   entries[0].ContentSize,
		Filename:      entries[0].Filename,
	}}
	mutated, err := index.Build(&index.Model{
		Name:    rel.Index().Name(),
		Version: rel.Index().Version(),
		Entries: model,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedManifest(t, host, repo, e2eTag, index.MediaTypeIndex, mutated, e2eCreds{})

	rel = mustFetch(t, client, tagRef(host, repo))
	sel := mustResolve(t, client, rel, ResolveQuery{
		Architecture:   entries[0].Selector.Architecture,
		Target:         entries[0].Selector.Target,
		Representation: entries[0].Selector.Representation,
		Compressions:   []string{entries[0].Selector.Compression},
	})
	err = client.FetchFiles(t.Context(), rel, sel, ToDir(t.TempDir()))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
}

// TestE2EPublishIndexPutFailureLeavesNoTag lands every blob and file
// manifest, then fails the index PUT so the tag does not resolve. Fetch of
// the tag is ErrNotFound: there is no broken artifact.
func TestE2EPublishIndexPutFailureLeavesNoTag(t *testing.T) {
	t.Parallel()
	reg := &interruptRegistry{}
	client := clientWithTransferPorts(t, reg, reg, nil)
	_, err := client.Publish(t.Context(), "example.com/e2e/interrupt:v1", validReleaseSpec(t, []byte("payload")))
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

	_, err = client.Fetch(t.Context(), "example.com/e2e/interrupt:v1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestE2EPublishDigestRefIsTagOnly rejects a digest-only Publish reference
// with ErrInvalidSpec before any registry traffic.
func TestE2EPublishDigestRefIsTagOnly(t *testing.T) {
	t.Parallel()
	hits := &hitTransport{}
	client, err := New(WithHTTPClient(&http.Client{Transport: hits}), WithPlainHTTP())
	if err != nil {
		t.Fatal(err)
	}
	pin := digest.FromBytes([]byte("x"))
	_, err = client.Publish(
		t.Context(),
		Reference("example.com/e2e/tagonly@"+pin.String()),
		validReleaseSpec(t, []byte("payload")),
	)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if n := hits.n.Load(); n != 0 {
		t.Fatalf("registry traffic = %d, want 0", n)
	}
}

type hitTransport struct {
	n atomic.Int64
}

func (h *hitTransport) RoundTrip(*http.Request) (*http.Response, error) {
	h.n.Add(1)
	return nil, errors.New("unexpected registry traffic")
}

type countingPorts struct {
	mu   sync.Mutex
	hits int
}

func (c *countingPorts) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func (c *countingPorts) hit() {
	c.mu.Lock()
	c.hits++
	c.mu.Unlock()
}

func (c *countingPorts) Get(context.Context, string, string) ([]byte, string, error) {
	c.hit()
	return nil, "", transfer.ErrNotFound
}

func (c *countingPorts) Put(context.Context, string, string, []byte) error {
	c.hit()
	return nil
}

func (c *countingPorts) Exists(context.Context, digest.Digest) (bool, error) {
	c.hit()
	return false, nil
}

func (c *countingPorts) Push(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
	c.hit()
	_, _ = io.Copy(io.Discard, r)
	return nil
}

func (c *countingPorts) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	c.hit()
	return nil, transfer.ErrNotFound
}

type interruptRegistry struct {
	mu      sync.Mutex
	pushed  []digest.Digest
	putRefs []string
	// stored holds bytes from successful Puts, keyed by ref.
	stored map[string][]byte
}

func (s *interruptRegistry) snapshot() ([]digest.Digest, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushed := make([]digest.Digest, len(s.pushed))
	copy(pushed, s.pushed)
	putRefs := make([]string, len(s.putRefs))
	copy(putRefs, s.putRefs)
	return pushed, putRefs
}

func (s *interruptRegistry) Get(_ context.Context, ref, _ string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, ok := s.stored[ref]
	if !ok {
		return nil, "", transfer.ErrNotFound
	}
	return bytes.Clone(raw), index.MediaTypeIndex, nil
}

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

func (s *interruptRegistry) Exists(context.Context, digest.Digest) (bool, error) {
	return false, nil
}

func (s *interruptRegistry) Push(_ context.Context, dgst digest.Digest, _ int64, r io.Reader) error {
	_, _ = io.Copy(io.Discard, r)
	s.mu.Lock()
	s.pushed = append(s.pushed, dgst)
	s.mu.Unlock()
	return nil
}

func (s *interruptRegistry) Pull(context.Context, digest.Digest) (io.ReadCloser, error) {
	return nil, transfer.ErrNotFound
}
