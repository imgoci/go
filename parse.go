package imgoci

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

// ParseIndex fully validates release-index bytes: JSON decode with duplicate-key
// rejection, the ten consumer rules of spec section 6 including canonical
// descriptor order (rule 9), and canonical bytes (rule 10).
//
// The three codec seams run in this order: Decode, then Validate, then
// VerifyCanonical. That order is a correctness requirement. The canonical
// transform is not a JSON grammar validator, so grammar and duplicate-key
// checks must happen first.
//
// ParseIndex never re-encodes for identity. The returned [Index] records the
// SHA-256 digest of the input bytes. On any failure the error wraps
// [ErrInvalidIndex].
func ParseIndex(b []byte) (*Index, error) {
	value, err := index.Decode(b)
	if err != nil {
		return nil, invalidIndex(err)
	}
	if err := index.Validate(value); err != nil {
		return nil, invalidIndex(err)
	}
	if err := index.VerifyCanonical(b); err != nil {
		return nil, invalidIndex(err)
	}
	return indexFromValue(value, digest.FromBytes(b)), nil
}

// invalidIndex wraps err with [ErrInvalidIndex] so [errors.Is] matches the
// public sentinel while the message keeps the underlying detail.
func invalidIndex(err error) error {
	//nolint:errorlint // "%w: %v" is the required wrapping form so errors.Is matches ErrInvalidIndex.
	return fmt.Errorf("%w: %v", ErrInvalidIndex, err)
}
