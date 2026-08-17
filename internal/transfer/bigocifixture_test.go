package transfer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
)

const (
	// bigOCIFixtureRoot is the module-root BigOCI v1 artifact tree shared
	// with the container-gated e2e suite. See testdata/bigoci/README.md.
	bigOCIFixtureRoot = "../../testdata/bigoci/v1"
	// bigOCIFixtureTwoPart is the conforming two-part artifact: the shape
	// spec §8 rule 2 requires of a BigOCI file imgoci retrieves.
	bigOCIFixtureTwoPart = "valid-two-part"
	// bigOCIFixtureOnePart is a conforming one-part artifact. It is a valid
	// BigOCI v1 file that the imgoci profile must reject for its part count
	// alone.
	bigOCIFixtureOnePart = "valid-one-part"
	// bigOCITitle is the two-part fixture's OCI title. It differs from every
	// io.imgoci.filename the tests use so the two names cannot be confused
	// (spec §8: a BigOCI title has no imgoci meaning).
	bigOCITitle = "bigoci-title-not-imgoci-name.bin"
	// bigOCIOnePartTitle is the one-part fixture's OCI title.
	bigOCIOnePartTitle = "bigoci-one-part-title.bin"
	// bigOCISchemaVersion is the OCI image-manifest schema version BigOCI v1
	// requires.
	bigOCISchemaVersion = 2
	// annotationBigOCIFileDigest is the BigOCI whole-file digest annotation.
	annotationBigOCIFileDigest = "io.bigoci.file.digest"
	// annotationBigOCIFileSize is the BigOCI whole-file size annotation.
	annotationBigOCIFileSize = "io.bigoci.file.size"
	// annotationBigOCIPartSize is the BigOCI split part-size annotation.
	annotationBigOCIPartSize = "io.bigoci.part.size"
	// bigOCIManifestFile is the manifest file name inside a fixture
	// directory.
	bigOCIManifestFile = "manifest.json"
	// decimalBase is the base the BigOCI size annotations are written in.
	decimalBase = 10
	// sizeBits is the width the BigOCI size annotations parse into.
	sizeBits = 64
)

// bigOCIArtifact is a committed BigOCI v1 fixture read off disk.
type bigOCIArtifact struct {
	// name is the fixture directory name under [bigOCIFixtureRoot].
	name string
	// manifest is the committed manifest.json bytes, byte for byte.
	manifest []byte
	// parts are the committed part blobs in manifest layer order.
	parts [][]byte
	// stored is the parts concatenated: the BigOCI stored file.
	stored []byte
	// partSize is the io.bigoci.part.size annotation.
	partSize int64
	// title is the org.opencontainers.image.title annotation.
	title string
}

