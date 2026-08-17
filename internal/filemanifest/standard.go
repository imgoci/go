package filemanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
)

const (
	// MediaTypeEmpty is application/vnd.oci.empty.v1+json.
	MediaTypeEmpty = "application/vnd.oci.empty.v1+json"
	// MediaTypeLayer is application/octet-stream.
	MediaTypeLayer = "application/octet-stream"
	// EmptyConfigDigest is the OCI empty-config blob digest (two bytes `{}`).
	EmptyConfigDigest digest.Digest = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	// EmptyConfigSize is the OCI empty-config blob size.
	EmptyConfigSize int64 = 2
)

const (
	schemaVersionV2     = 2
	sha256HexLength     = 64
	maxLayerSize        = 9007199254740991 // 2^53-1, exact in IEEE-754 binary64.
	jsonTokenObjectOpen = '{'
	jsonTokenArrayOpen  = '['
)

// Standard is a consumer-validated standard imgoci file manifest.
type Standard struct {
	// MediaType is the top-level mediaType as written.
	MediaType string
	// ArtifactType is the top-level artifactType as written.
	ArtifactType string
	// Layer is the single file-layer descriptor.
	Layer Layer
}

// Layer is the file-layer descriptor of a standard file manifest.
type Layer struct {
	// MediaType is the layer mediaType as written.
	MediaType string
	// Digest is the SHA-256 digest of the stored file blob.
	Digest digest.Digest
	// Size is the stored file blob size in bytes.
	Size int64
}

// ValidateStandard applies spec §3.1 consumer validation to a standard file
// manifest.
//
// Grammar decoding runs first (UTF-8, JSON types, duplicate-key rejection at
// every depth). [jcs.Verify] then checks that b is already RFC 8785 canonical,
// using the generic JSON tree so additional members remain visible. Defined
// members are checked against spec §3.1: schemaVersion 2, mediaType and
// artifactType, the empty-config constant, and exactly one
// application/octet-stream layer. Additional members on the manifest, config
// descriptor, or layer descriptor are ignored for imgoci behavior, except that
// an annotations member on any of those three objects must map string keys to
// string values.
func ValidateStandard(b []byte) (*Standard, error) {
	tree, err := decodeJSON(b)
	if err != nil {
		return nil, err
	}
	if err = jcs.Verify(b, tree); err != nil {
		return nil, fmt.Errorf("canonical bytes: %w", err)
	}
	obj, err := asObject(tree, "file manifest")
	if err != nil {
		return nil, err
	}
	return standardFromObject(obj)
}

// standardFromObject maps a JSON object onto a [Standard] and applies §3.1.
func standardFromObject(obj map[string]any) (*Standard, error) {
	schema, err := requiredInt64(obj, "schemaVersion")
	if err != nil {
		return nil, err
	}
	if schema != schemaVersionV2 {
		return nil, fmt.Errorf("schemaVersion must be %d", schemaVersionV2)
	}

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
	if !index.EqualMediaType(artifactType, index.ArtifactTypeFile) {
		return nil, fmt.Errorf("artifactType must identify %s", index.ArtifactTypeFile)
	}

	if err = validateAnnotations(obj, "annotations"); err != nil {
		return nil, err
	}
	if err = validateEmptyConfig(obj); err != nil {
		return nil, err
	}
	layer, err := validateSingleLayer(obj)
	if err != nil {
		return nil, err
	}

	return &Standard{
		MediaType:    mediaType,
		ArtifactType: artifactType,
		Layer:        layer,
	}, nil
}

// validateEmptyConfig requires config to be the OCI empty descriptor.
func validateEmptyConfig(obj map[string]any) error {
	raw, ok := obj["config"]
	if !ok {
		return errors.New("config is required")
	}
	cfg, err := asObject(raw, "config")
	if err != nil {
		return err
	}

	mediaType, err := requiredString(cfg, "mediaType")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if !index.EqualMediaType(mediaType, MediaTypeEmpty) {
		return fmt.Errorf("config mediaType must identify %s", MediaTypeEmpty)
	}

	size, err := requiredInt64(cfg, "size")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if size != EmptyConfigSize {
		return fmt.Errorf("config size must be %d", EmptyConfigSize)
	}

	dgst, err := requiredDigest(cfg, "digest")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if dgst != EmptyConfigDigest {
		return fmt.Errorf("config digest must be %s", EmptyConfigDigest)
	}

	if err = validateAnnotations(cfg, "config.annotations"); err != nil {
		return err
	}
	return nil
}

