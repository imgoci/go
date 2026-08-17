package imgoci

import (
	"fmt"
	"strings"

	"github.com/imgoci/go/internal/index"
)

// standardFileMediaType is the imgoci v1 standard file-manifest type. A
// consumer capability set must include it.
const standardFileMediaType = "application/vnd.imgoci.file.v1"

// bigociFileMediaType is the imgoci BigOCI file-manifest type.
const bigociFileMediaType = "application/vnd.bigoci.file.v1"

// Capabilities is a validated set of file-manifest types a consumer can
// retrieve. The set is normalized to ASCII-lowercase, parameter-free RFC 6838
// type/subtype values with duplicates removed.
//
// A zero Capabilities means "standard only": [Index.Resolve] treats it as
// [StandardCapabilities]. BigOCI is never assumed.
type Capabilities struct {
	// types holds the normalized, lowercase, unique file-manifest types.
	types []string
}

// NewCapabilities validates types as a consumer capability set. The set must
// include application/vnd.imgoci.file.v1 case-insensitively, must not contain
// duplicates after ASCII case folding, must not contain parameters, and every
// value must be an RFC 6838 type/subtype.
func NewCapabilities(types ...string) (Capabilities, error) {
	normalized := make([]string, 0, len(types))
	seen := make(map[string]struct{}, len(types))
	hasStandard := false

	for _, raw := range types {
		if strings.Contains(raw, ";") {
			return Capabilities{}, fmt.Errorf("capability %q: media types must not contain parameters", raw)
		}
		if !index.IsMediaType(raw) {
			return Capabilities{}, fmt.Errorf("capability %q: not an RFC 6838 type/subtype", raw)
		}
		folded := index.ASCIILower(raw)
		if _, ok := seen[folded]; ok {
			return Capabilities{}, fmt.Errorf("capability %q: duplicate after ASCII case folding", raw)
		}
		seen[folded] = struct{}{}
		normalized = append(normalized, folded)
		if EqualMediaType(folded, standardFileMediaType) {
			hasStandard = true
		}
	}
	if !hasStandard {
		return Capabilities{}, fmt.Errorf("capability set must include %s", standardFileMediaType)
	}

	return Capabilities{types: normalized}, nil
}

// StandardCapabilities returns the capability set containing only
// application/vnd.imgoci.file.v1. This is the zero-value default everywhere.
func StandardCapabilities() Capabilities {
	return Capabilities{types: []string{standardFileMediaType}}
}

// effective returns c, or [StandardCapabilities] when c is the zero value.
func (c Capabilities) effective() Capabilities {
	if len(c.types) == 0 {
		return StandardCapabilities()
	}
	return c
}

// supports reports whether mediaType is in the effective capability set under
// spec section 4 comparison.
func (c Capabilities) supports(mediaType string) bool {
	return supportsType(c.effective().types, mediaType)
}

// supportsType reports whether mediaType is in types under [EqualMediaType].
func supportsType(types []string, mediaType string) bool {
	for _, candidate := range types {
		if EqualMediaType(candidate, mediaType) {
			return true
		}
	}
	return false
}