// loadBigOCIArtifact reads the committed fixture named name.
//
// The manifest bytes are returned unmodified because spec §9 exempts a
// BigOCI manifest from imgoci re-encoding.
// [TestBigOCIFixturesAreValidBigOCIV1] checks them against the part blobs
// beside them.
func loadBigOCIArtifact(t *testing.T, name string) bigOCIArtifact {
	t.Helper()
	dir := filepath.Join(bigOCIFixtureRoot, name)
	raw, err := os.ReadFile(filepath.Join(dir, bigOCIManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	manifest := decodeOCIManifest(t, raw)
	parts := make([][]byte, len(manifest.Layers))
	var stored []byte
	for i := range manifest.Layers {
		part, readErr := os.ReadFile(filepath.Join(dir, fmt.Sprintf("part-%d.bin", i)))
		if readErr != nil {
			t.Fatal(readErr)
		}
		parts[i] = part
		stored = append(stored, part...)
	}
	partSize, err := strconv.ParseInt(manifest.Annotations[annotationBigOCIPartSize], decimalBase, sizeBits)
	if err != nil {
		t.Fatalf("%s: io.bigoci.part.size: %v", name, err)
	}
	return bigOCIArtifact{
		name:     name,
		manifest: raw,
		parts:    parts,
		stored:   stored,
		partSize: partSize,
		title:    manifest.Annotations[ocispec.AnnotationTitle],
	}
}

// decodeOCIManifest parses raw as an OCI image manifest.
func decodeOCIManifest(t *testing.T, raw []byte) ocispec.Manifest {
	t.Helper()
	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// bigOCIManifestBytes encodes a valid BigOCI File Format v1 manifest for
// stored split at partSize, carrying every member
// github.com/imgoci/bigoci v0.2.0 requires. title is written when non-empty.
//
// The encoding is BigOCI's canonical form — compact JSON, OCI struct member
// order, sorted annotation keys, no HTML escaping — not RFC 8785, which
// spec §9 exempts BigOCI manifests from.
func bigOCIManifestBytes(t *testing.T, stored []byte, partSize int64, title string) []byte {
	t.Helper()
	parts := bigOCISplit(t, stored, partSize)
	layers := make([]ocispec.Descriptor, len(parts))
	for i, part := range parts {
		layers[i] = ocispec.Descriptor{
			MediaType: filemanifest.MediaTypePart,
			Digest:    digest.FromBytes(part),
			Size:      int64(len(part)),
		}
	}
	config := ocispec.DescriptorEmptyJSON
	config.Data = nil
	annotations := map[string]string{
		annotationBigOCIFileDigest: digest.FromBytes(stored).String(),
		annotationBigOCIFileSize:   strconv.FormatInt(int64(len(stored)), decimalBase),
		annotationBigOCIPartSize:   strconv.FormatInt(partSize, decimalBase),
	}
	if title != "" {
		annotations[ocispec.AnnotationTitle] = title
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: bigOCISchemaVersion},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: index.ArtifactTypeBigOCI,
		Config:       config,
		Layers:       layers,
		Annotations:  annotations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})
}

// bigOCIManifestWithAnnotation returns raw with annotation key set to value.
//
// The result is neither canonical nor consistent with the split it describes:
// a registry document that misstates the stored file, which spec §8 requires
// the consumer to catch with its own digest and size checks.
func bigOCIManifestWithAnnotation(t *testing.T, raw []byte, key, value string) []byte {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	annotations, ok := obj["annotations"].(map[string]any)
	if !ok {
		t.Fatal("manifest has no annotations object")
	}
	annotations[key] = value
	out, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// bigOCISplit cuts stored into partSize-byte parts, the last one short.
func bigOCISplit(t *testing.T, stored []byte, partSize int64) [][]byte {
	t.Helper()
	if partSize <= 0 {
		t.Fatalf("part size %d must be positive", partSize)
	}
	parts := make([][]byte, 0, bigOCIPartCount(int64(len(stored)), partSize))
	for offset := int64(0); offset < int64(len(stored)); offset += partSize {
		end := min(offset+partSize, int64(len(stored)))
		parts = append(parts, stored[offset:end])
	}
	return parts
}

// bigOCIPartCount returns how many parts the BigOCI split rule plans for a
// file of size bytes at partSize bytes per part.
func bigOCIPartCount(size, partSize int64) int64 {
	count := size / partSize
	if size%partSize != 0 {
		count++
	}
	return count
}

// bigOCIHalfPartSize is the part size that splits n bytes into exactly two
// parts, for n of at least two. Unit fixtures use it so every runtime-built
// manifest is a two-part artifact, the shape spec §8 rule 2 requires.
func bigOCIHalfPartSize(n int) int64 {
	return int64((n + 1) / 2)
}

// TestBigOCIFixturesAreValidBigOCIV1 checks the committed fixtures against
// every member github.com/imgoci/bigoci v0.2.0 requires of a manifest it
// decodes, and pins them to [bigOCIManifestBytes].
//
// The imgoci consumer profile ignores schemaVersion, the config descriptor,
// the split rule, and io.bigoci.part.size, so without this check the rest of
// the suite would pass on manifests a BigOCI reader rejects.
func TestBigOCIFixturesAreValidBigOCIV1(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		wantParts int
		wantTitle string
	}{
		{name: bigOCIFixtureTwoPart, wantParts: 2, wantTitle: bigOCITitle},
		{name: bigOCIFixtureOnePart, wantParts: 1, wantTitle: bigOCIOnePartTitle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fx := loadBigOCIArtifact(t, tc.name)
			manifest := decodeOCIManifest(t, fx.manifest)
			if len(fx.parts) != tc.wantParts {
				t.Fatalf("parts = %d, want %d", len(fx.parts), tc.wantParts)
			}
			if fx.title != tc.wantTitle {
				t.Fatalf("title = %q, want %q", fx.title, tc.wantTitle)
			}
			assertBigOCIIdentity(t, manifest)
			assertBigOCIParts(t, fx, manifest)
			assertBigOCIFileAnnotations(t, fx, manifest)
			if got := bigOCIManifestBytes(t, fx.stored, fx.partSize, fx.title); !bytes.Equal(got, fx.manifest) {
				t.Fatalf(
					"committed manifest is not the canonical encoding of its parts:\n got %s\nwant %s",
					got,
					fx.manifest,
				)
			}
		})
	}
}

