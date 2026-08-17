//go:build e2e

package imgoci

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/imgoci/go/internal/index"
)

// TestE2EBigOCIRoundTrip publishes a BigOCI file (small PartSize, at least
// two parts), then Fetch → Resolve → FetchFiles, requiring byte-identical
// decoded output.
//
// Matrix: {none, gzip} × {single-role, shared-digest two-deliverable}.
// Shared-digest once-semantics (one PullTo per stored digest per destination
// parent) is unit-covered by internal/transfer.TestFetchFilesBigOCISharedFileDigestSequential;
// this test asserts correctness only.
func TestE2EBigOCIRoundTrip(t *testing.T) {
	t.Parallel()
	compressions := []string{"none", "gzip"}
	shapes := []string{"single-role", "shared-digest"}
	for _, reg := range e2eRegistries() {
		t.Run(reg.name, func(t *testing.T) {
			t.Parallel()
			host := startRegistry(t, reg.image)
			for _, compression := range compressions {
				t.Run(compression, func(t *testing.T) {
					t.Parallel()
					for _, shape := range shapes {
						t.Run(shape, func(t *testing.T) {
							t.Parallel()
							runBigOCIRoundTrip(t, host, compression, shape)
						})
					}
				})
			}
		})
	}
}

func runBigOCIRoundTrip(t *testing.T, host, compression, shape string) {
	t.Helper()
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	client := newE2EClient(t, e2eCreds{})
	var spec ReleaseSpec
	switch shape {
	case "single-role":
		spec, _, _ = singleRoleMultipartSpec(t, compression, content, e2eBigOCIPartSize)
	case "shared-digest":
		spec, _ = sharedDigestMultipartSpec(t, compression, content, e2eBigOCIPartSize)
	default:
		t.Fatalf("unknown shape %q", shape)
	}
	published, err := client.Publish(t.Context(), tagRef(host, repo), spec)
	if err != nil {
		t.Fatal(err)
	}
	rel := mustFetch(t, client, tagRef(host, repo))
	if rel.Digest() != published {
		t.Fatalf("Fetch digest %s, want published %s", rel.Digest(), published)
	}
	entry := firstFileEntry(t, rel)
	if !EqualMediaType(entry.ArtifactType, index.ArtifactTypeBigOCI) {
		t.Fatalf("artifactType %q, want BigOCI", entry.ArtifactType)
	}

	sel := mustResolve(t, client, rel, qemuDiskQuery(compression))
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	assertFileContent(t, filepath.Join(dir, "disk.qcow2"), content)

	if shape != "shared-digest" {
		return
	}
	metal := mustResolve(t, client, rel, ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "raw",
		Compressions:   []string{compression},
	})
	metalDir := t.TempDir()
	mustFetchFiles(t, client, rel, metal, ToDir(metalDir))
	assertFileContent(t, filepath.Join(metalDir, "disk.raw"), content)
}

// TestE2EBigOCIMultipartFallback publishes a tiny file with Multipart
// requested. Fewer than two planned parts uses the standard path,
// Progress.Fallbacks is 1, and the consumer retrieves a standard file.
func TestE2EBigOCIMultipartFallback(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCITinySize)
	spec, _, filename := singleRoleMultipartSpec(t, "none", content, 0)
	client := newE2EClient(t, e2eCreds{})
	var sink progressSink
	if _, err := client.Publish(t.Context(), tagRef(host, repo), spec, WithProgress(sink.fn())); err != nil {
		t.Fatal(err)
	}
	got := sink.snapshot()
	if got.Fallbacks != 1 {
		t.Fatalf("Fallbacks = %d, want 1", got.Fallbacks)
	}
	rel := mustFetch(t, client, tagRef(host, repo))
	entry := firstFileEntry(t, rel)
	if !EqualMediaType(entry.ArtifactType, index.ArtifactTypeFile) {
		t.Fatalf("fallback artifactType %q, want standard file", entry.ArtifactType)
	}
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	assertFileContent(t, filepath.Join(dir, filename), content)
}

// TestE2EBigOCIConcurrentFetchFiles runs two FetchFiles of the same
// selection into different dest files that share a parent directory.
//
// The stored cache is keyed per destination parent
// (`<parent>/.imgoci-stage/stored/`), so both calls lock the same cache entry.
// That once-per-parent lock is unit-covered by internal/file and
// internal/transfer.
func TestE2EBigOCIConcurrentFetchFiles(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, _ := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(host, repo), spec); err != nil {
		t.Fatal(err)
	}
	rel := mustFetch(t, client, tagRef(host, repo))
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))

	parent := t.TempDir()
	paths := []string{filepath.Join(parent, "a.img"), filepath.Join(parent, "b.img")}
	errCh := make(chan error, len(paths))
	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- client.FetchFiles(t.Context(), rel, sel, ToFiles(map[string]string{"disk": path}))
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range paths {
		assertFileContent(t, path, content)
	}
}

