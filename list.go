package imgoci

import (
	"errors"

	"github.com/imgoci/go/internal/index"
)

// ListQuery selects deliverables from a validated [Index]. Empty scalar fields
// match every value. A nil Usage slice applies no usage filter and matches
// every usage set. A non-nil Usage slice must be non-empty and free of
// duplicates; a deliverable matches only when its usage set contains every
// requested value. A nil Roles slice applies no role filter; a non-nil Roles
// slice must be non-empty and names every role a matching deliverable must
// contain.
type ListQuery struct {
	// Architecture is an exact, case-sensitive architecture filter. The empty
	// string matches every architecture.
	Architecture string
	// Target is an exact, case-sensitive target filter. The empty string
	// matches every target.
	Target string
	// Representation is an exact, case-sensitive representation filter. The
	// empty string matches every representation.
	Representation string
	// Usage, when non-nil, requires a matching deliverable's usage set to
	// contain every requested value (spec section 7.2). Nil applies no usage
	// filter and matches every usage set. A non-nil empty slice is invalid
	// per spec section 7.1. Order is irrelevant.
	Usage []string
	// Roles, when non-nil, requires every listed role to be present on a
	// matching deliverable. Nil means no role filter. A non-nil empty slice is
	// invalid per spec section 7.1.
	Roles []string
}

// Deliverable is one unique (architecture, target, representation, usage)
// group returned by [Index.List]. Spec section 7.2 defines a deliverable as
// all file entries that share that four-field key.
type Deliverable struct {
	// Architecture is the deliverable's architecture selector.
	Architecture string
	// Target is the deliverable's target selector.
	Target string
	// Representation is the deliverable's representation selector.
	Representation string
	// Usage is the deliverable's exact usage set, not the list-query filter
	// (spec section 7.2).
	Usage Usage
	// Roles lists every role in the deliverable, sorted by role in ascending
	// UTF-8 byte order. Each role includes its transport alternatives.
	Roles []DeliverableRole
}

// DeliverableRole is one file inside a listed [Deliverable], identified by its
// role selector and the transport alternatives stored for that file.
type DeliverableRole struct {
	// Role is the io.imgoci.role value.
	Role string
	// Alternatives lists stored encodings of this file, sorted by compression
	// in ascending UTF-8 byte order. Listing does not drop alternatives whose
	// file-manifest type the consumer cannot retrieve.
	Alternatives []TransportAlternative
}

// TransportAlternative is one stored encoding of a file: a compression and the
// file-manifest type declared on that descriptor.
type TransportAlternative struct {
	// Compression is the io.imgoci.compression value.
	Compression string
	// ArtifactType is the descriptor artifactType (the file-manifest type).
	ArtifactType string
}

// List returns every deliverable that matches q, including each role and its
// transport alternatives, sorted per spec section 7.2. An empty result is
// valid. Listing does not filter by consumer capabilities.
//
// List validates q completely before it inspects a single index entry, which
// is after the caller's [Client.Fetch] rather than before it; see
// [Client.Fetch] for that deviation from spec section 7.1.
func (x *Index) List(q ListQuery) ([]Deliverable, error) {
	if x == nil {
		return nil, errors.New("list: nil index")
	}
	listed, err := index.List(x.entries, index.ListQuery{
		Architecture:   q.Architecture,
		Target:         q.Target,
		Representation: q.Representation,
		Usage:          q.Usage,
		Roles:          q.Roles,
	})
	if err != nil {
		return nil, err
	}
	return deliverablesFromListed(listed), nil
}

// deliverablesFromListed maps the internal listed deliverables onto the public
// tree.
//
// Every role slice and alternative slice is cut from one backing array each,
// sized by a non-allocating pre-pass, so the mapping costs three allocations
// for the whole result instead of two per deliverable. Each cut uses a full
// slice expression, so cap equals len and a caller appending to one slice
// cannot reach a neighbour's elements. The consequence to know is retention:
// holding one [Deliverable] keeps both arrays alive, which suits a result set
// callers consume whole.
func deliverablesFromListed(listed []index.Listed) []Deliverable {
	totalRoles, totalAlts := 0, 0
	for _, d := range listed {
		totalRoles += len(d.Roles)
		for _, role := range d.Roles {
			totalAlts += len(role.Alternatives)
		}
	}
	roleArena := make([]DeliverableRole, totalRoles)
	altArena := make([]TransportAlternative, totalAlts)

	out := make([]Deliverable, len(listed))
	for i, d := range listed {
		roles := roleArena[:len(d.Roles):len(d.Roles)]
		roleArena = roleArena[len(d.Roles):]
		for j, role := range d.Roles {
			alts := altArena[:len(role.Alternatives):len(role.Alternatives)]
			altArena = altArena[len(role.Alternatives):]
			for k, alt := range role.Alternatives {
				alts[k] = TransportAlternative{
					Compression:  alt.Compression,
					ArtifactType: alt.ArtifactType,
				}
			}
			roles[j] = DeliverableRole{Role: role.Role, Alternatives: alts}
		}
		out[i] = Deliverable{
			Architecture:   d.Architecture,
			Target:         d.Target,
			Representation: d.Representation,
			Usage:          usageFromCanonical(d.Usage),
			Roles:          roles,
		}
	}

	return out
}
