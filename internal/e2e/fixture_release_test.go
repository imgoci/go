//go:build e2e

// Release fixtures: stored-file payloads, compression, file manifests, index
// bytes, and the seeded releases the tests fetch.

package e2e

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/opencontainers/go-digest"
	"github.com/ulikunitz/xz"

	imgoci "github.com/imgoci/go"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

const (
	// e2eLargeSize is the uncompressed qemu/qcow2 payload, large enough to
	// exercise streaming copies rather than a single in-memory buffer.
	e2eLargeSize = 3 << 20
	// e2eSmallSize is the uncompressed metal/raw payload.
	e2eSmallSize = 64 << 10
	// qemuPattern is the repeating qemu payload prefix.
	qemuPattern = "imgoci-e2e-qemu-stream-"
	// metalPattern is the repeating metal payload prefix.
	metalPattern = "imgoci-e2e-metal-raw-"
	// e2eAdversarialSize is uncompressed payload for truncated/decode-bomb
	// seeds: large enough to compress, small enough to upload quickly.
	e2eAdversarialSize = 8 << 10
	// e2eDecodeBombCeiling is a content-size well below e2eAdversarialSize
	// so CountingHashWriter aborts mid-stream.
	e2eDecodeBombCeiling = 1024
)

// seededFile is one file-manifest plus its stored layer, ready to push.
type seededFile struct {
	// content is the decoded payload FetchFiles must write.
	content []byte
	// stored is the layer blob (compressed when compression is not none).
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
	stored := compressBytes(t, compression, content)
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

// compressBytes encodes p as a stored layer for compression.
func compressBytes(t *testing.T, compression string, p []byte) []byte {
	t.Helper()
	switch compression {
	case "none":
		return p
	case "gzip":
		return gzipBytes(t, p)
	case "xz":
		return xzBytes(t, p)
	case "zstd":
		return zstdBytes(t, p)
	default:
		t.Fatalf("unknown compression %q", compression)
		return nil
	}
}

// storedSourceName appends the conventional suffix for a stored source file.
func storedSourceName(name, compression string) string {
	switch compression {
	case "gzip":
		return name + ".gz"
	case "xz":
		return name + ".xz"
	case "zstd":
		return name + ".zst"
	default:
		return name
	}
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

// xzBytes compresses p as a single xz stream.
func xzBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zstdBytes compresses p as a single non-skippable zstd frame.
func zstdBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Close() }()
	return enc.EncodeAll(p, nil)
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
func fileSpecFromSeeded(path string, file seededFile) imgoci.FileSpec {
	return imgoci.FileSpec{
		Source: imgoci.FromFile(path),
		Selector: imgoci.Selector{
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
// file manifests without Publish still need it present: zot and CNCF
// Distribution reject a file-manifest PUT whose config digest is missing.
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
	specs := make([]imgoci.FileSpec, 0, len(files))
	for i, file := range files {
		name := storedSourceName(fmt.Sprintf("%d-%s", i, file.filename), file.compression)
		specs = append(specs, fileSpecFromSeeded(writeTempBytes(t, dir, name, file.stored), file))
	}
	client := newE2EClient(t, cred)
	dgst, err := client.Publish(t.Context(), tagRef(registry, repo), imgoci.ReleaseSpec{
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
	if _, err = imgoci.ParseIndex(raw); err != nil {
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

// seedOverlongLayer publishes a gzip file whose layer descriptor size is short
// of the stored blob, so BoundedReader must abort. A conforming
// [Client.Publish] records the true stored size.
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

// seedTruncatedStored publishes a file whose layer blob is a prefix of a
// well-formed stored unit. The layer descriptor size matches the truncated
// blob; index content digest and size still name the complete payload. A
// conforming [Client.Publish] records stored size from a complete source read.
func seedTruncatedStored(t *testing.T, registry, repo, compression string) seededFile {
	t.Helper()
	tag := e2eTag
	content := repeatingBytes(qemuPattern, e2eAdversarialSize)
	file := buildSeededFile(t, content, compression, "trunc.qcow2", "qemu", "qcow2", "disk")
	if len(file.stored) < 2 {
		t.Fatal("stored fixture too small to truncate")
	}
	truncated := file.stored[:len(file.stored)/2]
	if len(truncated) == 0 {
		t.Fatal("truncated stored fixture is empty")
	}
	file.stored = truncated
	file.layerDigest = digest.FromBytes(truncated)
	file.manifest = canonicalFileManifest(t, file.layerDigest, int64(len(truncated)))
	file.manifestDigest = digest.FromBytes(file.manifest)

	seedEmptyConfigBlob(t, registry, repo, e2eCreds{})
	seedBlob(t, registry, repo, file.layerDigest, file.stored, e2eCreds{})
	seedManifest(t, registry, repo, file.manifestDigest.String(), index.MediaTypeManifest, file.manifest, e2eCreds{})
	idx := buildCanonicalIndex(t, []seededFile{file})
	seedManifest(t, registry, repo, tag, index.MediaTypeIndex, idx, e2eCreds{})
	return file
}

// seedDecodeBombCeiling publishes a high-ratio stored file whose index content
// size is far below the decoded length, so CountingHashWriter must abort
// mid-stream. A conforming [Client.Publish] records the true decoded size.
func seedDecodeBombCeiling(t *testing.T, registry, repo, compression string) seededFile {
	t.Helper()
	tag := e2eTag
	content := bytes.Repeat([]byte{0}, e2eAdversarialSize)
	file := buildSeededFile(t, content, compression, "bomb.qcow2", "qemu", "qcow2", "disk")
	seedEmptyConfigBlob(t, registry, repo, e2eCreds{})
	pushFile(t, registry, repo, file, e2eCreds{})

	entries := modelEntries([]seededFile{file})
	entries[0].ContentSize = e2eDecodeBombCeiling
	idx := buildIndexFromEntries(t, entries)
	seedManifest(t, registry, repo, tag, index.MediaTypeIndex, idx, e2eCreds{})
	return file
}

// seedBitflippedLayer publishes a gzip qemu disk whose stored layer is
// well-formed, but the index content digest names different decoded bytes. A
// conforming [Client.Publish] writes the hashed decoded bytes as the index
// content digest.
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
// content digest does not match the stored layer, so the second role fails. A
// conforming [Client.Publish] hashes each role's decoded source independently.
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
