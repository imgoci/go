//go:build e2e

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	imgoci "github.com/imgoci/go"
	"github.com/imgoci/go/internal/index"
)

const (
	// e2eBigOCIPartSize is small enough that a few-hundred-byte file plans at
	// least two parts. Random content keeps gzip from collapsing below this.
	e2eBigOCIPartSize int64 = 32
	// e2eBigOCIFileSize is the decoded payload for multipart round trips.
	e2eBigOCIFileSize = 128
	// e2eBigOCITinySize is small enough that even a 32-byte part size plans
	// one part, and the 512 MiB default certainly does.
	e2eBigOCITinySize = 8
	// annotationBigOCIFileSize is the BigOCI stored-size annotation.
	annotationBigOCIFileSize = "io.bigoci.file.size"
)

// e2eOCIDescriptor is one OCI descriptor in a retrieved file manifest.
type e2eOCIDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// e2eOCIManifest is the JSON shape of a retrieved OCI image manifest,
// including a BigOCI file.
type e2eOCIManifest struct {
	MediaType    string             `json:"mediaType"`
	ArtifactType string             `json:"artifactType"`
	Config       e2eOCIDescriptor   `json:"config"`
	Layers       []e2eOCIDescriptor `json:"layers"`
	Annotations  map[string]string  `json:"annotations"`
}

// progressSink records every [Progress] snapshot under a mutex so a
// WithProgress callback is safe under -race.
type progressSink struct {
	mu    sync.Mutex
	last  imgoci.Progress
	snaps []imgoci.Progress
}

// fn returns a [WithProgress] callback that stores each snapshot.
func (s *progressSink) fn() func(imgoci.Progress) {
	return func(p imgoci.Progress) {
		s.mu.Lock()
		s.last = p
		s.snaps = append(s.snaps, p)
		s.mu.Unlock()
	}
}

// snapshot returns the latest stored snapshot.
func (s *progressSink) snapshot() imgoci.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// snapshots returns a copy of every stored snapshot.
func (s *progressSink) snapshots() []imgoci.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]imgoci.Progress, len(s.snaps))
	copy(out, s.snaps)
	return out
}

// randomBytes returns size cryptographically random bytes so gzip cannot
// shrink a multipart fixture below two parts.
func randomBytes(t *testing.T, size int) []byte {
	t.Helper()
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

// parseOCIManifest decodes an OCI image manifest.
func parseOCIManifest(t *testing.T, raw []byte) e2eOCIManifest {
	t.Helper()
	var m e2eOCIManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("file manifest JSON: %v", err)
	}
	return m
}

