package index

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

const (
	testContentDigestA   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testContentDigestB   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testManifestDigest1  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testManifestDigest2  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testManifestDigest3  = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	fixtureRoot          = "../../testdata/conformance/v1"
	conformancePassCount = 12
	conformanceFailCount = 21
)

func TestDecodeInvalidUTF8(t *testing.T) {
	t.Parallel()
	_, err := Decode([]byte{0xff, 0xfe, '{', '}'})
	if err == nil {
		t.Fatal("Decode accepted invalid UTF-8")
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("error %v does not mention UTF-8", err)
	}
}

func TestDecodeDuplicateKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
	}{
		{name: "raw duplicate", raw: `{"a":1,"a":2}`},
		{name: "decoded-equal duplicate", raw: `{"\u0061":1,"a":2}`},
		{name: "nested duplicate", raw: `{"x":{"k":1,"k":2}}`},
		{name: "array object duplicate", raw: `{"x":[{"k":1,"k":2}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode([]byte(tc.raw))
			if err == nil {
				t.Fatal("Decode accepted duplicate keys")
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("error %v does not mention duplicate", err)
			}
		})
	}
}

func TestDecodeWrongMemberTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
	}{
		{name: "mediaType number", raw: `{"mediaType":1}`},
		{name: "artifactType bool", raw: `{"artifactType":true}`},
		{name: "schemaVersion string", raw: `{"schemaVersion":"2"}`},
		{name: "schemaVersion float", raw: `{"schemaVersion":2.5}`},
		{name: "manifests object", raw: `{"manifests":{}}`},
		{name: "annotations array", raw: `{"annotations":[]}`},
		{name: "annotation value number", raw: `{"annotations":{"io.imgoci.name":1}}`},
		{name: "descriptor size string", raw: `{"manifests":[{"size":"1"}]}`},
		{name: "descriptor not object", raw: `{"manifests":["x"]}`},
		{name: "root not object", raw: `[]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode([]byte(tc.raw))
			if err == nil {
				t.Fatal("Decode accepted a wrong member type")
			}
		})
	}
}

