package index

import (
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/jcs"
)

// Model is producer input for [Build].
type Model struct {
	// Name is io.imgoci.name.
	Name string
	// Version is org.opencontainers.image.version.
	Version string
	// Annotations are extra top-level annotations. Name and Version overwrite
	// the corresponding keys when both are set.
	Annotations map[string]string
	// Entries become the sorted manifests array.
	Entries []ModelEntry
}

// ModelEntry is one file-entry supplied to [Build].
type ModelEntry struct {
	// MediaType defaults to [MediaTypeManifest] when empty.
	MediaType string
	// ArtifactType defaults to [ArtifactTypeFile] when empty.
	ArtifactType string
	// Digest is the SHA-256 digest of the referenced file manifest.
	Digest digest.Digest
	// Size is the byte length of the referenced file manifest.
	Size int64
	// Selector is the five-field transport-alternative identity.
	Selector Selector
	// ContentDigest is io.imgoci.content.digest.
	ContentDigest digest.Digest
	// ContentSize is io.imgoci.content.size.
	ContentSize int64
	// Filename is io.imgoci.filename.
	Filename string
	// Annotations are extra descriptor annotations. Selector, content, and
	// filename fields overwrite the corresponding keys.
	Annotations map[string]string
}

// wireIndex is the producer JSON shape passed to [jcs.Encode].
type wireIndex struct {
	// SchemaVersion is always 2.
	SchemaVersion int64 `json:"schemaVersion"`
	// MediaType is the release-index media type.
	MediaType string `json:"mediaType"`
	// ArtifactType is the release artifact type.
	ArtifactType string `json:"artifactType"`
	// Manifests is the sorted descriptor array.
	Manifests []wireDescriptor `json:"manifests"`
	// Annotations is the top-level annotation map.
	Annotations map[string]string `json:"annotations"`
}

// wireDescriptor is one file-entry descriptor in producer JSON.
type wireDescriptor struct {
	// MediaType is the descriptor media type.
	MediaType string `json:"mediaType"`
	// Digest is the file-manifest digest string.
	Digest string `json:"digest"`
	// Size is the file-manifest byte length.
	Size int64 `json:"size"`
	// ArtifactType is the referenced manifest artifact type.
	ArtifactType string `json:"artifactType"`
	// Annotations is the descriptor annotation map.
	Annotations map[string]string `json:"annotations"`
}

// Build constructs a canonical release index from m.
//
// Producer-only selector-registry and annotation-location rules run before
// encoding; [Validate] does not apply those rules. Descriptors are sorted by
// the five-field UTF-8 byte-order tuple and encoded with [jcs.Encode]. The
// result is intended to pass [Decode], [Validate], and [VerifyCanonical];
// Build does not re-check that itself.
func Build(m *Model) ([]byte, error) {
	if m == nil {
		return nil, errors.New("model is nil")
	}
	if err := validateProducerModel(m); err != nil {
		return nil, err
	}
	v, err := valueFromModel(m)
	if err != nil {
		return nil, err
	}
	sortManifests(v.Manifests)
	if err := Validate(v); err != nil {
		return nil, err
	}
	return jcs.Encode(wireFromValue(v))
}

// valueFromModel copies a [Model] into a [Value] with presence flags set.
func valueFromModel(m *Model) (*Value, error) {
	if err := requireUTF8("name", m.Name); err != nil {
		return nil, err
	}
	if err := requireUTF8("version", m.Version); err != nil {
		return nil, err
	}
	rootAnn, err := copyAnnotations(m.Annotations)
	if err != nil {
		return nil, fmt.Errorf("root annotations: %w", err)
	}
	rootAnn[AnnotationName] = m.Name
	rootAnn[AnnotationVersion] = m.Version
	manifests := make([]Descriptor, 0, len(m.Entries))
	for i := range m.Entries {
		d, err := descriptorFromModel(m.Entries[i])
		if err != nil {
			return nil, fmt.Errorf("entries[%d]: %w", i, err)
		}
		manifests = append(manifests, d)
	}
	return &Value{
		SchemaVersion:    schemaVersionV2,
		MediaType:        MediaTypeIndex,
		ArtifactType:     ArtifactTypeRelease,
		Manifests:        manifests,
		Annotations:      rootAnn,
		schemaVersionSet: true,
		mediaTypeSet:     true,
		artifactTypeSet:  true,
		manifestsSet:     true,
		annotationsSet:   true,
	}, nil
}

// descriptorFromModel copies one [ModelEntry] into a [Descriptor].
func descriptorFromModel(e ModelEntry) (Descriptor, error) {
	if err := requireUTF8("mediaType", e.MediaType); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("artifactType", e.ArtifactType); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("architecture", e.Selector.Architecture); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("target", e.Selector.Target); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("representation", e.Selector.Representation); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("role", e.Selector.Role); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("compression", e.Selector.Compression); err != nil {
		return Descriptor{}, err
	}
	if err := requireUTF8("filename", e.Filename); err != nil {
		return Descriptor{}, err
	}
	ann, err := copyAnnotations(e.Annotations)
	if err != nil {
		return Descriptor{}, err
	}
	ann[AnnotationArchitecture] = e.Selector.Architecture
	ann[AnnotationTarget] = e.Selector.Target
	ann[AnnotationRepresentation] = e.Selector.Representation
	ann[AnnotationRole] = e.Selector.Role
	ann[AnnotationCompression] = e.Selector.Compression
	ann[AnnotationContentDigest] = e.ContentDigest.String()
	ann[AnnotationContentSize] = strconv.FormatInt(e.ContentSize, 10)
	ann[AnnotationFilename] = e.Filename
	mediaType := e.MediaType
	if mediaType == "" {
		mediaType = MediaTypeManifest
	}
	artifactType := e.ArtifactType
	if artifactType == "" {
		artifactType = ArtifactTypeFile
	}
	return Descriptor{
		MediaType:       mediaType,
		Digest:          e.Digest,
		Size:            e.Size,
		ArtifactType:    artifactType,
		Annotations:     ann,
		mediaTypeSet:    true,
		digestSet:       true,
		sizeSet:         true,
		artifactTypeSet: true,
		annotationsSet:  true,
	}, nil
}

// wireFromValue projects a [Value] onto the producer JSON shape.
func wireFromValue(v *Value) wireIndex {
	manifests := make([]wireDescriptor, 0, len(v.Manifests))
	for _, d := range v.Manifests {
		manifests = append(manifests, wireDescriptor{
			MediaType:    d.MediaType,
			Digest:       d.Digest.String(),
			Size:         d.Size,
			ArtifactType: d.ArtifactType,
			Annotations:  d.Annotations,
		})
	}
	return wireIndex{
		SchemaVersion: v.SchemaVersion,
		MediaType:     v.MediaType,
		ArtifactType:  v.ArtifactType,
		Manifests:     manifests,
		Annotations:   v.Annotations,
	}
}

// copyAnnotations copies annotation keys and values after a UTF-8 check.
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
