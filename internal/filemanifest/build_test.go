package filemanifest

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

func TestBuildStandardSelfOracle(t *testing.T) {
	t.Parallel()
	layerDigest := digest.FromBytes([]byte("stored"))
	tests := []struct {
		name string
		in   BuildInput
	}{
		{
			name: "no_annotations",
			in:   BuildInput{LayerDigest: layerDigest, LayerSize: 6},
		},
		{
			name: "with_annotations",
			in: BuildInput{
				LayerDigest: layerDigest,
				LayerSize:   6,
				Annotations: map[string]string{"io.example.key": "v"},
			},
		},
		{
			name: "custom_artifactType",
			in: BuildInput{
				ArtifactType: "APPLICATION/VND.IMGOCI.FILE.V1",
				LayerDigest:  layerDigest,
				LayerSize:    0,
			},
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
			assertFixedMembers(t, raw, len(tc.in.Annotations) > 0)
		})
	}
}

func TestBuildStandardDigestStability(t *testing.T) {
	t.Parallel()
	in := BuildInput{
		LayerDigest: digest.FromBytes([]byte("stored")),
		LayerSize:   6,
		Annotations: map[string]string{"io.example.key": "v", "io.example.other": "w"},
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
		{
			name: "invalid utf8 annotation key",
			in: BuildInput{
				LayerDigest: layerDigest,
				LayerSize:   0,
				Annotations: map[string]string{string([]byte{0xff}): "v"},
			},
			want: "UTF-8",
		},
		{
			name: "invalid utf8 annotation value",
			in: BuildInput{
				LayerDigest: layerDigest,
				LayerSize:   0,
				Annotations: map[string]string{"k": string([]byte{0xff})},
			},
			want: "UTF-8",
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
	wantType := in.ArtifactType
	if wantType == "" {
		wantType = index.ArtifactTypeFile
	}
	if std.ArtifactType != wantType {
		t.Fatalf("artifactType %q, want %q", std.ArtifactType, wantType)
	}
	if std.Layer.Digest != in.LayerDigest {
		t.Fatalf("layer digest %s, want %s", std.Layer.Digest, in.LayerDigest)
	}
	if std.Layer.Size != in.LayerSize {
		t.Fatalf("layer size %d, want %d", std.Layer.Size, in.LayerSize)
	}
	got := manifestAnnotations(t, raw)
	if !maps.Equal(got, in.Annotations) {
		t.Fatalf("annotations %v, want %v", got, in.Annotations)
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

func assertFixedMembers(t *testing.T, raw []byte, wantAnnotations bool) {
	t.Helper()
	obj := mustObject(t, raw, "file manifest")
	want := map[string]struct{}{
		"schemaVersion": {},
		"mediaType":     {},
		"artifactType":  {},
		"config":        {},
		"layers":        {},
	}
	if wantAnnotations {
		want["annotations"] = struct{}{}
	}
	assertKeySet(t, obj, want)

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

func manifestAnnotations(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	obj := mustObject(t, raw, "file manifest")
	rawAnn, ok := obj["annotations"]
	if !ok {
		return nil
	}
	ann, err := asObject(rawAnn, "annotations")
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(ann))
	for k, v := range ann {
		s, err := asString(v, "annotations["+k+"]")
		if err != nil {
			t.Fatal(err)
		}
		out[k] = s
	}
	return out
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