// TestDecodeRejectsNonIntegerNumberTokens covers spec §5.1 (schemaVersion must
// be the number 2) and spec §5.2 (size must be a JSON integer): a number token
// that carries a fraction is not an integer even when its value is integral.
func TestDecodeRejectsNonIntegerNumberTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
	}{
		{name: "schemaVersion trailing zero fraction", raw: `{"schemaVersion":2.0}`},
		{name: "descriptor size trailing zero fraction", raw: `{"manifests":[{"size":1.0}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decode([]byte(tc.raw))
			if err == nil {
				t.Fatalf("Decode accepted %s", tc.raw)
			}
			if !strings.Contains(err.Error(), "must be a JSON integer") {
				t.Fatalf("error %v does not name the JSON integer rule", err)
			}
		})
	}
}

func TestDecodeUnknownMembersTolerated(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "pass", "additional-members.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode rejected additional-members.json: %v", err)
	}
	if err := Validate(v); err != nil {
		t.Fatalf("Validate rejected additional-members.json: %v", err)
	}
}

func TestDecodePreservesUnknownAnnotations(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "pass", "unknown-annotations.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v.Annotations["io.imgoci.future"] != "x" {
		t.Fatalf("root unknown annotation lost: %#v", v.Annotations)
	}
	got := v.Manifests[0].Annotations["io.imgoci.file.manifest-type"]
	if got != ArtifactTypeFile {
		t.Fatalf("descriptor unknown annotation lost: %#v", v.Manifests[0].Annotations)
	}
}

// TestDecodePreservesUsageAnnotation feeds raw JSON through [Decode] so
// io.imgoci.usage is asserted on the decoded descriptor, not only on a
// constructed Descriptor literal.
func TestDecodePreservesUsageAnnotation(t *testing.T) {
	t.Parallel()
	const raw = `{"manifests":[{"annotations":{"io.imgoci.usage":"install,live"}}]}`
	v, err := Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Manifests[0].Annotations[AnnotationUsage]; got != "install,live" {
		t.Fatalf("Annotations[%q] = %q, want %q", AnnotationUsage, got, "install,live")
	}
	if got := v.Manifests[0].Selector().Usage; got != "install,live" {
		t.Fatalf("Selector().Usage = %q, want %q", got, "install,live")
	}
}

func TestDescriptorAccessors(t *testing.T) {
	t.Parallel()
	d := validDescriptor(
		"amd64",
		"qemu",
		"qcow2",
		"disk",
		"zstd",
		"disk.qcow2",
		testContentDigestA,
		"42",
		testManifestDigest1,
		7,
	)
	sel := d.Selector()
	if sel.Architecture != "amd64" || sel.Target != "qemu" || sel.Representation != "qcow2" ||
		sel.Role != "disk" || sel.Compression != "zstd" {
		t.Fatalf("Selector() = %+v", sel)
	}
	if d.ContentDigest() != digest.Digest(testContentDigestA) {
		t.Fatalf("ContentDigest() = %q", d.ContentDigest())
	}
	if d.ContentSize() != 42 {
		t.Fatalf("ContentSize() = %d", d.ContentSize())
	}
	if d.Filename() != "disk.qcow2" {
		t.Fatalf("Filename() = %q", d.Filename())
	}
}

func TestDescriptorSelectorUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ann  map[string]string
		want string
	}{
		{name: "absent annotation projects to empty", want: ""},
		{
			name: "present valid value projects verbatim",
			ann:  map[string]string{AnnotationUsage: "install,live"},
			want: "install,live",
		},
		{
			name: "present invalid value is still projected",
			ann:  map[string]string{AnnotationUsage: "live,install"},
			want: "live,install",
		},
		{
			name: "present empty value projects to empty, like absence",
			ann:  map[string]string{AnnotationUsage: ""},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := validDescriptor(
				"amd64", "qemu", "qcow2", "disk", "zstd",
				"disk.qcow2", testContentDigestA, "42", testManifestDigest1, 7,
			)
			maps.Copy(d.Annotations, tc.ann)
			if got := d.Selector().Usage; got != tc.want {
				t.Fatalf("Selector().Usage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescriptorAccessorsInvalid(t *testing.T) {
	t.Parallel()
	d := Descriptor{Annotations: map[string]string{
		AnnotationContentDigest: "not-a-digest",
		AnnotationContentSize:   "nope",
	}}
	if d.ContentDigest() != "" {
		t.Fatalf("ContentDigest() = %q, want empty", d.ContentDigest())
	}
	if d.ContentSize() != 0 {
		t.Fatalf("ContentSize() = %d, want 0", d.ContentSize())
	}
	if d.Filename() != "" {
		t.Fatalf("Filename() = %q, want empty", d.Filename())
	}
}

func validDescriptor(
	architecture, target, representation, role, compression, filename, contentDigest, contentSize, manifestDigest string,
	size int64,
) Descriptor {
	return Descriptor{
		MediaType:    MediaTypeManifest,
		Digest:       digest.Digest(manifestDigest),
		Size:         size,
		ArtifactType: ArtifactTypeFile,
		Annotations: map[string]string{
			AnnotationArchitecture:   architecture,
			AnnotationTarget:         target,
			AnnotationRepresentation: representation,
			AnnotationRole:           role,
			AnnotationCompression:    compression,
			AnnotationContentDigest:  contentDigest,
			AnnotationContentSize:    contentSize,
			AnnotationFilename:       filename,
		},
		mediaTypeSet:    true,
		digestSet:       true,
		sizeSet:         true,
		artifactTypeSet: true,
		annotationsSet:  true,
	}
}

func validValue(manifests ...Descriptor) *Value {
	if len(manifests) == 0 {
		manifests = []Descriptor{
			validDescriptor(
				"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			),
		}
	}
	return &Value{
		SchemaVersion:    schemaVersionV2,
		MediaType:        MediaTypeIndex,
		ArtifactType:     ArtifactTypeRelease,
		Manifests:        manifests,
		Annotations:      map[string]string{AnnotationName: "example", AnnotationVersion: "1"},
		schemaVersionSet: true,
		mediaTypeSet:     true,
		artifactTypeSet:  true,
		manifestsSet:     true,
		annotationsSet:   true,
	}
}

func cloneValue(v *Value) *Value {
	out := *v
	out.Annotations = copyStringMap(v.Annotations)
	out.Manifests = make([]Descriptor, len(v.Manifests))
	for i, d := range v.Manifests {
		out.Manifests[i] = d
		out.Manifests[i].Annotations = copyStringMap(d.Annotations)
	}
	return &out
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
