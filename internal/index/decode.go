package index

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"
)

// Media types and artifact types from imgoci spec §4.
const (
	// MediaTypeIndex is application/vnd.oci.image.index.v1+json.
	MediaTypeIndex = "application/vnd.oci.image.index.v1+json"
	// MediaTypeManifest is application/vnd.oci.image.manifest.v1+json.
	MediaTypeManifest = "application/vnd.oci.image.manifest.v1+json"
	// ArtifactTypeRelease is application/vnd.imgoci.release.v1.
	ArtifactTypeRelease = "application/vnd.imgoci.release.v1"
	// ArtifactTypeFile is application/vnd.imgoci.file.v1.
	ArtifactTypeFile = "application/vnd.imgoci.file.v1"
	// ArtifactTypeBigOCI is application/vnd.bigoci.file.v1.
	ArtifactTypeBigOCI = "application/vnd.bigoci.file.v1"
)

// Annotation keys defined for the release index and file-entry descriptors.
const (
	// AnnotationName is io.imgoci.name.
	AnnotationName = "io.imgoci.name"
	// AnnotationVersion is org.opencontainers.image.version.
	AnnotationVersion = "org.opencontainers.image.version"
	// AnnotationArchitecture is io.imgoci.architecture.
	AnnotationArchitecture = "io.imgoci.architecture"
	// AnnotationTarget is io.imgoci.target.
	AnnotationTarget = "io.imgoci.target"
	// AnnotationRepresentation is io.imgoci.representation.
	AnnotationRepresentation = "io.imgoci.representation"
	// AnnotationRole is io.imgoci.role.
	AnnotationRole = "io.imgoci.role"
	// AnnotationCompression is io.imgoci.compression.
	AnnotationCompression = "io.imgoci.compression"
	// AnnotationContentDigest is io.imgoci.content.digest.
	AnnotationContentDigest = "io.imgoci.content.digest"
	// AnnotationContentSize is io.imgoci.content.size.
	AnnotationContentSize = "io.imgoci.content.size"
	// AnnotationFilename is io.imgoci.filename.
	AnnotationFilename = "io.imgoci.filename"
)

const (
	schemaVersionV2     = 2
	maxBasicTokenBytes  = 128
	maxReleaseVersion   = 128
	maxFilenameBytes    = 255
	sha256HexLength     = 64
	minManifestSize     = 1
	maxManifestSize     = 9007199254740991 // 2^53-1, exact in IEEE-754 binary64.
	jsonTokenObjectOpen = '{'
	jsonTokenArrayOpen  = '['
)

// Spec §6 consumer-validation rule numbers named in errors.
const (
	specRuleRoot           = 1
	specRuleDescriptor     = 2
	specRuleSyntax         = 3
	specRuleRoles          = 4
	specRuleSelector       = 5
	specRuleFileIdentity   = 6
	specRuleFilename       = 7
	specRuleSharedManifest = 8
	specRuleOrder          = 9
	specRuleCanonical      = 10
)

// Value is a decoded release index. Unknown top-level and descriptor members
// are ignored for imgoci behavior and are not stored here; [VerifyCanonical]
// inspects original bytes so those members still participate in rule 10.
type Value struct {
	// SchemaVersion is the OCI image-index schema version.
	SchemaVersion int64
	// MediaType is the top-level media type as written.
	MediaType string
	// ArtifactType is the top-level artifact type as written.
	ArtifactType string
	// Manifests are the file-entry descriptors in document order.
	Manifests []Descriptor
	// Annotations is the top-level annotation map, including unknown keys.
	Annotations map[string]string
	// schemaVersionSet reports whether schemaVersion was present in the JSON.
	schemaVersionSet bool
	// mediaTypeSet reports whether mediaType was present in the JSON.
	mediaTypeSet bool
	// artifactTypeSet reports whether artifactType was present in the JSON.
	artifactTypeSet bool
	// manifestsSet reports whether manifests was present in the JSON.
	manifestsSet bool
	// annotationsSet reports whether annotations was present in the JSON.
	annotationsSet bool
}

