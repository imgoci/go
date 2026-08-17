package filemanifest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

func TestValidateStandardHappy(t *testing.T) {
	t.Parallel()
	raw := mustCanonical(t, validManifestMap())
	std, err := ValidateStandard(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !index.EqualMediaType(std.MediaType, index.MediaTypeManifest) {
		t.Fatalf("mediaType %q", std.MediaType)
	}
	if !index.EqualMediaType(std.ArtifactType, index.ArtifactTypeFile) {
		t.Fatalf("artifactType %q", std.ArtifactType)
	}
	if std.Layer.Digest == "" || std.Layer.Size != 6 {
		t.Fatalf("layer %+v", std.Layer)
	}
}

func TestValidateStandardExtraMembersTolerated(t *testing.T) {
	t.Parallel()
	m := validManifestMap()
	m["extra"] = true
	m["annotations"] = map[string]any{"io.example.key": "v"}
	cfg := m["config"].(map[string]any)
	cfg["x"] = "y"
	cfg["annotations"] = map[string]any{"io.example.cfg": "v"}
	layer := m["layers"].([]any)[0].(map[string]any)
	layer["foo"] = "bar"
	layer["annotations"] = map[string]any{"io.example.layer": ""}
	raw := mustCanonical(t, m)
	if _, err := ValidateStandard(raw); err != nil {
		t.Fatalf("extra members rejected: %v", err)
	}
}

func TestValidateStandardRejects(t *testing.T) {
	t.Parallel()
	layerDigest := digest.FromBytes([]byte("stored")).String()
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "wrong config digest",
			raw: mustCanonical(t, withConfig(validManifestMap(), map[string]any{
				"digest":    digest.FromBytes([]byte("no")).String(),
				"mediaType": MediaTypeEmpty,
				"size":      EmptyConfigSize,
			})),
			want: "config digest",
		},
		{
			name: "wrong config size",
			raw: mustCanonical(t, withConfig(validManifestMap(), map[string]any{
				"digest":    string(EmptyConfigDigest),
				"mediaType": MediaTypeEmpty,
				"size":      1,
			})),
			want: "config size",
		},
		{
			name: "two layers",
			raw: mustCanonical(t, withLayers(validManifestMap(), []any{
				map[string]any{"digest": layerDigest, "mediaType": MediaTypeLayer, "size": 6},
				map[string]any{"digest": layerDigest, "mediaType": MediaTypeLayer, "size": 6},
			})),
			want: "exactly one",
		},
		{
			name: "wrong layer type",
			raw: mustCanonical(t, withLayers(validManifestMap(), []any{
				map[string]any{"digest": layerDigest, "mediaType": "application/json", "size": 6},
			})),
			want: "octet-stream",
		},
		{
			name: "non-canonical bytes",
			raw:  prettyJSON(t, validManifestMap()),
			want: "canonical",
		},
		{
			name: "schemaVersion wrong value",
			raw:  mustCanonical(t, withMember(validManifestMap(), "schemaVersion", 3)),
			want: "schemaVersion must be 2",
		},
		{
			name: "schemaVersion missing",
			raw:  mustCanonical(t, withoutMember(validManifestMap(), "schemaVersion")),
			want: "schemaVersion is required",
		},
		{
			name: "schemaVersion not an integer",
			raw:  mustCanonical(t, withMember(validManifestMap(), "schemaVersion", 2.5)),
			want: "schemaVersion must be a JSON integer",
		},
		{
			name: "wrong top-level mediaType",
			raw:  mustCanonical(t, withMember(validManifestMap(), "mediaType", index.MediaTypeIndex)),
			want: "mediaType must identify " + index.MediaTypeManifest,
		},
		{
			name: "wrong top-level artifactType",
			raw:  mustCanonical(t, withMember(validManifestMap(), "artifactType", index.ArtifactTypeRelease)),
			want: "artifactType must identify " + index.ArtifactTypeFile,
		},
		{
			name: "missing config",
			raw:  mustCanonical(t, withoutMember(validManifestMap(), "config")),
			want: "config is required",
		},
		{
			name: "wrong config mediaType",
			raw: mustCanonical(t, withConfig(validManifestMap(), map[string]any{
				"digest":    string(EmptyConfigDigest),
				"mediaType": "application/json",
				"size":      EmptyConfigSize,
			})),
			want: "config mediaType must identify " + MediaTypeEmpty,
		},
		{
			name: "zero layers",
			raw:  mustCanonical(t, withLayers(validManifestMap(), []any{})),
			want: "layers must contain exactly one descriptor, got 0",
		},
		{
			name: "malformed layer digest",
			raw:  mustCanonical(t, withLayers(validManifestMap(), []any{layerMap("not-a-digest", 6)})),
			want: "layers[0]: digest",
		},
		{
			name: "uppercase layer digest",
			raw: mustCanonical(t, withLayers(validManifestMap(), []any{
				layerMap("sha256:"+strings.ToUpper(digest.FromBytes([]byte("stored")).Encoded()), 6),
			})),
			want: "layers[0]: digest",
		},
		{
			name: "fractional layer size",
			raw:  mustCanonical(t, withLayers(validManifestMap(), []any{layerMap(layerDigest, 1.5)})),
			want: "layers[0]: size must be a JSON integer",
		},
		{
			name: "negative layer size",
			raw:  mustCanonical(t, withLayers(validManifestMap(), []any{layerMap(layerDigest, -1)})),
			want: "layers[0] size must be a JSON integer from 0 through 9007199254740991",
		},
		{
			name: "layer size above maximum",
			raw:  mustCanonical(t, withLayers(validManifestMap(), []any{layerMap(layerDigest, maxLayerSize+1)})),
			want: "layers[0] size must be a JSON integer from 0 through 9007199254740991",
		},
		{
			name: "non-canonical bytes inside an ignored member",
			raw:  nonCanonicalUnknownMember(t),
			want: "canonical",
		},
		{
			name: "config annotations is not an object",
			raw:  mustCanonical(t, withConfigMember(validManifestMap(), "annotations", "nope")),
			want: "config.annotations must be a JSON object",
		},
		{
			name: "config annotation value is not a string",
			raw: mustCanonical(t, withConfigMember(validManifestMap(), "annotations",
				map[string]any{"bad": 1})),
			want: "config.annotations[bad] must be a JSON string",
		},
		{
			name: "layer annotation value is not a string",
			raw: mustCanonical(t, withLayerMember(validManifestMap(), "annotations",
				map[string]any{"bad": false})),
			want: "layers[0].annotations[bad] must be a JSON string",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateStandard(tc.raw)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateStandardCaseInsensitiveTypes(t *testing.T) {
	t.Parallel()
	m := validManifestMap()
	m["mediaType"] = "APPLICATION/VND.OCI.IMAGE.MANIFEST.V1+JSON"
	m["artifactType"] = "APPLICATION/VND.IMGOCI.FILE.V1"
	raw := mustCanonical(t, m)
	if _, err := ValidateStandard(raw); err != nil {
		t.Fatal(err)
	}
}

// TestValidateStandardAcceptsLayerSizeBoundaries checks the inclusive
// layer-size bounds, 0 and maxLayerSize.
func TestValidateStandardAcceptsLayerSizeBoundaries(t *testing.T) {
	t.Parallel()
	layerDigest := digest.FromBytes([]byte("stored")).String()
	tests := []struct {
		// name identifies the boundary under test.
		name string
		// size is the layer size written to the manifest.
		size any
		// want is the size ValidateStandard must report.
		want int64
	}{
		{name: "zero", size: 0, want: 0},
		{name: "maximum", size: maxLayerSize, want: maxLayerSize},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := mustCanonical(t, withLayers(validManifestMap(), []any{layerMap(layerDigest, tc.size)}))
			std, err := ValidateStandard(raw)
			if err != nil {
				t.Fatalf("size %v rejected: %v", tc.size, err)
			}
			if std.Layer.Size != tc.want {
				t.Fatalf("layer size %d, want %d", std.Layer.Size, tc.want)
			}
		})
	}
}

