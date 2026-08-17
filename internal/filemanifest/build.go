package filemanifest

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

// BuildInput is producer input for [BuildStandard].
type BuildInput struct {
	// LayerDigest is the SHA-256 digest of the stored file blob.
	LayerDigest digest.Digest
	// LayerSize is the stored file blob size in bytes.
	LayerSize int64
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
// image-manifest media type, [index.ArtifactTypeFile], the OCI empty-config
// constant, and exactly one application/octet-stream layer. The encoded bytes
// are a function of the layer digest and layer size alone. The result is
// intended to pass [ValidateStandard].
func BuildStandard(in BuildInput) ([]byte, error) {
	if err := in.LayerDigest.Validate(); err != nil {
		return nil, fmt.Errorf("layer digest: %w", err)
	}
	if err := requireSHA256(in.LayerDigest); err != nil {
		return nil, fmt.Errorf("layer digest: %w", err)
	}
	if in.LayerSize < 0 || in.LayerSize > maxLayerSize {
		return nil, fmt.Errorf("layer size must be a JSON integer from 0 through %d", maxLayerSize)
	}

	return jcs.Encode(wireManifest{
		SchemaVersion: schemaVersionV2,
		MediaType:     index.MediaTypeManifest,
		ArtifactType:  index.ArtifactTypeFile,
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
	})
}