// mutateManifestJSON unmarshals raw into a generic object, applies fn, and
// remarsals. BigOCI manifests are not RFC 8785-canonical, so key order after
// remarshal is fine.
func mutateManifestJSON(t *testing.T, raw []byte, fn func(map[string]any)) []byte {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	fn(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// seedIndexForFileManifest PUTs fileManifest by digest and a release index at
// e2eTag that names it. artifactType is the index descriptor artifactType.
func seedIndexForFileManifest(
	t *testing.T,
	registry, repo string,
	fileManifest []byte,
	artifactType, mediaType string,
	sel imgoci.Selector,
	filename string,
	content []byte,
) {
	t.Helper()
	if mediaType == "" {
		mediaType = index.MediaTypeManifest
	}
	if artifactType == "" {
		artifactType = index.ArtifactTypeBigOCI
	}
	dgst := digest.FromBytes(fileManifest)
	seedManifest(t, registry, repo, dgst.String(), index.MediaTypeManifest, fileManifest, e2eCreds{})
	raw, err := index.Build(&index.Model{
		Name:    "e2e",
		Version: "1",
		Entries: []index.ModelEntry{{
			MediaType:    mediaType,
			ArtifactType: artifactType,
			Digest:       dgst,
			Size:         int64(len(fileManifest)),
			Selector: index.Selector{
				Architecture:   sel.Architecture,
				Target:         sel.Target,
				Representation: sel.Representation,
				Usage:          sel.Usage.String(),
				Role:           sel.Role,
				Compression:    sel.Compression,
			},
			ContentDigest: digest.FromBytes(content),
			ContentSize:   int64(len(content)),
			Filename:      filename,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = imgoci.ParseIndex(raw); err != nil {
		t.Fatalf("seeded index is not consumer-valid: %v", err)
	}
	seedManifest(t, registry, repo, e2eTag, index.MediaTypeIndex, raw, e2eCreds{})
}

// qemuDiskSelector is the single-role qemu/qcow2 disk used by BigOCI e2e.
func qemuDiskSelector(compression string) imgoci.Selector {
	return imgoci.Selector{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Role:           "disk",
		Compression:    compression,
	}
}

// qemuDiskQuery selects the qemu/qcow2 disk at compression.
func qemuDiskQuery(compression string) imgoci.ResolveQuery {
	return imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{compression},
	}
}

// metalDiskSelector is the shared-digest metal/raw companion.
func metalDiskSelector(compression string) imgoci.Selector {
	return imgoci.Selector{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "raw",
		Role:           "disk",
		Compression:    compression,
	}
}

// multipartFileSpec is one FileSpec requesting BigOCI publication at partSize.
func multipartFileSpec(path string, sel imgoci.Selector, filename string, partSize int64) imgoci.FileSpec {
	return imgoci.FileSpec{
		Source:    imgoci.FromFile(path),
		Selector:  sel,
		Filename:  filename,
		Multipart: &imgoci.MultipartSpec{PartSize: partSize},
	}
}

// singleRoleMultipartSpec publishes one qemu disk as BigOCI.
func singleRoleMultipartSpec(
	t *testing.T,
	compression string,
	content []byte,
	partSize int64,
) (imgoci.ReleaseSpec, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeStoredSource(t, dir, "disk.qcow2", compression, content)
	spec := imgoci.ReleaseSpec{
		Name:    "e2e",
		Version: "1",
		Files:   []imgoci.FileSpec{multipartFileSpec(path, qemuDiskSelector(compression), "disk.qcow2", partSize)},
	}
	return spec, path, "disk.qcow2"
}

// sharedDigestMultipartSpec publishes one stored file as qemu and metal.
func sharedDigestMultipartSpec(
	t *testing.T,
	compression string,
	content []byte,
	partSize int64,
) (imgoci.ReleaseSpec, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeStoredSource(t, dir, "shared.bin", compression, content)
	spec := imgoci.ReleaseSpec{
		Name:    "e2e",
		Version: "1",
		Files: []imgoci.FileSpec{
			multipartFileSpec(path, qemuDiskSelector(compression), "disk.qcow2", partSize),
			multipartFileSpec(path, metalDiskSelector(compression), "disk.raw", partSize),
		},
	}
	return spec, path
}

// firstFileEntry returns the first index entry of rel.
func firstFileEntry(t *testing.T, rel *imgoci.Release) imgoci.FileEntry {
	t.Helper()
	entries := rel.Index().Entries()
	if len(entries) == 0 {
		t.Fatal("release has no file entries")
	}
	return entries[0]
}

// fileManifestOf GETs the file manifest named by entry.
func fileManifestOf(t *testing.T, host, repo string, entry imgoci.FileEntry) ([]byte, e2eOCIManifest) {
	t.Helper()
	raw := getManifestRaw(t, host, repo, entry.Digest.String(), index.MediaTypeManifest, e2eCreds{})
	return raw, parseOCIManifest(t, raw)
}

// headReferencedBlobs HEADs the empty-config blob and every part blob named
// by a published BigOCI file manifest.
func headReferencedBlobs(t *testing.T, host, repo string, m e2eOCIManifest) {
	t.Helper()
	if m.Config.Digest == "" {
		t.Fatal("file manifest has no config digest")
	}
	headBlob(t, host, repo, digest.Digest(m.Config.Digest), e2eCreds{})
	if len(m.Layers) < 2 {
		t.Fatalf("BigOCI file manifest has %d layers, want at least 2 parts", len(m.Layers))
	}
	for _, layer := range m.Layers {
		headBlob(t, host, repo, digest.Digest(layer.Digest), e2eCreds{})
	}
}

// sortedTags returns a copy of tags sorted for comparison.
func sortedTags(tags []string) []string {
	out := append([]string(nil), tags...)
	slices.Sort(out)
	return out
}

// bigOCIFixtureDir is the module-root BigOCI v1 artifact tree, shared with the
// internal/transfer unit suite, relative to this package directory. See
// testdata/bigoci/README.md.
const bigOCIFixtureDir = "../../testdata/bigoci/v1"

// bigOCIFixtureTwoPartName is the committed two-part artifact directory.
const bigOCIFixtureTwoPartName = "valid-two-part"

// bigOCIFixture is a committed BigOCI v1 artifact read off disk.
type bigOCIFixture struct {
	// manifest is the committed manifest.json bytes, byte for byte. imgoci
	// must not re-encode a BigOCI manifest, so these are the bytes seeded.
	manifest []byte
	// parsed is manifest decoded for its descriptors and annotations.
	parsed e2eOCIManifest
	// parts are the committed part blobs in manifest layer order.
	parts [][]byte
	// stored is the parts concatenated: the BigOCI stored file.
	stored []byte
	// title is the org.opencontainers.image.title annotation, which differs
	// from every io.imgoci.filename the tests use.
	title string
}

// loadBigOCIFixture reads the committed artifact named name.
func loadBigOCIFixture(t *testing.T, name string) bigOCIFixture {
	t.Helper()
	dir := filepath.Join(bigOCIFixtureDir, name)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseOCIManifest(t, raw)
	if len(parsed.Layers) < 2 {
		t.Fatalf("fixture %s has %d parts, want at least 2", name, len(parsed.Layers))
	}
	parts := make([][]byte, len(parsed.Layers))
	var stored []byte
	for i := range parsed.Layers {
		part, readErr := os.ReadFile(filepath.Join(dir, "part-"+strconv.Itoa(i)+".bin"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		parts[i] = part
		stored = append(stored, part...)
	}
	return bigOCIFixture{
		manifest: raw,
		parsed:   parsed,
		parts:    parts,
		stored:   stored,
		title:    parsed.Annotations[ocispec.AnnotationTitle],
	}
}

// seedBigOCIFixture PUTs everything fx needs into repo: the OCI empty config
// blob, every part blob, the committed file manifest by digest, and a
// release index naming it at compression "none", where the stored file is
// also the content.
func seedBigOCIFixture(t *testing.T, host, repo string, fx bigOCIFixture, filename string) {
	t.Helper()
	config := ocispec.DescriptorEmptyJSON
	seedBlob(t, host, repo, config.Digest, config.Data, e2eCreds{})
	for i, part := range fx.parts {
		seedBlob(t, host, repo, digest.Digest(fx.parsed.Layers[i].Digest), part, e2eCreds{})
	}
	seedIndexForFileManifest(
		t, host, repo, fx.manifest, index.ArtifactTypeBigOCI, index.MediaTypeManifest,
		qemuDiskSelector("none"), filename, fx.stored,
	)
}

// startResizingBlobProxy fronts backend and serves stored-blob GET bodies
// resized by delta bytes, then returns the proxy host:port. It is the
// length-fault counterpart of [startTruncatingBlobProxy].
//
// Spec §8 rule 2 checks each part's size on its own, so a part body one byte
// short or one byte long must fail retrieval even when nothing else about the
// artifact is wrong. Redirect Location hosts are rewritten to backend because
// zot may name an unreachable container address. Bodies under four bytes — the
// OCI empty config blob — are forwarded untouched.
func startResizingBlobProxy(t *testing.T, backend string, delta int) string {
	t.Helper()
	target, err := url.Parse("http://" + backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	blobClient := &http.Client{
		Timeout: e2eHTTPTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			req.URL.Scheme = "http"
			req.URL.Host = backend
			return nil
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gzipBlobRequest(r) {
			proxy.ServeHTTP(w, r)
			return
		}
		u := *target
		u.Path = r.URL.Path
		u.RawPath = r.URL.RawPath
		u.RawQuery = r.URL.RawQuery
		req, reqErr := http.NewRequestWithContext(r.Context(), r.Method, u.String(), nil)
		if reqErr != nil {
			http.Error(w, reqErr.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		resp, doErr := blobClient.Do(req)
		if doErr != nil {
			http.Error(w, doErr.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadGateway)
			return
		}
		switch resp.StatusCode {
		case http.StatusOK, http.StatusPartialContent:
			if len(body) >= 4 {
				body = resizeBlobBody(body, delta)
			}
		}
		for key, values := range resp.Header {
			switch strings.ToLower(key) {
			case "content-length", "content-range", "transfer-encoding", "docker-content-digest":
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return hostPortOf(t, server.URL)
}

// resizeBlobBody returns body with delta bytes removed from or appended to its
// end. Padding is zero bytes, so only the length differs from body.
func resizeBlobBody(body []byte, delta int) []byte {
	out := bytes.Clone(body)
	switch {
	case delta < 0:
		return out[:max(0, len(out)+delta)]
	case delta > 0:
		return append(out, make([]byte, delta)...)
	default:
		return out
	}
}
