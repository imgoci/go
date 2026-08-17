//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/opencontainers/go-digest"

	imgoci "github.com/imgoci/go"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
)

// TestBigOCICaseVariedMediaType raw-seeds a BigOCI file manifest whose
// mediaType, artifactType, and part mediaType spellings vary in ASCII case.
// Spec §4 comparison is case-insensitive; the consumer must accept the file.
func TestBigOCICaseVariedMediaType(t *testing.T) {
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
	raw, _ := fileManifestOf(t, host, repo, firstFileEntry(t, rel))
	varied := mutateManifestJSON(t, raw, func(obj map[string]any) {
		obj["mediaType"] = "APPLICATION/VND.OCI.IMAGE.MANIFEST.V1+JSON"
		obj["artifactType"] = "APPLICATION/VND.BIGOCI.FILE.V1"
		layers, _ := obj["layers"].([]any)
		for _, item := range layers {
			layer, _ := item.(map[string]any)
			if layer == nil {
				continue
			}
			layer["mediaType"] = "APPLICATION/VND.BIGOCI.FILE.PART.V1"
		}
	})
	seedIndexForFileManifest(
		t, host, repo, varied,
		"APPLICATION/VND.BIGOCI.FILE.V1",
		"APPLICATION/VND.OCI.IMAGE.MANIFEST.V1+JSON",
		qemuDiskSelector("none"), filename, content,
	)
	rel = mustFetch(t, client, tagRef(host, repo))
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, imgoci.ToDir(dir))
	assertFileContent(t, filepath.Join(dir, filename), content)
}

// TestBigOCIPushByDigestWritesNoTag asserts that after a multipart
// Publish the registry's raw /v2/<repo>/tags/list contains only the release
// tag. File manifests are digest-only (PushByDigest).
func TestBigOCIPushByDigestWritesNoTag(t *testing.T) {
	t.Parallel()
	host := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, _ := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(host, repo), spec); err != nil {
		t.Fatal(err)
	}
	tags := listTagsRaw(t, host, repo, e2eCreds{})
	if !slices.Equal(sortedTags(tags), []string{e2eTag}) {
		t.Fatalf("tags/list = %q, want only %q", tags, e2eTag)
	}
}

// TestBigOCICLIInterop round-trips a file through the bigoci CLI in both
// directions: our Publish then `bigoci pull` by descriptor digest, and
// `bigoci push` then our FetchFiles via a raw-seeded index.
//
// The CLI directory is resolved once on this parent so a cloned checkout
// outlives the parallel subtests. See [bigociCLIDir] for IMGOCI_BIGOCI_CLI_DIR
// and IMGOCI_BIGOCI_FORCE_CLONE.
func TestBigOCICLIInterop(t *testing.T) {
	t.Parallel()
	_ = bigociCLIDir(t)
	host := startRegistry(t, e2eRegistries()[0].image)
	client := newE2EClient(t, e2eCreds{})

	t.Run("publish-then-cli-pull", func(t *testing.T) {
		t.Parallel()
		repo := testRepo(t)
		content := randomBytes(t, e2eBigOCIFileSize)
		spec, storedPath, _ := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
		if _, err := client.Publish(t.Context(), tagRef(host, repo), spec); err != nil {
			t.Fatal(err)
		}
		rel := mustFetch(t, client, tagRef(host, repo))
		entry := firstFileEntry(t, rel)
		dest := filepath.Join(t.TempDir(), "pulled.bin")
		runBigociCLIPull(t, "pull", "-plain-http",
			host+"/"+repo+"@"+entry.Digest.String(), dest)
		stored, err := os.ReadFile(storedPath)
		if err != nil {
			t.Fatal(err)
		}
		assertFileContent(t, dest, stored)
	})

	t.Run("cli-push-then-fetchfiles", func(t *testing.T) {
		t.Parallel()
		repo := testRepo(t)
		content := randomBytes(t, e2eBigOCIFileSize)
		src := writeTempBytes(t, t.TempDir(), "cli.bin", content)
		cliRef := host + "/" + repo + ":file"
		digestLine := runBigociCLI(t, "push", "-plain-http",
			"-part-size", strconv.FormatInt(e2eBigOCIPartSize, 10),
			src, cliRef)
		fileDgst, err := digest.Parse(digestLine)
		if err != nil {
			t.Fatalf("CLI stdout %q is not a digest: %v", digestLine, err)
		}
		manifest := getManifestRaw(t, host, repo, fileDgst.String(), index.MediaTypeManifest, e2eCreds{})
		seedIndexForFileManifest(
			t, host, repo, manifest, index.ArtifactTypeBigOCI, index.MediaTypeManifest,
			qemuDiskSelector("none"), "disk.qcow2", content,
		)
		rel := mustFetch(t, client, tagRef(host, repo))
		sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
		dir := t.TempDir()
		mustFetchFiles(t, client, rel, sel, imgoci.ToDir(dir))
		assertFileContent(t, filepath.Join(dir, "disk.qcow2"), content)
	})
}

