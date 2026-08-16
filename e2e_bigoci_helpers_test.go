//go:build e2e

package imgoci

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"

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

// progressSink records the latest [Progress] snapshot under a mutex so a
// WithProgress callback is safe under -race.
type progressSink struct {
	mu   sync.Mutex
	last Progress
}

// fn returns a [WithProgress] callback that stores each snapshot.
func (s *progressSink) fn() func(Progress) {
	return func(p Progress) {
		s.mu.Lock()
		s.last = p
		s.mu.Unlock()
	}
}

// snapshot returns the latest stored snapshot.
func (s *progressSink) snapshot() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
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
	sel Selector,
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
	if _, err = ParseIndex(raw); err != nil {
		t.Fatalf("seeded index is not consumer-valid: %v", err)
	}
	seedManifest(t, registry, repo, e2eTag, index.MediaTypeIndex, raw, e2eCreds{})
}

// qemuDiskSelector is the single-role qemu/qcow2 disk used by BigOCI e2e.
func qemuDiskSelector(compression string) Selector {
	return Selector{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Role:           "disk",
		Compression:    compression,
	}
}

// qemuDiskQuery selects the qemu/qcow2 disk at compression.
func qemuDiskQuery(compression string) ResolveQuery {
	return ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{compression},
	}
}

// metalDiskSelector is the shared-digest metal/raw companion.
func metalDiskSelector(compression string) Selector {
	return Selector{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "raw",
		Role:           "disk",
		Compression:    compression,
	}
}

// multipartFileSpec is one FileSpec requesting BigOCI publication at partSize.
func multipartFileSpec(path string, sel Selector, filename string, partSize int64) FileSpec {
	return FileSpec{
		Source:    FromFile(path),
		Selector:  sel,
		Filename:  filename,
		Multipart: &MultipartSpec{PartSize: partSize},
	}
}

// singleRoleMultipartSpec publishes one qemu disk as BigOCI.
func singleRoleMultipartSpec(
	t *testing.T,
	compression string,
	content []byte,
	partSize int64,
) (ReleaseSpec, string, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeStoredSource(t, dir, "disk.qcow2", compression, content)
	spec := ReleaseSpec{
		Name:    "e2e",
		Version: "1",
		Files:   []FileSpec{multipartFileSpec(path, qemuDiskSelector(compression), "disk.qcow2", partSize)},
	}
	return spec, path, "disk.qcow2"
}

// sharedDigestMultipartSpec publishes one stored file as qemu and metal.
func sharedDigestMultipartSpec(t *testing.T, compression string, content []byte, partSize int64) (ReleaseSpec, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeStoredSource(t, dir, "shared.bin", compression, content)
	spec := ReleaseSpec{
		Name:    "e2e",
		Version: "1",
		Files: []FileSpec{
			multipartFileSpec(path, qemuDiskSelector(compression), "disk.qcow2", partSize),
			multipartFileSpec(path, metalDiskSelector(compression), "disk.raw", partSize),
		},
	}
	return spec, path
}

// firstFileEntry returns the first index entry of rel.
func firstFileEntry(t *testing.T, rel *Release) FileEntry {
	t.Helper()
	entries := rel.Index().Entries()
	if len(entries) == 0 {
		t.Fatal("release has no file entries")
	}
	return entries[0]
}

// fileManifestOf GETs the file manifest named by entry.
func fileManifestOf(t *testing.T, host, repo string, entry FileEntry) ([]byte, e2eOCIManifest) {
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

// bigociCLIDir is ~/code/imgoci/bigoci/cli, the CLI's own module.
func bigociCLIDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "code", "imgoci", "bigoci", "cli")
	if _, err = os.Stat(dir); err != nil {
		t.Fatalf("bigoci CLI directory %s: %v", dir, err)
	}
	return dir
}

// runBigociCLI runs `go run .` in the bigoci CLI module with args and
// returns stdout. DOCKER_CONFIG points at an empty directory so the CLI's
// always-on Docker credential helper cannot pick up unrelated logins.
func runBigociCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runBigociCLIOutput(t, args...)
	return strings.TrimSpace(stdout)
}

// runBigociCLIPull is [runBigociCLI] for pull, which writes nothing to stdout.
func runBigociCLIPull(t *testing.T, args ...string) {
	t.Helper()
	_, _ = runBigociCLIOutput(t, args...)
}

// runBigociCLIOutput executes the CLI and returns stdout and stderr.
func runBigociCLIOutput(t *testing.T, args ...string) (string, string) {
	t.Helper()
	cliDir := bigociCLIDir(t)
	dockerConfig := t.TempDir()
	cmdArgs := append([]string{"run", "."}, args...)
	cmd := exec.CommandContext(t.Context(), "go", cmdArgs...)
	cmd.Dir = cliDir
	cmd.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfig)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bigoci CLI %v: %v\nstderr:\n%s\nstdout:\n%s", args, err, stderr.String(), stdout.String())
	}
	return stdout.String(), stderr.String()
}