// TestE2EBigOCITruncatedPart fails FetchFiles when a part blob is corrupted
// after publish. Content-addressed registries refuse a digest-mismatched
// overwrite, so a reverse proxy bit-flips the part GET (same length: a short
// read is retried via Range and would reassemble). The consumer reports
// [ErrDigestMismatch] (bigoci.ErrDigestMismatch mapped) and commits nothing.
func TestE2EBigOCITruncatedPart(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(backend, repo), spec); err != nil {
		t.Fatal(err)
	}
	rel := mustFetch(t, client, tagRef(backend, repo))
	_, manifest := fileManifestOf(t, backend, repo, firstFileEntry(t, rel))
	if len(manifest.Layers) < 1 {
		t.Fatal("published BigOCI file has no parts")
	}
	if manifest.Layers[0].Digest == "" {
		t.Fatal("first part digest is empty")
	}
	front := startTruncatingBlobProxy(t, backend)
	rel = mustFetch(t, client, tagRef(front, repo))
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, filename))
}

// TestE2EBigOCIWrongFileSize raw-seeds a BigOCI file whose io.bigoci.file.size
// lies. A negative size is rejected by the imgoci BigOCI profile
// ([filemanifest.ValidateBigOCI]); FetchFiles maps that to [ErrInvalidIndex] or
// [ErrDigestMismatch]. A numeric lie that remains a valid token fails bigoci
// Decode with an unmapped split error instead.
func TestE2EBigOCIWrongFileSize(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(host, repo), spec); err != nil {
		t.Fatal(err)
	}
	rel := mustFetch(t, client, tagRef(host, repo))
	entry := firstFileEntry(t, rel)
	raw, parsed := fileManifestOf(t, host, repo, entry)
	if parsed.Annotations[annotationBigOCIFileSize] == "" {
		t.Fatal("published manifest missing io.bigoci.file.size")
	}
	lying := mutateManifestJSON(t, raw, func(obj map[string]any) {
		ann, _ := obj["annotations"].(map[string]any)
		if ann == nil {
			t.Fatal("manifest has no annotations object")
		}
		ann[annotationBigOCIFileSize] = "-1"
	})
	seedIndexForFileManifest(
		t, host, repo, lying, index.ArtifactTypeBigOCI, index.MediaTypeManifest,
		qemuDiskSelector("none"), filename, content,
	)
	rel = mustFetch(t, client, tagRef(host, repo))
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir))
	if !errors.Is(err, ErrInvalidIndex) && !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrInvalidIndex or ErrDigestMismatch", err)
	}
	assertNoFile(t, filepath.Join(dir, filename))
}

// TestE2EBigOCICommittedFixture seeds the committed two-part artifact from
// testdata/bigoci/v1 and retrieves it through the ordinary Client path.
//
// Nothing here is published by imgoci: the manifest, both part blobs, and
// the empty config go in as bytes, so the retrieval runs the real
// internal/multipart.Client and the real bigoci.Client, and both validate
// the artifact the way spec §8 rule 2 requires. A unit test that mocks the
// Multipart port cannot prove the delegation exists; this one fails if it is
// removed or broken.
//
// The fixture's OCI title differs from io.imgoci.filename, so the retrieved
// file also proves the title has no imgoci meaning (spec §8).
func TestE2EBigOCICommittedFixture(t *testing.T) {
	t.Parallel()
	const filename = "disk.qcow2"
	host := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	fx := loadBigOCIFixture(t, bigOCIFixtureTwoPartName)
	if fx.title == "" || fx.title == filename {
		t.Fatalf("fixture title %q must differ from the imgoci filename %q", fx.title, filename)
	}
	seedBigOCIFixture(t, host, repo, fx, filename)

	client := newE2EClient(t, e2eCreds{})
	rel := mustFetch(t, client, tagRef(host, repo))
	entry := firstFileEntry(t, rel)
	if !EqualMediaType(entry.ArtifactType, index.ArtifactTypeBigOCI) {
		t.Fatalf("artifactType %q, want BigOCI", entry.ArtifactType)
	}
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, ToDir(dir))
	assertFileContent(t, filepath.Join(dir, filename), fx.stored)
	assertNoFile(t, filepath.Join(dir, fx.title))
}

// TestE2EBigOCIWrongPartLength fails FetchFiles when a part body arrives one
// byte short or one byte long.
//
// Spec §8 rule 2 verifies each part's digest and size before the parts are
// assembled, so neither length can produce a committed destination. The
// artifact is published normally and only the read path is fronted by
// [startResizingBlobProxy], so the length fault is the single difference
// from a passing round trip.
func TestE2EBigOCIWrongPartLength(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eRegistries()[0].image)
	tests := []struct {
		name  string
		delta int
	}{
		{name: "one-byte-short", delta: -1},
		{name: "one-byte-long", delta: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := testRepo(t)
			content := randomBytes(t, e2eBigOCIFileSize)
			spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
			client := newE2EClient(t, e2eCreds{})
			if _, err := client.Publish(t.Context(), tagRef(backend, repo), spec); err != nil {
				t.Fatal(err)
			}
			front := startResizingBlobProxy(t, backend, tc.delta)
			rel := mustFetch(t, client, tagRef(front, repo))
			sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
			dir := t.TempDir()
			if err := client.FetchFiles(t.Context(), rel, sel, ToDir(dir)); err == nil {
				t.Fatal("FetchFiles accepted a part body of the wrong length")
			}
			assertNoFile(t, filepath.Join(dir, filename))
		})
	}
}