// TestBigOCIGraphCompleteness HEADs every blob referenced by a published
// BigOCI file: the empty config and every part.
func TestBigOCIGraphCompleteness(t *testing.T) {
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
	_, manifest := fileManifestOf(t, host, repo, firstFileEntry(t, rel))
	if manifest.Config.Digest != string(filemanifest.EmptyConfigDigest) {
		t.Fatalf("config digest %s, want empty config %s", manifest.Config.Digest, filemanifest.EmptyConfigDigest)
	}
	headReferencedBlobs(t, host, repo, manifest)
}

// TestBigOCIGzippedProxy fails FetchFiles when a reverse proxy gzip-codes
// blob GETs. The index GET stays identity-coded, so Fetch succeeds;
// Multipart.PullTo then hits bigoci's identity enforcement through our adapter.
// A gzipped file-manifest GET is intercepted by the identity-wrapped registry
// adapter before PullTo, so this test covers the blob path the adapter
// forwards.
func TestBigOCIGzippedProxy(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(backend, repo), spec); err != nil {
		t.Fatal(err)
	}
	front := startGzipProxy(t, backend, gzipBlobRequest)
	rel := mustFetch(t, client, tagRef(front, repo))
	sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
	dir := t.TempDir()
	err := client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(dir))
	assertIdentityError(t, err)
	assertNoFile(t, filepath.Join(dir, filename))
}

// TestBigOCICrossHostRedirect 307s blob GETs to a second in-process host
// that serves the bytes. Identity-coded storage succeeds under bigoci's
// default verified mode; gzip on the second host fails.
func TestBigOCICrossHostRedirect(t *testing.T) {
	t.Parallel()
	backend := startRegistry(t, e2eRegistries()[0].image)
	repo := testRepo(t)
	content := randomBytes(t, e2eBigOCIFileSize)
	spec, _, filename := singleRoleMultipartSpec(t, "none", content, e2eBigOCIPartSize)
	client := newE2EClient(t, e2eCreds{})
	if _, err := client.Publish(t.Context(), tagRef(backend, repo), spec); err != nil {
		t.Fatal(err)
	}

	t.Run("identity-storage", func(t *testing.T) {
		t.Parallel()
		storage := startStorageProxy(t, backend, false)
		front := startBlobRedirectFront(t, backend, storage)
		rel := mustFetch(t, client, tagRef(front, repo))
		sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
		dir := t.TempDir()
		mustFetchFiles(t, client, rel, sel, imgoci.ToDir(dir))
		assertFileContent(t, filepath.Join(dir, filename), content)
	})

	t.Run("gzip-storage", func(t *testing.T) {
		t.Parallel()
		storage := startStorageProxy(t, backend, true)
		front := startBlobRedirectFront(t, backend, storage)
		rel := mustFetch(t, client, tagRef(front, repo))
		sel := mustResolve(t, client, rel, qemuDiskQuery("none"))
		dir := t.TempDir()
		err := client.FetchFiles(t.Context(), rel, sel, imgoci.ToDir(dir))
		assertIdentityError(t, err)
		assertNoFile(t, filepath.Join(dir, filename))
	})
}
