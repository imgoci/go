package filemanifest

import (
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
	layer := m["layers"].([]any)[0].(map[string]any)
	layer["foo"] = "bar"
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