// validateSingleLayer requires layers to contain exactly one file layer.
func validateSingleLayer(obj map[string]any) (Layer, error) {
	raw, ok := obj["layers"]
	if !ok {
		return Layer{}, errors.New("layers is required")
	}
	arr, err := asArray(raw, "layers")
	if err != nil {
		return Layer{}, err
	}
	if len(arr) != 1 {
		return Layer{}, fmt.Errorf("layers must contain exactly one descriptor, got %d", len(arr))
	}
	layerObj, err := asObject(arr[0], "layers[0]")
	if err != nil {
		return Layer{}, err
	}

	mediaType, err := requiredString(layerObj, "mediaType")
	if err != nil {
		return Layer{}, fmt.Errorf("layers[0]: %w", err)
	}
	if !index.EqualMediaType(mediaType, MediaTypeLayer) {
		return Layer{}, fmt.Errorf("layers[0] mediaType must identify %s", MediaTypeLayer)
	}

	dgst, err := requiredDigest(layerObj, "digest")
	if err != nil {
		return Layer{}, fmt.Errorf("layers[0]: %w", err)
	}
	if err = requireSHA256(dgst); err != nil {
		return Layer{}, fmt.Errorf("layers[0] digest: %w", err)
	}

	size, err := requiredInt64(layerObj, "size")
	if err != nil {
		return Layer{}, fmt.Errorf("layers[0]: %w", err)
	}
	if size < 0 || size > maxLayerSize {
		return Layer{}, fmt.Errorf("layers[0] size must be a JSON integer from 0 through %d", maxLayerSize)
	}

	if err = validateAnnotations(layerObj, "layers[0].annotations"); err != nil {
		return Layer{}, err
	}

	return Layer{MediaType: mediaType, Digest: dgst, Size: size}, nil
}

// validateAnnotations requires annotations, when present, to map string keys to
// string values.
//
// path names the annotations member being validated (for example
// `config.annotations`) so that diagnostics identify which object failed.
// Spec §3.1 applies this rule to the manifest, the config descriptor, and the
// file-layer descriptor alike.
func validateAnnotations(obj map[string]any, path string) error {
	raw, ok := obj["annotations"]
	if !ok {
		return nil
	}
	ann, err := asObject(raw, path)
	if err != nil {
		return err
	}
	for k, v := range ann {
		if _, err := asString(v, path+"["+k+"]"); err != nil {
			return err
		}
	}
	return nil
}

// requiredString copies a required JSON string member.
func requiredString(obj map[string]any, key string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	return asString(raw, key)
}

// requiredInt64 copies a required JSON integer member.
func requiredInt64(obj map[string]any, key string) (int64, error) {
	raw, ok := obj[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	return asInt64(raw, key)
}

// requiredDigest copies a required digest string member.
func requiredDigest(obj map[string]any, key string) (digest.Digest, error) {
	s, err := requiredString(obj, key)
	if err != nil {
		return "", err
	}
	dgst, err := digest.Parse(s)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return dgst, nil
}

// requireSHA256 reports an error when dgst is not sha256 plus 64 lowercase hex
// digits.
func requireSHA256(dgst digest.Digest) error {
	if dgst.Algorithm() != digest.SHA256 {
		return fmt.Errorf("must be sha256: followed by %d lowercase hex digits", sha256HexLength)
	}
	if len(dgst.Encoded()) != sha256HexLength {
		return fmt.Errorf("must be sha256: followed by %d lowercase hex digits", sha256HexLength)
	}
	return nil
}

// decodeJSON parses b into a generic JSON tree and rejects duplicate keys.
func decodeJSON(b []byte) (any, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("file manifest bytes are not valid UTF-8")
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
	if s[i] < '1' || s[i] > '9' {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