// assertBigOCIIdentity checks the schema version, media type, artifact type,
// and the OCI empty config descriptor.
func assertBigOCIIdentity(t *testing.T, manifest ocispec.Manifest) {
	t.Helper()
	if manifest.SchemaVersion != bigOCISchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", manifest.SchemaVersion, bigOCISchemaVersion)
	}
	if !index.EqualMediaType(manifest.MediaType, ocispec.MediaTypeImageManifest) {
		t.Fatalf("mediaType = %q, want %q", manifest.MediaType, ocispec.MediaTypeImageManifest)
	}
	if !index.EqualMediaType(manifest.ArtifactType, index.ArtifactTypeBigOCI) {
		t.Fatalf("artifactType = %q, want %q", manifest.ArtifactType, index.ArtifactTypeBigOCI)
	}
	want := ocispec.DescriptorEmptyJSON
	got := manifest.Config
	if !index.EqualMediaType(got.MediaType, want.MediaType) || got.Digest != want.Digest || got.Size != want.Size {
		t.Fatalf("config = %+v, want the OCI empty descriptor %+v", got, want)
	}
	if len(got.Data) != 0 {
		t.Fatal("config carries inline data the canonical encoding omits")
	}
}

// assertBigOCIParts checks every part descriptor against the committed part
// blob and against the split io.bigoci.part.size implies.
func assertBigOCIParts(t *testing.T, fx bigOCIArtifact, manifest ocispec.Manifest) {
	t.Helper()
	stored := int64(len(fx.stored))
	if want := bigOCIPartCount(stored, fx.partSize); int64(len(manifest.Layers)) != want {
		t.Fatalf("layers = %d, want %d for %d bytes at part size %d",
			len(manifest.Layers), want, stored, fx.partSize)
	}
	for i, layer := range manifest.Layers {
		part := fx.parts[i]
		if !index.EqualMediaType(layer.MediaType, filemanifest.MediaTypePart) {
			t.Fatalf("layers[%d] mediaType = %q, want %q", i, layer.MediaType, filemanifest.MediaTypePart)
		}
		if layer.Digest != digest.FromBytes(part) {
			t.Fatalf("layers[%d] digest = %s, want %s", i, layer.Digest, digest.FromBytes(part))
		}
		if layer.Size != int64(len(part)) {
			t.Fatalf("layers[%d] size = %d, want %d", i, layer.Size, len(part))
		}
		want := fx.partSize
		if i == len(manifest.Layers)-1 {
			want = stored - fx.partSize*int64(len(manifest.Layers)-1)
		}
		if layer.Size != want {
			t.Fatalf("layers[%d] size = %d, want %d under the split rule", i, layer.Size, want)
		}
	}
}

// assertBigOCIFileAnnotations checks the whole-file digest and size
// annotations against the concatenated part blobs.
func assertBigOCIFileAnnotations(t *testing.T, fx bigOCIArtifact, manifest ocispec.Manifest) {
	t.Helper()
	wantDigest := digest.FromBytes(fx.stored).String()
	if got := manifest.Annotations[annotationBigOCIFileDigest]; got != wantDigest {
		t.Fatalf("%s = %q, want %q", annotationBigOCIFileDigest, got, wantDigest)
	}
	wantSize := strconv.Itoa(len(fx.stored))
	if got := manifest.Annotations[annotationBigOCIFileSize]; got != wantSize {
		t.Fatalf("%s = %q, want %q", annotationBigOCIFileSize, got, wantSize)
	}
	if got := manifest.Annotations[annotationBigOCIPartSize]; got == "" {
		t.Fatalf("%s is missing", annotationBigOCIPartSize)
	}
}
