//go:build e2e

package imgoci

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

const (
	// e2eRegistryPort is the distribution-spec listen port both images expose.
	e2eRegistryPort = "5000/tcp"
	// e2eStartupTimeout is long enough for a first-time image pull.
	e2eStartupTimeout = 3 * time.Minute
	// e2eHTTPTimeout covers seeding a few-MiB blob over localhost.
	e2eHTTPTimeout = 2 * time.Minute
	// e2eLargeSize is the uncompressed qemu/qcow2 payload, large enough to
	// exercise streaming copies rather than a single in-memory buffer.
	e2eLargeSize = 3 << 20
	// e2eSmallSize is the uncompressed metal/raw payload.
	e2eSmallSize = 64 << 10
	// e2eRepo is the repository path every anonymous registry seed uses.
	e2eRepo = "e2e/release"
	// e2eTag is the release tag the production-representative fixture is
	// published at.
	e2eTag = "v1"
	// e2eUser and e2ePass are the htpasswd credentials the auth tests present.
	e2eUser = "e2e"
	e2ePass = "secret"
	// e2eWrongPass is a secret that is not in the htpasswd file.
	e2eWrongPass = "wrong-secret"
	// e2eDistribution is the CNCF Distribution image adversarial cases pin.
	e2eDistribution = "registry:2"
	// identityErrorText is the unexported identityTransport error message.
	identityErrorText = "the response is not identity coded"
	// e2eBearerToken is the token the in-process realm issues.
	e2eBearerToken = "e2e-token"
	// qemuPattern is the repeating qemu payload prefix.
	qemuPattern = "imgoci-e2e-qemu-stream-"
	// metalPattern is the repeating metal payload prefix.
	metalPattern = "imgoci-e2e-metal-raw-"
)

// e2eRegistry is one testcontainers image the core suite runs against.
type e2eRegistry struct {
	// name is the subtest label.
	name string
	// image is the container image reference.
	image string
}

// e2eRegistries returns zot and CNCF Distribution, the two registries the
// slice-2 gate must stay green against.
func e2eRegistries() []e2eRegistry {
	return []e2eRegistry{
		{name: "zot", image: "ghcr.io/project-zot/zot:v2.1.20"},
		{name: "distribution", image: e2eDistribution},
	}
}

// startRegistry launches image and returns the host:port the test process
// should dial. The container is cleaned up with t.
func startRegistry(t *testing.T, image string) string {
	t.Helper()
	return startRegistryWith(t, image, nil, nil, http.StatusOK)
}

// startHtpasswdRegistry launches registry:2 with htpasswd basic auth.
func startHtpasswdRegistry(t *testing.T) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(e2ePass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	htpasswd := e2eUser + ":" + string(hash) + "\n"
	files := []testcontainers.ContainerFile{{
		Reader:            strings.NewReader(htpasswd),
		ContainerFilePath: "/auth/htpasswd",
		FileMode:          0o644,
	}}
	env := map[string]string{
		"REGISTRY_AUTH":                "htpasswd",
		"REGISTRY_AUTH_HTPASSWD_REALM": "RegistryRealm",
		"REGISTRY_AUTH_HTPASSWD_PATH":  "/auth/htpasswd",
	}
	return startRegistryWith(t, e2eDistribution, env, files, http.StatusUnauthorized)
}

// startRegistryWith launches image with optional env and files, waiting until
// GET /v2/ returns wantStatus.
func startRegistryWith(
	t *testing.T,
	image string,
	env map[string]string,
	files []testcontainers.ContainerFile,
	wantStatus int,
) string {
	t.Helper()
	ctx := t.Context()

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts(e2eRegistryPort),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/v2/").WithPort(e2eRegistryPort).
				WithStartupTimeout(e2eStartupTimeout).
				WithStatusCodeMatcher(func(status int) bool { return status == wantStatus }),
		),
	}
	if len(env) > 0 {
		opts = append(opts, testcontainers.WithEnv(env))
	}
	if len(files) > 0 {
		opts = append(opts, testcontainers.WithFiles(files...))
	}

	container, err := testcontainers.Run(ctx, image, opts...)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("starting %s: %v", image, err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, e2eRegistryPort)
	if err != nil {
		t.Fatal(err)
	}
	return net.JoinHostPort(host, port.Port())
}

