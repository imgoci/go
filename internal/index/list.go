package index

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// ListQuery selects deliverables from a validated index. Empty scalar fields
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

// Alternative is one stored encoding of a file: a compression and the
// file-manifest type declared on that descriptor.
type Alternative struct {
	// Compression is the io.imgoci.compression value.
	Compression string
	// ArtifactType is the descriptor artifactType (the file-manifest type).
	ArtifactType string
}

// ListedRole is one file inside a [Listed], identified by its
// role selector and the transport alternatives stored for that file.
type ListedRole struct {
	// Role is the io.imgoci.role value.
	Role string
	// Alternatives lists stored encodings of this file, sorted by compression
	// in ascending UTF-8 byte order. Listing does not drop alternatives whose
	// file-manifest type the consumer cannot retrieve.
	Alternatives []Alternative
}

// Listed is one unique (architecture, target, representation, usage)
// group returned by [List]. Spec section 7.2 defines a deliverable as
// all file entries that share that four-field key. Usage is the
// canonical serialized set.
type Listed struct {
	// Architecture is the deliverable's architecture selector.
	Architecture string
	// Target is the deliverable's target selector.
	Target string
	// Representation is the deliverable's representation selector.
	Representation string
	// Usage is the deliverable's exact usage set, not the list-query filter
	// (spec section 7.2).
	Usage string
	// Roles lists every role in the deliverable, sorted by role in ascending
	// UTF-8 byte order. Each role includes its transport alternatives.
	Roles []ListedRole
}

// listedGroup accumulates roles and alternatives for one deliverable key.
type listedGroup struct {
	// architecture is the deliverable architecture.
	architecture string
	// target is the deliverable target.
	target string
	// representation is the deliverable representation.
	representation string
	// usage is the deliverable's exact usage set.
	usage string
	// roles maps role name to compression-sorted alternatives as they arrive.
	roles map[string][]Alternative
}

// List returns every deliverable in entries that matches q, including each
// role and its transport alternatives, sorted per spec section 7.2. An empty
// result is valid. Listing does not filter by consumer capabilities.
//
// List validates q completely before it inspects a single entry, which
// is after the caller has retrieved the index rather than before it. That
// ordering is a deviation from spec section 7.1.
func List(entries []Entry, q ListQuery) ([]Listed, error) {
	usageFilter, err := validateListQuery(q)
	if err != nil {
		return nil, err
	}

	groups := make(map[deliverableKey]*listedGroup)
	order := make([]deliverableKey, 0)
	for _, e := range entries {
		if !matchesListScalars(e.Selector, q) {
			continue
		}
		if !UsageContainsAll(e.Selector.Usage, usageFilter) {
			continue
		}
		key := newDeliverableKey(e.Selector)
		group, ok := groups[key]
		if !ok {
			group = &listedGroup{
				architecture:   e.Selector.Architecture,
				target:         e.Selector.Target,
				representation: e.Selector.Representation,
				usage:          e.Selector.Usage,
				roles:          make(map[string][]Alternative),
			}
			groups[key] = group
			order = append(order, key)
		}
		alt := Alternative{
			Compression:  e.Selector.Compression,
			ArtifactType: e.ArtifactType,
		}
		group.roles[e.Selector.Role] = append(group.roles[e.Selector.Role], alt)
	}

	out := make([]Listed, 0, len(order))
	for _, key := range order {
		group := groups[key]
		if !matchesListRoles(group.roles, q.Roles) {
			continue
		}
		out = append(out, group.toDeliverable())
	}
	sortDeliverables(out)
	return out, nil
}

// validateListQuery reports whether q satisfies spec section 7.1 and returns
// the canonical usage filter. An empty returned filter means no usage filter.
func validateListQuery(q ListQuery) (string, error) {
	if q.Architecture != "" {
		if err := validateArchitecture(q.Architecture); err != nil {
			return "", fmt.Errorf("list query architecture: %w", err)
		}
	}
	if q.Target != "" {
		if err := validateBasicToken(q.Target); err != nil {
			return "", fmt.Errorf("list query target: %w", err)
		}
	}
	if q.Representation != "" {
		if err := validateBasicToken(q.Representation); err != nil {
			return "", fmt.Errorf("list query representation: %w", err)
		}
	}
	usageFilter, err := validateListUsage(q.Usage)
	if err != nil {
		return "", err
	}
	if err := validateRoleList(q.Roles); err != nil {
		return "", fmt.Errorf("list query roles: %w", err)
	}
	return usageFilter, nil
}

// validateListUsage reports whether usage is a valid spec section 7.1 list
// query usage list and returns its canonical form. Nil is valid and means
// "omitted". A non-nil list must be non-empty and free of duplicates.
func validateListUsage(usage []string) (string, error) {
	if usage == nil {
		return "", nil
	}
	if len(usage) == 0 {
		return "", errors.New("list query usage: must be non-empty when present")
	}
	return canonicalUsageQuery(usage, "list query usage")
}

// matchesListScalars reports whether sel matches the exact scalar filters in q.
func matchesListScalars(sel Selector, q ListQuery) bool {
	if q.Architecture != "" && sel.Architecture != q.Architecture {
		return false
	}
	if q.Target != "" && sel.Target != q.Target {
		return false
	}
	if q.Representation != "" && sel.Representation != q.Representation {
		return false
	}
	return true
}

// matchesListRoles reports whether roles contains every requested role. A nil
// requested slice applies no filter.
func matchesListRoles(roles map[string][]Alternative, requested []string) bool {
	if requested == nil {
		return true
	}
	for _, role := range requested {
		if _, ok := roles[role]; !ok {
			return false
		}
	}
	return true
}

// newDeliverableKey returns the four-field grouping key for sel.
func newDeliverableKey(sel Selector) deliverableKey {
	return deliverableKey{
		architecture:   sel.Architecture,
		target:         sel.Target,
		representation: sel.Representation,
		usage:          sel.Usage,
	}
}

// toDeliverable copies g into a sorted [Listed].
func (g *listedGroup) toDeliverable() Listed {
	roleNames := make([]string, 0, len(g.roles))
	for role := range g.roles {
		roleNames = append(roleNames, role)
	}
	slices.Sort(roleNames)
	roles := make([]ListedRole, 0, len(roleNames))
	for _, role := range roleNames {
		alts := slices.Clone(g.roles[role])
		slices.SortFunc(alts, func(a, b Alternative) int {
			return cmp.Compare(a.Compression, b.Compression)
		})
		roles = append(roles, ListedRole{Role: role, Alternatives: alts})
	}
	return Listed{
		Architecture:   g.architecture,
		Target:         g.target,
		Representation: g.representation,
		Usage:          g.usage,
		Roles:          roles,
	}
}

// sortDeliverables sorts d by architecture, then target, then representation,
// then usage, each in ascending UTF-8 byte order.
func sortDeliverables(d []Listed) {
	slices.SortFunc(d, func(a, b Listed) int {
		if c := cmp.Compare(a.Architecture, b.Architecture); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Representation, b.Representation); c != 0 {
			return c
		}
		return cmp.Compare(a.Usage, b.Usage)
	})
}