// Descriptor is one file-entry descriptor in a release index.
type Descriptor struct {
	// MediaType is the descriptor media type as written.
	MediaType string
	// Digest is the SHA-256 digest of the referenced file manifest.
	Digest digest.Digest
	// Size is the byte length of the referenced file manifest.
	Size int64
	// ArtifactType is the referenced manifest's artifact type as written.
	ArtifactType string
	// Annotations is the descriptor annotation map, including unknown keys.
	Annotations map[string]string
	// mediaTypeSet reports whether mediaType was present in the JSON.
	mediaTypeSet bool
	// digestSet reports whether digest was present in the JSON.
	digestSet bool
	// sizeSet reports whether size was present in the JSON.
	sizeSet bool
	// artifactTypeSet reports whether artifactType was present in the JSON.
	artifactTypeSet bool
	// annotationsSet reports whether annotations was present in the JSON.
	annotationsSet bool
}

// Selector is the five-field identity of a transport alternative.
type Selector struct {
	// Architecture is io.imgoci.architecture.
	Architecture string
	// Target is io.imgoci.target.
	Target string
	// Representation is io.imgoci.representation.
	Representation string
	// Role is io.imgoci.role.
	Role string
	// Compression is io.imgoci.compression.
	Compression string
}

// Decode parses b as a release-index JSON document.
//
// It requires [utf8.Valid] input, rejects duplicate object keys at every depth
// after JSON string decoding (`"\u0061"` duplicates `"a"`), and requires known
// members to have the JSON types specified by the spec. Unknown members are
// never rejected. Descriptor digest strings are parsed with
// [digest.Parse].
func Decode(b []byte) (*Value, error) {
	raw, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	obj, err := asObject(raw, "release index")
	if err != nil {
		return nil, err
	}
	return valueFromObject(obj)
}

// Selector returns the five-field selector read from the descriptor annotations.
func (d Descriptor) Selector() Selector {
	return Selector{
		Architecture:   d.annotation(AnnotationArchitecture),
		Target:         d.annotation(AnnotationTarget),
		Representation: d.annotation(AnnotationRepresentation),
		Role:           d.annotation(AnnotationRole),
		Compression:    d.annotation(AnnotationCompression),
	}
}

// ContentDigest returns io.imgoci.content.digest parsed as a digest.
// An invalid or missing value yields the zero digest.
func (d Descriptor) ContentDigest() digest.Digest {
	parsed, err := digest.Parse(d.annotation(AnnotationContentDigest))
	if err != nil {
		return ""
	}
	return parsed
}

// ContentSize returns io.imgoci.content.size parsed as an int64.
// An invalid or missing value yields 0.
func (d Descriptor) ContentSize() int64 {
	n, err := strconv.ParseInt(d.annotation(AnnotationContentSize), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// Filename returns the io.imgoci.filename annotation.
func (d Descriptor) Filename() string {
	return d.annotation(AnnotationFilename)
}

// annotation returns the annotation value for key, or empty if missing.
func (d Descriptor) annotation(key string) string {
	if d.Annotations == nil {
		return ""
	}
	return d.Annotations[key]
}

// decodeJSON parses b into a generic JSON tree and rejects duplicate keys.
func decodeJSON(b []byte) (any, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("index bytes are not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	raw, err := decodeJSONValue(dec)
	if err != nil {
		return nil, err
	}
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return raw, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("trailing JSON token %v", tok)
}

// decodeJSONValue reads one JSON value from dec.
func decodeJSONValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case jsonTokenObjectOpen:
			return decodeJSONObject(dec)
		case jsonTokenArrayOpen:
			return decodeJSONArray(dec)
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", t)
		}
	case bool, string, json.Number, nil:
		return t, nil
	default:
		return nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

// decodeJSONObject reads a JSON object, comparing keys after string decoding.
func decodeJSONObject(dec *json.Decoder) (map[string]any, error) {
	obj := make(map[string]any)
	seen := make(map[string]struct{})
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is %T, want string", tok)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate object key %q", key)
		}
		seen[key] = struct{}{}
		val, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	if err := consumeDelim(dec, '}'); err != nil {
		return nil, err
	}
	return obj, nil
}

// decodeJSONArray reads a JSON array.
func decodeJSONArray(dec *json.Decoder) ([]any, error) {
	var arr []any
	for dec.More() {
		val, err := decodeJSONValue(dec)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	if err := consumeDelim(dec, ']'); err != nil {
		return nil, err
	}
	return arr, nil
}

// consumeDelim reads the next token and requires it to equal want.
func consumeDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	got, ok := tok.(json.Delim)
	if !ok || got != want {
		return fmt.Errorf("expected delimiter %q, got %v", want, tok)
	}
	return nil
}