// e2eCreds are optional basic credentials for seed HTTP against a protected
// registry.
type e2eCreds struct {
	// user is the htpasswd user name. Empty means anonymous.
	user string
	// pass is the htpasswd secret.
	pass string
}

// seedHTTP is the client seed helpers share. It does not follow redirects:
// upload Location headers must be resolved against the original POST URL.
func seedHTTP() *http.Client {
	return &http.Client{
		Timeout: e2eHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// seedBlob uploads data as a monolithic POST+PUT blob, matching go-oci-blob's
// startRegistry seed helper. It is a raw primitive for adversarial fixtures
// a conforming [Client.Publish] cannot emit.
func seedBlob(t *testing.T, registry, repo string, dgst digest.Digest, data []byte, cred e2eCreds) {
	t.Helper()
	ctx := t.Context()
	client := seedHTTP()

	postURL := fmt.Sprintf("http://%s/v2/%s/blobs/uploads/", registry, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start upload: status %d", resp.StatusCode)
	}

	base, err := url.Parse(postURL)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("upload Location %q: %v", resp.Header.Get("Location"), err)
	}
	putURL := base.ResolveReference(loc)
	query := putURL.Query()
	query.Set("digest", dgst.String())
	putURL.RawQuery = query.Encode()

	req, err = http.NewRequestWithContext(ctx, http.MethodPut, putURL.String(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit upload: status %d", resp.StatusCode)
	}
}

// seedManifest PUTs a manifest or index at ref with the given Content-Type.
// It is a raw primitive for adversarial fixtures a conforming [Client.Publish]
// cannot emit.
func seedManifest(t *testing.T, registry, repo, ref, mediaType string, data []byte, cred e2eCreds) {
	t.Helper()
	ctx := t.Context()
	putURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Content-Type", mediaType)
	req.ContentLength = int64(len(data))
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
	default:
		t.Fatalf("put manifest %s: status %d", ref, resp.StatusCode)
	}
}

// getIndexRaw GETs the index document at ref (tag or sha256:…) and returns
// the original response bytes. Accept is the release-index media type.
func getIndexRaw(t *testing.T, registry, repo, ref string, cred e2eCreds) []byte {
	t.Helper()
	getURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, getURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Accept", index.MediaTypeIndex)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get index %s: status %d body %s", ref, resp.StatusCode, body)
	}
	return body
}

// applyCreds attaches basic credentials when they are non-empty.
func applyCreds(req *http.Request, cred e2eCreds) {
	if cred.user != "" {
		req.SetBasicAuth(cred.user, cred.pass)
	}
}

// seededFile is one file-manifest plus its stored layer, ready to push.
type seededFile struct {
	// content is the decoded payload FetchFiles must write.
	content []byte
	// stored is the layer blob (gzip bytes when compression is gzip).
	stored []byte
	// compression is io.imgoci.compression.
	compression string
	// filename is io.imgoci.filename.
	filename string
	// architecture is io.imgoci.architecture.
	architecture string
	// target is io.imgoci.target.
	target string
	// representation is io.imgoci.representation.
	representation string
	// role is io.imgoci.role.
	role string
	// layerDigest is sha256 of stored.
	layerDigest digest.Digest
	// manifest is the canonical file-manifest bytes.
	manifest []byte
	// manifestDigest is sha256 of manifest.
	manifestDigest digest.Digest
}

// buildSeededFile constructs a spec §3.1 standard file and its layer.
func buildSeededFile(
	t *testing.T,
	content []byte,
	compression, filename, target, repr, role string,
) seededFile {
	t.Helper()
	stored := content
	if compression == "gzip" {
		stored = gzipBytes(t, content)
	}
	layerDigest := digest.FromBytes(stored)
	manifest := canonicalFileManifest(t, layerDigest, int64(len(stored)))
	return seededFile{
		content:        content,
		stored:         stored,
		compression:    compression,
		filename:       filename,
		architecture:   "amd64",
		target:         target,
		representation: repr,
		role:           role,
		layerDigest:    layerDigest,
		manifest:       manifest,
		manifestDigest: digest.FromBytes(manifest),
	}
}

