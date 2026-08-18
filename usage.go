package imgoci

import (
	"fmt"

	"github.com/imgoci/go/internal/index"
)

// Usage is a deliverable usage set: the producer-asserted ways in which a
// deliverable can be used, such as live, install, or install-offline.
//
// The zero Usage is the empty set, which is how a release index spells a
// deliverable whose descriptors omit io.imgoci.usage. A Usage is immutable and
// comparable, so it can be used in a map key and compared with ==. Two Usage
// values are equal exactly when they hold the same set, because [NewUsage]
// sorts and de-duplicates the tokens it is given.
//
// Usage values are producer assertions. Validation and retrieval never execute
// a deliverable, so imgoci cannot prove that one behaves as declared.
type Usage struct {
	// canonical is the spec section 5.3 serialized form: sorted, unique,
	// comma-separated basic tokens, or empty for the empty set.
	canonical string
}

// NewUsage returns the usage set containing values.
//
// The tokens may be supplied in any order and may repeat; the result is sorted
// and de-duplicated, which is the canonical form the wire format requires.
// Calling NewUsage with no values returns the empty set.
//
// It fails when a token is not a spec section 5.3 basic token (1 to 128 ASCII
// bytes matching ^[a-z0-9]+([._-][a-z0-9]+)*$), when the serialized set would
// exceed 4096 bytes, or when the set contains install-offline without install,
// which spec section 5.4 requires a consumer to reject.
func NewUsage(values ...string) (Usage, error) {
	canonical, err := index.CanonicalizeUsage(values)
	if err != nil {
		return Usage{}, fmt.Errorf("usage: %w", err)
	}
	if err := index.ValidateUsageRelationship(canonical); err != nil {
		return Usage{}, fmt.Errorf("usage: %w", err)
	}

	return Usage{canonical: canonical}, nil
}

// String returns the canonical serialized set, such as "install,install-offline".
// The empty set is the empty string.
func (u Usage) String() string {
	return u.canonical
}

// Values returns the tokens of the set in ascending UTF-8 byte order. The
// empty set returns nil. The slice is freshly allocated on every call.
func (u Usage) Values() []string {
	return index.UsageValues(u.canonical)
}

// usageFromCanonical wraps a value that a validated release index already
// proved canonical. It never re-validates: [index.Validate] rejected any
// noncanonical io.imgoci.usage annotation before the entry reached this point.
func usageFromCanonical(canonical string) Usage {
	return Usage{canonical: canonical}
}
