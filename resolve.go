package imgoci

import (
	"errors"
	"fmt"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

// ResolveQuery selects one deliverable from a validated [Index]. Architecture,
// Target, Representation, and Usage are required exact matches. Nil and empty
// Usage both request the empty usage set. A nil Roles slice applies the spec
// section 7.3 default-role rule; a non-nil Roles slice must be non-empty and
// selects exactly those roles. Compressions is a required preference-ordered
// list. A zero Capabilities means [StandardCapabilities].
type ResolveQuery struct {
	// Architecture is the required exact architecture selector.
	Architecture string
	// Target is the required exact target selector.
	Target string
	// Representation is the required exact representation selector.
	Representation string
	// Usage is the complete requested usage set, compared for exact equality
	// with a deliverable's usage set. Nil and an empty slice both mean the
	// empty usage set, which is what a deliverable whose descriptors omit
	// io.imgoci.usage has. Duplicates are rejected. Order is irrelevant
	// because the library canonicalizes the list before matching.
	Usage []string
	// Roles, when non-nil, selects exactly those roles. Nil applies the
	// default-role rule. A non-nil empty slice is invalid per spec section 7.1.
	Roles []string
	// Compressions is the required accepted-compression list, most preferred
	// first, with no duplicates.
	Compressions []string
	// Capabilities is the consumer's supported file-manifest types. The zero
	// value means [StandardCapabilities].
	Capabilities Capabilities
}

// Resolved is the atomic result of [Index.Resolve]. It carries the selected
// file entries and the canonical digest of the index they were selected from.
// FetchFiles later binds by that digest, not by pointer identity.
type Resolved struct {
	// digest is the source index digest recorded at parse time.
	digest digest.Digest
	// entries are the selected file entries, one per resolved role.
	entries []FileEntry
}

// Entries returns the selected file entries. The slice and every entry's
// Annotations map are freshly copied on every call.
func (r *Resolved) Entries() []FileEntry {
	if r == nil {
		return nil
	}
	return cloneEntries(r.entries)
}

// IndexDigest returns the SHA-256 digest of the canonical index bytes this
// selection was derived from. Retrieval binds a [Resolved] to a release by
// this digest, not by pointer identity.
func (r *Resolved) IndexDigest() digest.Digest {
	if r == nil {
		return ""
	}
	return r.digest
}

// Resolve selects one deliverable from x according to spec section 7.3.
// Selection is atomic: each step completes for every selected role before the
// next step starts, and any failure returns a nil result with no partial
// entries.
//
// Offline selection failures (no deliverable, a selected role absent from that
// deliverable, or no accepted compression) return a descriptive error without a
// matchable sentinel. Only the capability filter wraps [ErrUnsupportedType].
//
// Resolve validates q completely before it inspects a single index entry, which
// is after the caller's [Client.Fetch] rather than before it; see
// [Client.Fetch] for that deviation from spec section 7.1.
func (x *Index) Resolve(q ResolveQuery) (*Resolved, error) {
	if x == nil {
		return nil, errors.New("resolve: nil index")
	}
	chosen, err := index.Resolve(x.entries, index.ResolveQuery{
		Architecture:   q.Architecture,
		Target:         q.Target,
		Representation: q.Representation,
		Usage:          q.Usage,
		Roles:          q.Roles,
		Compressions:   q.Compressions,
		Capabilities:   q.Capabilities.effective().types,
	})
	if err != nil {
		var capErr *index.CapabilityError
		if errors.As(err, &capErr) {
			return nil, fmt.Errorf("%w: %w", ErrUnsupportedType, err)
		}
		return nil, err
	}
	return &Resolved{digest: x.digest, entries: fileEntriesFrom(chosen)}, nil
}