// canonicalFileManifest returns RFC 8785 bytes for a spec §3.1 standard
// file manifest: empty config {}, one application/octet-stream layer.
func canonicalFileManifest(t *testing.T, layer digest.Digest, size int64) []byte {
	t.Helper()
	doc := map[string]any{
		"schemaVersion": 2,
		"mediaType":     index.MediaTypeManifest,
		"artifactType":  index.ArtifactTypeFile,
		"config": map[string]any{
			"mediaType": filemanifest.MediaTypeEmpty,
			"digest":    string(filemanifest.EmptyConfigDigest),
			"size":      filemanifest.EmptyConfigSize,
		},
		"layers": []any{
			map[string]any{
				"mediaType": filemanifest.MediaTypeLayer,
				"digest":    layer.String(),
				"size":      size,
			},
		},
	}
	raw, err := jcs.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = filemanifest.ValidateStandard(raw); err != nil {
		t.Fatalf("seeded file manifest is not consumer-valid: %v", err)
	}
	return raw
}

// writeTempBytes writes data at dir/name and returns the path.
func writeTempBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// gzipBytes compresses p as a single gzip member with a zero mtime so the
// stored digest is a function of p alone.
func gzipBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Name = ""
	zw.ModTime = time.Time{}
	if _, err := zw.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// repeatingBytes returns size bytes of pattern, truncated to size.
func repeatingBytes(pattern string, size int) []byte {
	raw := bytes.Repeat([]byte(pattern), size/len(pattern)+1)
	return raw[:size]
}

// seededRelease is a production-representative two-deliverable index plus
// an incus-vm two-role companion used by the last-role and rename tests.
type seededRelease struct {
	// registry is host:port.
	registry string
	// repo is the repository path.
	repo string
	// tag is the tag the canonical index is published at.
	tag string
	// cred is the seed HTTP credential.
	cred e2eCreds
	// index is the canonical release-index bytes.
	index []byte
	// indexDigest is sha256 of index.
	indexDigest digest.Digest
	// qemu is amd64/qemu/qcow2 gzip role disk.
	qemu seededFile
	// metal is amd64/metal/raw none role disk.
	metal seededFile
	// disk is incus-vm role disk.
	disk seededFile
	// metadata is incus-vm role metadata.
	metadata seededFile
}

// seedCanonicalRelease publishes the production-representative fixture through
// [Client.Publish]. qemu is a few MiB gzip disk; metal is a smaller
// uncompressed disk. The incus-vm pair is two roles in one deliverable for
// commit-order tests.
func seedCanonicalRelease(t *testing.T, registry, repo string, cred e2eCreds) seededRelease {
	t.Helper()
	tag := e2eTag
	qemu := buildSeededFile(t, repeatingBytes(qemuPattern, e2eLargeSize),
		"gzip", "disk.qcow2", "qemu", "qcow2", "disk")
	metal := buildSeededFile(t, repeatingBytes(metalPattern, e2eSmallSize),
		"none", "disk.raw", "metal", "raw", "disk")
	disk := buildSeededFile(t, []byte("incus-disk-bytes\n"),
		"none", "incus.img", "incus", "incus-vm", "disk")
	metadata := buildSeededFile(t, []byte("incus-metadata-bytes\n"),
		"none", "incus.meta", "incus", "incus-vm", "metadata")

	dgst := publishSeededFiles(t, registry, repo, cred, []seededFile{qemu, metal, disk, metadata})
	idx := getIndexRaw(t, registry, repo, tag, cred)
	if digest.FromBytes(idx) != dgst {
		t.Fatalf("tagged index digest %s, want published %s", digest.FromBytes(idx), dgst)
	}

	return seededRelease{
		registry:    registry,
		repo:        repo,
		tag:         tag,
		cred:        cred,
		index:       idx,
		indexDigest: dgst,
		qemu:        qemu,
		metal:       metal,
		disk:        disk,
		metadata:    metadata,
	}
}

// fileSpecFromSeeded maps a seeded file onto a [FileSpec] whose Source is path.
func fileSpecFromSeeded(path string, file seededFile) FileSpec {
	return FileSpec{
		Source: FromFile(path),
		Selector: Selector{
			Architecture:   file.architecture,
			Target:         file.target,
			Representation: file.representation,
			Role:           file.role,
			Compression:    file.compression,
		},
		Filename: file.filename,
	}
}