// valueFromObject maps a generic JSON object onto a [Value].
func valueFromObject(obj map[string]any) (*Value, error) {
	v := &Value{}
	if err := assignOptionalInt64(obj, "schemaVersion", &v.SchemaVersion, &v.schemaVersionSet); err != nil {
		return nil, err
	}
	if err := assignOptionalString(obj, "mediaType", &v.MediaType, &v.mediaTypeSet); err != nil {
		return nil, err
	}
	if err := assignOptionalString(obj, "artifactType", &v.ArtifactType, &v.artifactTypeSet); err != nil {
		return nil, err
	}
	if err := assignOptionalAnnotations(obj, "annotations", &v.Annotations, &v.annotationsSet); err != nil {
		return nil, err
	}
	if err := assignOptionalManifests(obj, &v.Manifests, &v.manifestsSet); err != nil {
		return nil, err
	}
	return v, nil
}

// descriptorFromObject maps a generic JSON object onto a [Descriptor].
func descriptorFromObject(obj map[string]any, path string) (Descriptor, error) {
	var d Descriptor
	if err := assignOptionalString(obj, "mediaType", &d.MediaType, &d.mediaTypeSet); err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := assignOptionalString(obj, "artifactType", &d.ArtifactType, &d.artifactTypeSet); err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := assignOptionalInt64(obj, "size", &d.Size, &d.sizeSet); err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := assignOptionalAnnotations(obj, "annotations", &d.Annotations, &d.annotationsSet); err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", path, err)
	}
	raw, ok := obj["digest"]
	if !ok {
		return d, nil
	}
	s, err := asString(raw, "digest")
	if err != nil {
		return Descriptor{}, fmt.Errorf("%s: %w", path, err)
	}
	parsed, err := digest.Parse(s)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%s: digest: %w", path, err)
	}
	d.Digest = parsed
	d.digestSet = true
	return d, nil
}

// assignOptionalString copies a JSON string member when present.
func assignOptionalString(obj map[string]any, key string, dst *string, set *bool) error {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	s, err := asString(raw, key)
	if err != nil {
		return err
	}
	*dst = s
	*set = true
	return nil
}

// assignOptionalInt64 copies a JSON integer member when present.
func assignOptionalInt64(obj map[string]any, key string, dst *int64, set *bool) error {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	n, err := asInt64(raw, key)
	if err != nil {
		return err
	}
	*dst = n
	*set = true
	return nil
}

// assignOptionalAnnotations copies a JSON object of strings when present.
func assignOptionalAnnotations(obj map[string]any, key string, dst *map[string]string, set *bool) error {
	raw, ok := obj[key]
	if !ok {
		return nil
	}
	ann, err := annotationsFromValue(raw, key)
	if err != nil {
		return err
	}
	*dst = ann
	*set = true
	return nil
}

// assignOptionalManifests copies the manifests array when present.
func assignOptionalManifests(obj map[string]any, dst *[]Descriptor, set *bool) error {
	raw, ok := obj["manifests"]
	if !ok {
		return nil
	}
	arr, err := asArray(raw, "manifests")
	if err != nil {
		return err
	}
	out := make([]Descriptor, 0, len(arr))
	for i, item := range arr {
		descObj, err := asObject(item, fmt.Sprintf("manifests[%d]", i))
		if err != nil {
			return err
		}
		d, err := descriptorFromObject(descObj, fmt.Sprintf("manifests[%d]", i))
		if err != nil {
			return err
		}
		out = append(out, d)
	}
	*dst = out
	*set = true
	return nil
}

// annotationsFromValue converts a JSON object into a string map.
func annotationsFromValue(raw any, field string) (map[string]string, error) {
	obj, err := asObject(raw, field)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		s, err := asString(v, field+"["+k+"]")
		if err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, nil
}

// asObject requires v to be a JSON object.
func asObject(v any, field string) (map[string]any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", field)
	}
	return obj, nil
}

// asArray requires v to be a JSON array.
func asArray(v any, field string) ([]any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON array", field)
	}
	return arr, nil
}

// asString requires v to be a JSON string.
func asString(v any, field string) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a JSON string", field)
	}
	return s, nil
}

// asInt64 requires v to be a JSON integer that fits in int64.
func asInt64(v any, field string) (int64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("%s must be a JSON integer", field)
	}
	s := n.String()
	if !isJSONInteger(s) {
		return 0, fmt.Errorf("%s must be a JSON integer", field)
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	return i, nil
}

// isJSONInteger reports whether s is a JSON integer token without a fraction.
func isJSONInteger(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		if len(s) == 1 {
			return false
		}
		i = 1
	}
	if s[i] == '0' {
		return i+1 == len(s)
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
