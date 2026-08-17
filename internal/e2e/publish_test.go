//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"

	imgoci "github.com/imgoci/go"
	"github.com/imgoci/go/internal/index"
)

// TestPublishNonCanonicalIndexRejected fails Fetch with ErrInvalidIndex when
// the tag names pretty-printed index bytes. A conforming [Client.Publish] RFC
// 8785-encodes the index, so it cannot store indented JSON at the tag.
func TestPublishNonCanonicalIndexRejected(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(
		t.Context(),
		tagRef(host, repo),
		simpleReleaseSpec(t, []byte("noncanonical\n")),
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
	if !errors.Is(err, imgoci.ErrInvalidIndex) {
		t.Fatalf("err = %v, want ErrInvalidIndex", err)
	}
}

// TestPublishWrongSizeDescriptorRejected fails FetchFiles when the index
// entry declares a file-manifest size that is not the retrieved byte length. A
// conforming [Client.Publish] records the size from filemanifest.BuildStandard.
func TestPublishWrongSizeDescriptorRejected(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eDistribution)
	repo := testRepo(t)
	client := newE2EClient(t, e2eCreds{})
	spec := simpleReleaseSpec(t, []byte("wrong-size-payload\n"))
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
		Selector:      indexSelectorOf(entries[0].Selector),
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
	sel := mustResolve(t, client, rel, imgoci.ResolveQuery{
		Architecture:   entries[0].Selector.Architecture,
		Target:         entries[0].Selector.Target,
		Representation: entries[0].Selector.Representation,
		Compressions:   []string{entries[0].Selector.Compression},
	})
	err = client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(t.TempDir()))
	if !errors.Is(err, imgoci.ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
}

// TestPublishDigestRefIsTagOnly rejects a digest-only Publish reference
// with ErrInvalidSpec before any registry traffic.
func TestPublishDigestRefIsTagOnly(t *testing.T) {
	t.Parallel()
	hits := &hitTransport{}
	client, err := imgoci.New(imgoci.WithHTTPClient(&http.Client{Transport: hits}), imgoci.WithPlainHTTP())
	if err != nil {
		t.Fatal(err)
	}
	pin := digest.FromBytes([]byte("x"))
	_, err = client.Publish(
		t.Context(),
		imgoci.Reference("example.com/e2e/tagonly@"+pin.String()),
		simpleReleaseSpec(t, []byte("payload")),
	)
	if !errors.Is(err, imgoci.ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if n := hits.n.Load(); n != 0 {
		t.Fatalf("registry traffic = %d, want 0", n)
	}
}

// hitTransport counts HTTP round trips and fails every one of them, so a test
// can assert that a rejection happened before any registry traffic.
type hitTransport struct {
	n atomic.Int64
}

// RoundTrip counts the request and refuses it.
func (h *hitTransport) RoundTrip(*http.Request) (*http.Response, error) {
	h.n.Add(1)
	return nil, errors.New("unexpected registry traffic")
}

// simpleReleaseSpec is a one-file release spec over the given stored bytes,
// declaring no compression.
func simpleReleaseSpec(t *testing.T, data []byte) imgoci.ReleaseSpec {
	t.Helper()
	return imgoci.ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []imgoci.FileSpec{{
			Source: imgoci.FromFile(writeTempBytes(t, t.TempDir(), "file.bin", data)),
			Selector: imgoci.Selector{
				Architecture:   "amd64",
				Target:         "x-test-target",
				Representation: "x-test-format",
				Role:           "x-test-file",
				Compression:    "none",
			},
			Filename: "a",
		}},
	}
}

// indexSelectorOf copies a public selector onto the index model type so a test
// can rebuild index bytes from a fetched release.
func indexSelectorOf(s imgoci.Selector) index.Selector {
	return index.Selector{
		Architecture:   s.Architecture,
		Target:         s.Target,
		Representation: s.Representation,
		Usage:          s.Usage.String(),
		Role:           s.Role,
		Compression:    s.Compression,
	}
}