// seedEmptyConfigBlob uploads the OCI empty-config blob `{}` that standard
// file manifests reference.
//
// [Client.Publish] pushes this blob itself. Raw adversarial seeders that PUT
// file manifests without Publish still need it present: both gate registries
// reject a file-manifest PUT whose config digest is missing.
func seedEmptyConfigBlob(t *testing.T, registry, repo string, cred e2eCreds) {
	t.Helper()
	seedBlob(t, registry, repo, filemanifest.EmptyConfigDigest, []byte("{}"), cred)
}

// publishSeededFiles writes each file's stored bytes and publishes them with
// [Client.Publish] at e2eTag. Consumer-subject tests seed this way so the
// producer is the only conforming writer.
func publishSeededFiles(t *testing.T, registry, repo string, cred e2eCreds, files []seededFile) digest.Digest {
	t.Helper()
	dir := t.TempDir()
	specs := make([]FileSpec, 0, len(files))
	for i, file := range files {
		name := fmt.Sprintf("%d-%s", i, file.filename)
		if file.compression == "gzip" {
			name += ".gz"
		}
		specs = append(specs, fileSpecFromSeeded(writeTempBytes(t, dir, name, file.stored), file))
	}
	client := newE2EClient(t, cred)
	dgst, err := client.Publish(t.Context(), tagRef(registry, repo), ReleaseSpec{
		Name:    "e2e",
		Version: "1",
		Files:   specs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return dgst
}

// pushFile uploads the layer blob and the file manifest by digest.
func pushFile(t *testing.T, registry, repo string, file seededFile, cred e2eCreds) {
	t.Helper()
	seedBlob(t, registry, repo, file.layerDigest, file.stored, cred)
	seedManifest(t, registry, repo, file.manifestDigest.String(), index.MediaTypeManifest, file.manifest, cred)
}

// modelEntries copies seeded files into producer entries, hashing each
// file's decoded content as the index content digest.
func modelEntries(files []seededFile) []index.ModelEntry {
	entries := make([]index.ModelEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, index.ModelEntry{
			Digest: file.manifestDigest,
			Size:   int64(len(file.manifest)),
			Selector: index.Selector{
				Architecture:   file.architecture,
				Target:         file.target,
				Representation: file.representation,
				Role:           file.role,
				Compression:    file.compression,
			},
			ContentDigest: digest.FromBytes(file.content),
			ContentSize:   int64(len(file.content)),
			Filename:      file.filename,
		})
	}
	return entries
}

// buildCanonicalIndex encodes a release index through internal/index.Build.
func buildCanonicalIndex(t *testing.T, files []seededFile) []byte {
	t.Helper()
	return buildIndexFromEntries(t, modelEntries(files))
}

// buildIndexFromEntries encodes entries through internal/index.Build and
// requires the result to pass consumer ParseIndex.
func buildIndexFromEntries(t *testing.T, entries []index.ModelEntry) []byte {
	t.Helper()
	raw, err := index.Build(&index.Model{
		Name:    "e2e",
		Version: "1",
		Entries: entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseIndex(raw); err != nil {
		t.Fatalf("seeded index is not consumer-valid: %v", err)
	}
	return raw
}

// seedAlternateIndex publishes a second valid index at the same tag, with
// different disk bytes, so a tag mutation cannot redirect a pinned Fetch.
func seedAlternateIndex(t *testing.T, rel seededRelease) {
	t.Helper()
	alt := buildSeededFile(t, []byte("mutated-qemu-content\n"),
		"gzip", "disk.qcow2", "qemu", "qcow2", "disk")
	publishSeededFiles(t, rel.registry, rel.repo, rel.cred, []seededFile{alt, rel.metal, rel.disk, rel.metadata})
}

// seedOverlongLayer publishes a gzip file manifest whose layer size is short
// of the stored blob, so BoundedReader must abort.
//
// Publish cannot emit this fixture: it records the true stored blob size on
// the file-manifest layer descriptor, so declared size cannot be short of
// the bytes it uploaded.
func seedOverlongLayer(t *testing.T, registry, repo string) seededFile {
	t.Helper()
	tag := e2eTag
	content := bytes.Repeat([]byte("overlong-"), 4096)
	stored := gzipBytes(t, content)
	if len(stored) < 2 {
		t.Fatal("gzipped fixture too small to shorten")
	}
	short := int64(len(stored) - 1)
	layerDigest := digest.FromBytes(stored)
	manifest := canonicalFileManifest(t, layerDigest, short)
	file := seededFile{
		content:        content,
		stored:         stored,
		compression:    "gzip",
		filename:       "short.qcow2",
		architecture:   "amd64",
		target:         "qemu",
		representation: "qcow2",
		role:           "disk",
		layerDigest:    layerDigest,
		manifest:       manifest,
		manifestDigest: digest.FromBytes(manifest),
	}

	seedEmptyConfigBlob(t, registry, repo, e2eCreds{})
	seedBlob(t, registry, repo, file.layerDigest, file.stored, e2eCreds{})
	seedManifest(t, registry, repo, file.manifestDigest.String(), index.MediaTypeManifest, file.manifest, e2eCreds{})
	idx := buildCanonicalIndex(t, []seededFile{file})
	seedManifest(t, registry, repo, tag, index.MediaTypeIndex, idx, e2eCreds{})
	return file
}

// testRepo is a distribution-spec repository unique to this test name.
func testRepo(t *testing.T) string {
	t.Helper()
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, t.Name())
	return "e2e/" + strings.ToLower(name)
}

// tagRef is registry/repo:tag for the consumer Reference grammar.
func tagRef(registry, repo string) Reference {
	return Reference(registry + "/" + repo + ":" + e2eTag)
}

// digestRef is registry/repo@sha256:... for the consumer Reference grammar.
func digestRef(registry, repo string, dgst digest.Digest) Reference {
	return Reference(registry + "/" + repo + "@" + dgst.String())
}

// newE2EClient builds a plaintext client, optionally with static credentials.
func newE2EClient(t *testing.T, cred e2eCreds) *Client {
	t.Helper()
	opts := []Option{WithPlainHTTP()}
	if cred.user != "" {
		opts = append(opts, WithCredentials(cred.user, cred.pass))
	}
	client, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// resolveQEMU selects the qemu/qcow2 disk deliverable.
func resolveQEMU(t *testing.T, client *Client, rel *Release) *Resolved {
	t.Helper()
	return mustResolve(t, client, rel, ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"gzip", "none"},
	})
}

// resolveMetal selects the metal/raw disk deliverable.
func resolveMetal(t *testing.T, client *Client, rel *Release) *Resolved {
	t.Helper()
	return mustResolve(t, client, rel, ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "raw",
		Compressions:   []string{"none", "gzip"},
	})
}

