package filemanifest

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

func TestValidateBigOCIHappy(t *testing.T) {
	t.Parallel()
	fileDigest := digest.FromBytes([]byte("stored-file"))
	raw := mustJSON(t, validBigOCIMap(fileDigest, 11))
	got, err := ValidateBigOCI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !index.EqualMediaType(got.MediaType, index.MediaTypeManifest) {
		t.Fatalf("mediaType %q", got.MediaType)
	}
	if !index.EqualMediaType(got.ArtifactType, index.ArtifactTypeBigOCI) {
		t.Fatalf("artifactType %q", got.ArtifactType)
	}
	if got.FileDigest != fileDigest {
		t.Fatalf("FileDigest %s, want %s", got.FileDigest, fileDigest)
	}
	if got.FileSize != 11 {
		t.Fatalf("FileSize %d, want 11", got.FileSize)
	}
}

func TestValidateBigOCIRejectsOnePart(t *testing.T) {
	t.Parallel()
	m := validBigOCIMap(digest.FromBytes([]byte("stored-file")), 11)
	m["layers"] = []any{validBigOCILayer()}
	_, err := ValidateBigOCI(mustJSON(t, m))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "at least 2 parts") {
		t.Fatalf("error %v does not mention the two-part profile", err)
	}
}

func TestValidateBigOCICaseInsensitiveTypes(t *testing.T) {
	t.Parallel()
	m := validBigOCIMap(digest.FromBytes([]byte("stored-file")), 11)
	m["mediaType"] = "APPLICATION/VND.OCI.IMAGE.MANIFEST.V1+JSON"
	m["artifactType"] = "APPLICATION/VND.BIGOCI.FILE.V1"
	layers := m["layers"].([]any)
	for i := range layers {
		layer := layers[i].(map[string]any)
		layer["mediaType"] = "APPLICATION/VND.BIGOCI.FILE.PART.V1"
	}
	if _, err := ValidateBigOCI(mustJSON(t, m)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBigOCIExtrasTolerated(t *testing.T) {
	t.Parallel()
	m := validBigOCIMap(digest.FromBytes([]byte("stored-file")), 12)
	m["schemaVersion"] = schemaVersionV2
	m["extra"] = true
	m["config"] = map[string]any{
		"digest":    string(EmptyConfigDigest),
		"mediaType": MediaTypeEmpty,
		"size":      EmptyConfigSize,
		"x":         "y",
	}
	ann := m["annotations"].(map[string]any)
	ann["io.bigoci.part.size"] = "6"
	ann["org.opencontainers.image.title"] = "stored.bin"
	layer := m["layers"].([]any)[0].(map[string]any)
	layer["foo"] = "bar"
	if _, err := ValidateBigOCI(mustJSON(t, m)); err != nil {
		t.Fatalf("extras rejected: %v", err)
	}
	if _, err := ValidateBigOCI(prettyJSON(t, m)); err != nil {
		t.Fatalf("non-canonical bytes rejected: %v", err)
	}
}

func TestValidateBigOCIRejects(t *testing.T) {
	t.Parallel()
	fileDigest := digest.FromBytes([]byte("stored-file"))
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "missing digest annotation",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				ann := m["annotations"].(map[string]any)
				delete(ann, annotationFileDigest)
				return m
			}()),
			want: annotationFileDigest,
		},
		{
			name: "missing size annotation",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				ann := m["annotations"].(map[string]any)
				delete(ann, annotationFileSize)
				return m
			}()),
			want: annotationFileSize,
		},
		{
			name: "missing annotations",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				delete(m, "annotations")
				return m
			}()),
			want: "annotations",
		},
		{
			name: "truncated digest",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileDigest] = "sha256:deadbeef"
				return m
			}()),
			want: annotationFileDigest,
		},
		{
			name: "non sha256 digest",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileDigest] = "sha512:" + strings.Repeat("ab", 64)
				return m
			}()),
			want: annotationFileDigest,
		},
		{
			name: "malformed digest",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileDigest] = "not-a-digest"
				return m
			}()),
			want: annotationFileDigest,
		},
		{
			name: "fractional size",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileSize] = "11.0"
				return m
			}()),
			want: annotationFileSize,
		},
		{
			name: "scientific size",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileSize] = "1e2"
				return m
			}()),
			want: annotationFileSize,
		},
		{
			name: "negative size",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileSize] = "-1"
				return m
			}()),
			want: annotationFileSize,
		},
		{
			name: "empty size",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["annotations"].(map[string]any)[annotationFileSize] = ""
				return m
			}()),
			want: annotationFileSize,
		},
		{
			name: "wrong artifact type",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["artifactType"] = index.ArtifactTypeFile
				return m
			}()),
			want: index.ArtifactTypeBigOCI,
		},
		{
			name: "zero layers",
			raw: mustJSON(t, func() map[string]any {
				m := validBigOCIMap(fileDigest, 11)
				m["layers"] = []any{}
				return m
			}()),
			want: "at least 2 parts",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateBigOCI(tc.raw)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v does not contain %q", err, tc.want)
			}
		})
	}
}

func validBigOCIMap(fileDigest digest.Digest, size int64) map[string]any {
	return map[string]any{
		"mediaType":    index.MediaTypeManifest,
		"artifactType": index.ArtifactTypeBigOCI,
		"layers": []any{
			validBigOCILayer(),
			validBigOCILayer(),
		},
		"annotations": map[string]any{
			annotationFileDigest: fileDigest.String(),
			annotationFileSize:   strconv.FormatInt(size, 10),
		},
	}
}

func validBigOCILayer() map[string]any {
	return map[string]any{
		"digest":    digest.FromBytes([]byte("part")).String(),
		"mediaType": MediaTypePart,
		"size":      6,
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