func validManifestMap() map[string]any {
	return map[string]any{
		"schemaVersion": schemaVersionV2,
		"mediaType":     index.MediaTypeManifest,
		"artifactType":  index.ArtifactTypeFile,
		"config": map[string]any{
			"digest":    string(EmptyConfigDigest),
			"mediaType": MediaTypeEmpty,
			"size":      EmptyConfigSize,
		},
		"layers": []any{
			map[string]any{
				"digest":    digest.FromBytes([]byte("stored")).String(),
				"mediaType": MediaTypeLayer,
				"size":      6,
			},
		},
	}
}

func withConfig(m map[string]any, cfg map[string]any) map[string]any {
	m["config"] = cfg
	return m
}

// withMember sets a top-level manifest member.
func withMember(m map[string]any, key string, val any) map[string]any {
	m[key] = val
	return m
}

// withoutMember deletes a top-level manifest member.
func withoutMember(m map[string]any, key string) map[string]any {
	delete(m, key)
	return m
}

// withConfigMember sets a member on the config descriptor.
func withConfigMember(m map[string]any, key string, val any) map[string]any {
	m["config"].(map[string]any)[key] = val
	return m
}

// withLayerMember sets a member on the first file-layer descriptor.
func withLayerMember(m map[string]any, key string, val any) map[string]any {
	m["layers"].([]any)[0].(map[string]any)[key] = val
	return m
}

// layerMap builds a file-layer descriptor carrying dgst and size verbatim.
func layerMap(dgst string, size any) map[string]any {
	return map[string]any{"digest": dgst, "mediaType": MediaTypeLayer, "size": size}
}

// nonCanonicalUnknownMember canonically encodes a manifest carrying an unknown
// `extra` object, then swaps the two keys inside it. Spec §3.1 requires
// consumers to ignore unknown members, so accepting these bytes would mean
// canonical verification skips that subtree.
func nonCanonicalUnknownMember(t *testing.T) []byte {
	t.Helper()
	raw := mustCanonical(t, withMember(validManifestMap(), "extra", map[string]any{"a": 1, "b": 2}))
	sorted := []byte(`{"a":1,"b":2}`)
	swapped := []byte(`{"b":2,"a":1}`)
	if !bytes.Contains(raw, sorted) {
		t.Fatalf("canonical manifest %s lacks %s", raw, sorted)
	}
	return bytes.Replace(raw, sorted, swapped, 1)
}

func withLayers(m map[string]any, layers []any) map[string]any {
	m["layers"] = layers
	return m
}

func mustCanonical(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := jcs.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func prettyJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