// resolveIncus selects both incus-vm roles.
func resolveIncus(t *testing.T, client *Client, rel *Release) *Resolved {
	t.Helper()
	return mustResolve(t, client, rel, ResolveQuery{
		Architecture:   "amd64",
		Target:         "incus",
		Representation: "incus-vm",
		Compressions:   []string{"none", "gzip"},
	})
}

// mustResolve fails the test when Resolve does.
func mustResolve(t *testing.T, client *Client, rel *Release, q ResolveQuery) *Resolved {
	t.Helper()
	sel, err := client.Resolve(rel, q)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

// mustFetch fails the test when Fetch does.
func mustFetch(t *testing.T, client *Client, ref Reference) *Release {
	t.Helper()
	rel, err := client.Fetch(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

// seedBitflippedLayer publishes a gzip qemu disk whose stored layer is
// well-formed, but the index content digest names different decoded bytes.
//
// Publish cannot emit this fixture: pass 1 hashes decoded bytes and writes
// that digest onto the index; a conforming producer cannot advertise a
// different content digest for the same stored layer.
func seedBitflippedLayer(t *testing.T, registry, repo string) seededFile {
	t.Helper()
	tag := e2eTag
	content := repeatingBytes(qemuPattern, e2eSmallSize)
	file := buildSeededFile(t, content, "gzip", "disk.qcow2", "qemu", "qcow2", "disk")
	seedEmptyConfigBlob(t, registry, repo, e2eCreds{})
	pushFile(t, registry, repo, file, e2eCreds{})

	flipped := slices.Clone(content)
	flipped[0]++
	entries := modelEntries([]seededFile{file})
	entries[0].ContentDigest = digest.FromBytes(flipped)
	idx := buildIndexFromEntries(t, entries)
	seedManifest(t, registry, repo, tag, index.MediaTypeIndex, idx, e2eCreds{})
	return file
}

// seedCorruptSecondRole publishes an incus-vm pair where metadata's index
// content digest does not match the stored layer, so the second role fails.
//
// Publish cannot emit this fixture: each role's index content digest is
// computed from that role's decoded source, so metadata cannot name bytes
// that were never hashed.
func seedCorruptSecondRole(t *testing.T, registry, repo string) (seededFile, seededFile) {
	t.Helper()
	tag := e2eTag
	disk := buildSeededFile(t, []byte("incus-disk-bytes\n"),
		"none", "incus.img", "incus", "incus-vm", "disk")
	metadata := buildSeededFile(t, []byte("incus-metadata-bytes\n"),
		"none", "incus.meta", "incus", "incus-vm", "metadata")
	seedEmptyConfigBlob(t, registry, repo, e2eCreds{})
	pushFile(t, registry, repo, disk, e2eCreds{})
	pushFile(t, registry, repo, metadata, e2eCreds{})

	entries := modelEntries([]seededFile{disk, metadata})
	entries[1].ContentDigest = digest.FromBytes([]byte("not-metadata-bytes\n"))
	idx := buildIndexFromEntries(t, entries)
	seedManifest(t, registry, repo, tag, index.MediaTypeIndex, idx, e2eCreds{})
	return disk, metadata
}

// mustFetchFiles fails the test when FetchFiles does.
func mustFetchFiles(t *testing.T, client *Client, rel *Release, sel *Resolved, dest Dest) {
	t.Helper()
	if err := client.FetchFiles(t.Context(), rel, sel, dest); err != nil {
		t.Fatal(err)
	}
}

// assertFileContent requires path to contain want.
func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %d bytes, want %d identical to seed", path, len(got), len(want))
	}
}

// assertNoFile requires path not to exist.
func assertNoFile(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		t.Fatalf("committed file exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

// assertIdentityError requires err to wrap the identity-enforcement failure.
//
// Manifest Get surfaces the message on Error(); go-oci-blob wraps blob Get
// in a requestError whose Error string is "registry request failed", so the
// identity text is only visible on the unwrapped chain.
func assertIdentityError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected identity-enforcement error")
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), identityErrorText) {
			return
		}
	}
	t.Fatalf("err = %v, want chain containing %q", err, identityErrorText)
}

