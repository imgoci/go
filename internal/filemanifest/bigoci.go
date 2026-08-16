package filemanifest

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

const (
	// MediaTypePart is application/vnd.bigoci.file.part.v1.
	MediaTypePart = "application/vnd.bigoci.file.part.v1"

	annotationFileDigest = "io.bigoci.file.digest"
	annotationFileSize   = "io.bigoci.file.size"
	minBigOCIParts       = 2
)

// BigOCIProfile is a consumer-validated imgoci BigOCI file-manifest profile.
//
// A 1-part artifact is a valid bigoci file but not a valid imgoci profile:
// spec §8 requires at least two parts. Extra members the profile does not
// constrain — including bigoci's own part-size and title annotations — are
// ignored for imgoci behavior.
type BigOCIProfile struct {
	// MediaType is the top-level mediaType as written.
	MediaType string
	// ArtifactType is the top-level artifactType as written.
	ArtifactType string
	// FileDigest is the full io.bigoci.file.digest annotation.
	FileDigest digest.Digest
	// FileSize is the io.bigoci.file.size annotation, a decimal int64.
	FileSize int64
}

// ValidateBigOCI applies the imgoci BigOCI file-manifest profile to b.
//
// Grammar decoding runs first (UTF-8, JSON types, duplicate-key rejection).
// The profile then requires an OCI image-manifest mediaType, a bigoci file
// artifactType, at least two parts, and the io.bigoci.file.{digest,size}
// annotations. Media and artifact types are compared with
// [index.EqualMediaType]. Canonical-bytes verification is not applied:
// BigOCI manifests use BigOCI File Format v1 encoding, which imgoci must
// not rewrite.
func ValidateBigOCI(b []byte) (*BigOCIProfile, error) {
	tree, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	obj, err := asObject(tree, "file manifest")
	if err != nil {
		return nil, err
	}
	return bigOCIFromObject(obj)
}

// bigOCIFromObject maps a JSON object onto a [BigOCIProfile] and applies the
// imgoci profile checks.
func bigOCIFromObject(obj map[string]any) (*BigOCIProfile, error) {
	mediaType, err := requiredString(obj, "mediaType")
	if err != nil {
		return nil, err
	}
	if !index.EqualMediaType(mediaType, index.MediaTypeManifest) {
		return nil, fmt.Errorf("mediaType must identify %s", index.MediaTypeManifest)
	}

	artifactType, err := requiredString(obj, "artifactType")
	if err != nil {
		return nil, err
	}
	if !index.EqualMediaType(artifactType, index.ArtifactTypeBigOCI) {
		return nil, fmt.Errorf("artifactType must identify %s", index.ArtifactTypeBigOCI)
	}

	if err = validateBigOCILayers(obj); err != nil {
		return nil, err
	}
	fileDigest, fileSize, err := extractBigOCIFileAnnotations(obj)
	if err != nil {
		return nil, err
	}

	return &BigOCIProfile{
		MediaType:    mediaType,
		ArtifactType: artifactType,
		FileDigest:   fileDigest,
		FileSize:     fileSize,
	}, nil
}

// validateBigOCILayers requires layers to contain at least [minBigOCIParts]
// part descriptors. Extra members on a descriptor are ignored. Layer media
// types are compared with [index.EqualMediaType].
func validateBigOCILayers(obj map[string]any) error {
	raw, ok := obj["layers"]
	if !ok {
		return errors.New("layers is required")
	}
	arr, err := asArray(raw, "layers")
	if err != nil {
		return err
	}
	if len(arr) < minBigOCIParts {
		return fmt.Errorf("imgoci BigOCI profile requires at least %d parts, got %d", minBigOCIParts, len(arr))
	}
	for i, item := range arr {
		layer, err := asObject(item, fmt.Sprintf("layers[%d]", i))
		if err != nil {
			return err
		}
		mediaType, err := requiredString(layer, "mediaType")
		if err != nil {
			return fmt.Errorf("layers[%d]: %w", i, err)
		}
		if !index.EqualMediaType(mediaType, MediaTypePart) {
			return fmt.Errorf("layers[%d] mediaType must identify %s", i, MediaTypePart)
		}
	}
	return nil
}

// extractBigOCIFileAnnotations copies io.bigoci.file.digest and
// io.bigoci.file.size. The digest is the full sha256 digest; the size is a
// decimal int64 string. Other annotations are ignored.
func extractBigOCIFileAnnotations(obj map[string]any) (digest.Digest, int64, error) {
	raw, ok := obj["annotations"]
	if !ok {
		return "", 0, errors.New("annotations is required")
	}
	ann, err := asObject(raw, "annotations")
	if err != nil {
		return "", 0, err
	}
	for k, v := range ann {
		if _, annErr := asString(v, "annotations["+k+"]"); annErr != nil {
			return "", 0, annErr
		}
	}

	digestStr, err := requiredString(ann, annotationFileDigest)
	if err != nil {
		return "", 0, err
	}
	fileDigest, err := digest.Parse(digestStr)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", annotationFileDigest, err)
	}
	if err = requireSHA256(fileDigest); err != nil {
		return "", 0, fmt.Errorf("%s: %w", annotationFileDigest, err)
	}

	sizeStr, err := requiredString(ann, annotationFileSize)
	if err != nil {
		return "", 0, err
	}
	if !isJSONInteger(sizeStr) {
		return "", 0, fmt.Errorf("%s must be a decimal int64", annotationFileSize)
	}
	fileSize, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", annotationFileSize, err)
	}
	if fileSize < 0 {
		return "", 0, fmt.Errorf("%s must be a decimal int64", annotationFileSize)
	}

	return fileDigest, fileSize, nil
}
