package filemanifest

import (
	"bytes"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

// goldenStandardPath holds the independent byte oracle for [BuildStandard].
//
// The file is the spec §13 standard file-manifest example in
// compact canonical form, produced outside this repository by CPython 3
// `json.dumps(sort_keys=True, separators=(",",":"))`. That call is not an
// RFC 8785 implementation in general; it agrees with RFC 8785 on this object
// only because the object is pure ASCII with integer-valued numbers.
// Regenerating the file from [BuildStandard] or internal/jcs copies any
// producer defect into the oracle. testdata/README.md holds the exact command.
const goldenStandardPath = "testdata/standard-v1.json"

// goldenLayerDigest is the layer digest of the spec §13 example manifest.
const goldenLayerDigest = "sha256:" + goldenLayerHex

// goldenLayerHex is the 64-character hex body of [goldenLayerDigest].
const goldenLayerHex = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// goldenLayerSize is the layer size of the spec §13 example manifest.
const goldenLayerSize int64 = 123456789

// TestBuildStandardGoldenBytes compares [BuildStandard] output byte for byte
// with [goldenStandardPath]. A change to the producer member set or to the
// encoding fails here even when this package's own [ValidateStandard] and
// internal/jcs change identically.
func TestBuildStandardGoldenBytes(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(goldenStandardPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(want); n == 0 || want[n-1] == '\n' {
		t.Fatalf("golden %s must be non-empty and have no trailing newline", goldenStandardPath)
	}

	got, err := BuildStandard(BuildInput{
		LayerDigest: digest.Digest(goldenLayerDigest),
		LayerSize:   goldenLayerSize,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("BuildStandard bytes differ from %s\ngot:  %s\nwant: %s", goldenStandardPath, got, want)
	}
}

// TestBuildStandardGoldenCatchesSelfOracleBlindSpot builds producer output that
// both [ValidateStandard] and [jcs.Verify] accept and byte equality with the
// golden rejects.
//
// Spec §6:559-561 requires a consumer to accept producer-only violations such
// as an extra annotations member, and [jcs.Verify] only checks that bytes are
// the canonical form of the tree they encode. Neither one reports a producer
// whose member set grew.
func TestBuildStandardGoldenCatchesSelfOracleBlindSpot(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(goldenStandardPath)
	if err != nil {
		t.Fatal(err)
	}
	// Canonical order puts "annotations" first, so this is still valid JCS.
	drifted := []byte(strings.Replace(string(want),
		`{"artifactType"`,
		`{"annotations":{"io.imgoci.filename":"exampleos-42.1.qcow2"},"artifactType"`, 1))
	if bytes.Equal(drifted, want) {
		t.Fatal("mutation did not change the bytes; the test would be vacuous")
	}

	if _, err := ValidateStandard(drifted); err != nil {
		t.Fatalf("ValidateStandard rejected a producer-only violation: %v", err)
	}
	assertCanonicalBytes(t, drifted)
	if bytes.Equal(drifted, want) {
		t.Fatal("golden did not catch the extra producer member")
	}
}

func TestBuildStandardSelfOracle(t *testing.T) {
	t.Parallel()
	layerDigest := digest.FromBytes([]byte("stored"))
	tests := []struct {
		name string
		in   BuildInput
	}{
		{
			name: "nonzero_layer",
			in:   BuildInput{LayerDigest: layerDigest, LayerSize: 6},
		},
		{
			name: "empty_layer",
			in:   BuildInput{LayerDigest: layerDigest, LayerSize: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := BuildStandard(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			std, err := ValidateStandard(raw)
			if err != nil {
				t.Fatalf("ValidateStandard: %v", err)
			}
			assertRoundTrip(t, tc.in, std, raw)
			assertCanonicalBytes(t, raw)
			assertFixedMembers(t, raw)
		})
	}
}

func TestBuildStandardDigestStability(t *testing.T) {
	t.Parallel()
	in := BuildInput{
		LayerDigest: digest.FromBytes([]byte("stored")),
		LayerSize:   6,
	}
	first, err := BuildStandard(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildStandard(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("unstable encoding\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestBuildStandardRejects(t *testing.T) {
	t.Parallel()
	layerDigest := digest.FromBytes([]byte("stored"))
	tests := []struct {
		name string
		in   BuildInput
		want string
	}{
		{
			name: "empty digest",
			in:   BuildInput{LayerSize: 0},
			want: "digest",
		},
		{
			name: "sha512 digest",
			in: BuildInput{
				LayerDigest: digest.Digest("sha512:" + strings.Repeat("a", 128)),
				LayerSize:   0,
			},
			want: "digest",
		},
		{
			name: "invalid hex",
			in: BuildInput{
				LayerDigest: digest.Digest("sha256:" + strings.Repeat("z", 64)),
				LayerSize:   0,
			},
			want: "digest",
		},
		{
			name: "negative size",
			in:   BuildInput{LayerDigest: layerDigest, LayerSize: -1},
			want: "size",
		},
		{
			name: "size overflow",
			in:   BuildInput{LayerDigest: layerDigest, LayerSize: maxLayerSize + 1},
			want: "size",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := BuildStandard(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func assertRoundTrip(t *testing.T, in BuildInput, std *Standard, raw []byte) {
	t.Helper()
	if std.ArtifactType != index.ArtifactTypeFile {
		t.Fatalf("artifactType %q, want %q", std.ArtifactType, index.ArtifactTypeFile)
	}
	if std.Layer.Digest != in.LayerDigest {
		t.Fatalf("layer digest %s, want %s", std.Layer.Digest, in.LayerDigest)
	}
	if std.Layer.Size != in.LayerSize {
		t.Fatalf("layer size %d, want %d", std.Layer.Size, in.LayerSize)
	}
	obj := mustObject(t, raw, "file manifest")
	if _, ok := obj["annotations"]; ok {
		t.Fatalf("producer emitted annotations: %v", maps.Keys(obj))
	}
}

func assertCanonicalBytes(t *testing.T, raw []byte) {
	t.Helper()
	tree, err := decodeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := jcs.Verify(raw, tree); err != nil {
		t.Fatalf("jcs.Verify: %v", err)
	}
}

func assertFixedMembers(t *testing.T, raw []byte) {
	t.Helper()
	obj := mustObject(t, raw, "file manifest")
	assertKeySet(t, obj, map[string]struct{}{
		"schemaVersion": {},
		"mediaType":     {},
		"artifactType":  {},
		"config":        {},
		"layers":        {},
	})

	cfg := mustChildObject(t, obj, "config")
	assertKeySet(t, cfg, map[string]struct{}{
		"digest":    {},
		"mediaType": {},
		"size":      {},
	})

	layers, err := asArray(obj["layers"], "layers")
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 {
		t.Fatalf("layers length %d, want 1", len(layers))
	}
	layer, err := asObject(layers[0], "layers[0]")
	if err != nil {
		t.Fatal(err)
	}
	assertKeySet(t, layer, map[string]struct{}{
		"digest":    {},
		"mediaType": {},
		"size":      {},
	})
}

func mustObject(t *testing.T, raw []byte, field string) map[string]any {
	t.Helper()
	tree, err := decodeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := asObject(tree, field)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func mustChildObject(t *testing.T, obj map[string]any, key string) map[string]any {
	t.Helper()
	child, err := asObject(obj[key], key)
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func assertKeySet(t *testing.T, obj map[string]any, want map[string]struct{}) {
	t.Helper()
	if len(obj) != len(want) {
		t.Fatalf("members %v, want %v", maps.Keys(obj), maps.Keys(want))
	}
	for k := range want {
		if _, ok := obj[k]; !ok {
			t.Fatalf("missing member %q in %v", k, maps.Keys(obj))
		}
	}
}