// getManifestRaw GETs a manifest or index at ref with Accept and returns the
// original response bytes. [getIndexRaw] is the release-index convenience.
func getManifestRaw(t *testing.T, registry, repo, ref, accept string, cred e2eCreds) []byte {
	t.Helper()
	getURL := fmt.Sprintf("http://%s/v2/%s/manifests/%s", registry, repo, ref)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, getURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	req.Header.Set("Accept", accept)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get manifest %s: status %d body %s", ref, resp.StatusCode, body)
	}
	return body
}

// listTagsRaw GETs /v2/<repo>/tags/list and returns the decoded tag names in
// registry order. This is the raw distribution-spec endpoint, not a library
// helper: PushByDigest must not appear here.
func listTagsRaw(t *testing.T, registry, repo string, cred e2eCreds) []string {
	t.Helper()
	listURL := fmt.Sprintf("http://%s/v2/%s/tags/list", registry, repo)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, listURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tags/list %s: status %d body %s", repo, resp.StatusCode, body)
	}
	var parsed struct {
		Tags []string `json:"tags"`
	}
	if err = json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("tags/list JSON: %v body %s", err, body)
	}
	if parsed.Tags == nil {
		return []string{}
	}
	return parsed.Tags
}

// headBlob HEADs /v2/<repo>/blobs/<digest> and requires HTTP 200. Used to
// prove a referenced blob exists without downloading it.
func headBlob(t *testing.T, registry, repo string, dgst digest.Digest, cred e2eCreds) {
	t.Helper()
	headURL := fmt.Sprintf("http://%s/v2/%s/blobs/%s", registry, repo, dgst.String())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, headURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCreds(req, cred)
	resp, err := seedHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD blob %s: status %d", dgst, resp.StatusCode)
	}
}
