package filemanifest

import (
	"fmt"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

// BuildInput is producer input for [BuildStandard].
type BuildInput struct {
	// ArtifactType is the top-level artifactType. Empty selects
	// [index.ArtifactTypeFile].
	ArtifactType string
	// LayerDigest is the SHA-256 digest of the stored file blob.
	LayerDigest digest.Digest
	// LayerSize is the stored file blob size in bytes.
	LayerSize int64
	// Annotations are optional top-level manifest annotations. A nil map
	// omits the member.
	Annotations map[string]string
}

// wireManifest is the producer JSON shape passed to [jcs.Encode].
type wireManifest struct {
	// SchemaVersion is always 2.
	SchemaVersion int64 `json:"schemaVersion"`
	// MediaType is the image-manifest media type.
	MediaType string `json:"mediaType"`
	// ArtifactType is the file artifact type.
	ArtifactType string `json:"artifactType"`
	// Config is the OCI empty descriptor.
	Config wireDescriptor `json:"config"`
	// Layers holds the single file-layer descriptor.
	Layers []wireDescriptor `json:"layers"`
	// Annotations is the optional top-level annotation map.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// wireDescriptor is one producer descriptor (config or layer).
type wireDescriptor struct {
	// MediaType is the descriptor media type.
	MediaType string `json:"mediaType"`
	// Digest is the descriptor digest string.
	Digest string `json:"digest"`
	// Size is the referenced blob size in bytes.
	Size int64 `json:"size"`
}

// BuildStandard constructs a canonical standard file manifest from in.
//
// The output uses the spec §3.1 fixed member set: schemaVersion 2, the
// image-manifest media type, the file artifact type (or in.ArtifactType when
// set), the OCI empty-config constant, and exactly one application/octet-stream
// layer. Annotations are copied when present. The result is intended to pass
// [ValidateStandard].
func BuildStandard(in BuildInput) ([]byte, error) {
	if err := requireUTF8("artifactType", in.ArtifactType); err != nil {
		return nil, err
	}
	if err := in.LayerDigest.Validate(); err != nil {
		return nil, fmt.Errorf("layer digest: %w", err)
	}
	if err := requireSHA256(in.LayerDigest); err != nil {
		return nil, fmt.Errorf("layer digest: %w", err)
	}
	if in.LayerSize < 0 || in.LayerSize > maxLayerSize {
		return nil, fmt.Errorf("layer size must be a JSON integer from 0 through %d", maxLayerSize)
	}
	var ann map[string]string
	if len(in.Annotations) > 0 {
		var err error
		if ann, err = copyAnnotations(in.Annotations); err != nil {
			return nil, err
		}
	}

	artifactType := in.ArtifactType
	if artifactType == "" {
		artifactType = index.ArtifactTypeFile
	}

	return jcs.Encode(wireManifest{
		SchemaVersion: schemaVersionV2,
		MediaType:     index.MediaTypeManifest,
		ArtifactType:  artifactType,
		Config: wireDescriptor{
			MediaType: MediaTypeEmpty,
			Digest:    string(EmptyConfigDigest),
			Size:      EmptyConfigSize,
		},
		Layers: []wireDescriptor{{
			MediaType: MediaTypeLayer,
			Digest:    in.LayerDigest.String(),
			Size:      in.LayerSize,
		}},
		Annotations: ann,
	})
}

// copyAnnotations copies annotation keys and values after a UTF-8 check. The
// input must be non-empty; [BuildStandard] omits the member for empty maps.
func copyAnnotations(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, val := range in {
		if err := requireUTF8("annotation key", k); err != nil {
			return nil, err
		}
		if err := requireUTF8("annotation value", val); err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, nil
}

// requireUTF8 reports an error when s is not valid UTF-8.
func requireUTF8(field, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s is not valid UTF-8", field)
	}
	return nil
}
