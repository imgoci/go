package imgoci

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// maxBasicTokenBytes is the spec section 5.3 maximum length of a basic token.
const maxBasicTokenBytes = 128

// ListQuery selects deliverables from a validated [Index]. Empty scalar fields
// match every value. A nil Roles slice applies no role filter; a non-nil Roles
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
	// Roles, when non-nil, requires every listed role to be present on a
	// matching deliverable. Nil means no role filter. A non-nil empty slice is
	// invalid per spec section 7.1.
	Roles []string
}

// Deliverable is one unique (architecture, target, representation) group
// returned by [Index.List]. Spec section 7.2 defines a deliverable as all file
// entries that share that three-field key.
type Deliverable struct {
	// Architecture is the deliverable's architecture selector.
	Architecture string
	// Target is the deliverable's target selector.
	Target string
	// Representation is the deliverable's representation selector.
	Representation string
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

// listedGroup accumulates roles and alternatives for one deliverable key.
type listedGroup struct {
	// architecture is the deliverable architecture.
	architecture string
	// target is the deliverable target.
	target string
	// representation is the deliverable representation.
	representation string
	// roles maps role name to compression-sorted alternatives as they arrive.
	roles map[string][]TransportAlternative
}

// List returns every deliverable that matches q, including each role and its
// transport alternatives, sorted per spec section 7.2. An empty result is
// valid. Listing does not filter by consumer capabilities.
//
// Spec section 7.1 requires a consumer to validate the query before fetching
// the release. This API is fetch-once, query-many: a query first exists here,
// after the caller's explicit [Client.Fetch]. List therefore validates q
// completely before it inspects a single index entry, but necessarily after
// the index was retrieved. The only consequence is one wasted manifest round
// trip for an invalid query; an invalid query never yields a result.
func (x *Index) List(q ListQuery) ([]Deliverable, error) {
	if x == nil {
		return nil, errors.New("list: nil index")
	}
	if err := validateListQuery(q); err != nil {
		return nil, err
	}

	groups := make(map[string]*listedGroup)
	order := make([]string, 0)
	for _, entry := range x.entries {
		if !matchesListScalars(entry.Selector, q) {
			continue
		}
		key := deliverableKey(entry.Selector)
		group, ok := groups[key]
		if !ok {
			group = &listedGroup{
				architecture:   entry.Selector.Architecture,
				target:         entry.Selector.Target,
				representation: entry.Selector.Representation,
				roles:          make(map[string][]TransportAlternative),
			}
			groups[key] = group
			order = append(order, key)
		}
		alt := TransportAlternative{
			Compression:  entry.Selector.Compression,
			ArtifactType: entry.ArtifactType,
		}
		group.roles[entry.Selector.Role] = append(group.roles[entry.Selector.Role], alt)
	}

	out := make([]Deliverable, 0, len(order))
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

// validateListQuery reports whether q satisfies spec section 7.1.
func validateListQuery(q ListQuery) error {
	if q.Architecture != "" {
		if err := validateArchitecture(q.Architecture); err != nil {
			return fmt.Errorf("list query architecture: %w", err)
		}
	}
	if q.Target != "" {
		if err := validateBasicToken(q.Target); err != nil {
			return fmt.Errorf("list query target: %w", err)
		}
	}
	if q.Representation != "" {
		if err := validateBasicToken(q.Representation); err != nil {
			return fmt.Errorf("list query representation: %w", err)
		}
	}
	if err := validateRoleList(q.Roles); err != nil {
		return fmt.Errorf("list query roles: %w", err)
	}
	return nil
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
func matchesListRoles(roles map[string][]TransportAlternative, requested []string) bool {
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

// deliverableKey is the map key for one (architecture, target, representation).
func deliverableKey(sel Selector) string {
	return sel.Architecture + "\x00" + sel.Target + "\x00" + sel.Representation
}

// toDeliverable copies g into a sorted public [Deliverable].
func (g *listedGroup) toDeliverable() Deliverable {
	roleNames := make([]string, 0, len(g.roles))
	for role := range g.roles {
		roleNames = append(roleNames, role)
	}
	slices.Sort(roleNames)
	roles := make([]DeliverableRole, 0, len(roleNames))
	for _, role := range roleNames {
		alts := slices.Clone(g.roles[role])
		slices.SortFunc(alts, func(a, b TransportAlternative) int {
			return cmp.Compare(a.Compression, b.Compression)
		})
		roles = append(roles, DeliverableRole{Role: role, Alternatives: alts})
	}
	return Deliverable{
		Architecture:   g.architecture,
		Target:         g.target,
		Representation: g.representation,
		Roles:          roles,
	}
}

// sortDeliverables sorts d by architecture, then target, then representation,
// each in ascending UTF-8 byte order.
func sortDeliverables(d []Deliverable) {
	slices.SortFunc(d, func(a, b Deliverable) int {
		if c := cmp.Compare(a.Architecture, b.Architecture); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		return cmp.Compare(a.Representation, b.Representation)
	})
}

// validateRoleList reports whether roles is a valid spec section 7.1 role list.
// Nil is valid and means "omitted". A non-nil list must be non-empty and free
// of duplicates, and every value must be a basic token.
func validateRoleList(roles []string) error {
	if roles == nil {
		return nil
	}
	if len(roles) == 0 {
		return errors.New("must be non-empty when present")
	}
	return validateUniqueBasicTokens(roles, "role")
}

// validateUniqueBasicTokens reports whether values are unique basic tokens.
func validateUniqueBasicTokens(values []string, kind string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateBasicToken(value); err != nil {
			return fmt.Errorf("%s %q: %w", kind, value, err)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

// validateArchitecture reports whether s is one basic token or two basic
// tokens separated by a slash, per spec section 5.3.
func validateArchitecture(s string) error {
	left, right, ok := strings.Cut(s, "/")
	if !ok {
		return validateBasicToken(s)
	}
	if strings.Contains(right, "/") {
		return fmt.Errorf("%q: architecture must be one basic token or two separated by /", s)
	}
	if err := validateBasicToken(left); err != nil {
		return fmt.Errorf("architecture first token: %w", err)
	}
	if err := validateBasicToken(right); err != nil {
		return fmt.Errorf("architecture second token: %w", err)
	}
	return nil
}

// validateBasicToken reports whether s matches spec section 5.3 basic-token
// syntax: 1 to 128 ASCII bytes of ^[a-z0-9]+([._-][a-z0-9]+)*$.
func validateBasicToken(s string) error {
	if !validBasicToken(s) {
		return fmt.Errorf("%q is not a basic token", s)
	}
	return nil
}

// validBasicToken reports whether s is a spec section 5.3 basic token.
func validBasicToken(s string) bool {
	if len(s) == 0 || len(s) > maxBasicTokenBytes {
		return false
	}
	if !isBasicTokenAlnum(s[0]) {
		return false
	}
	needAlnum := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		if isBasicTokenAlnum(c) {
			needAlnum = false
			continue
		}
		if needAlnum || !isBasicTokenSep(c) {
			return false
		}
		needAlnum = true
	}
	return !needAlnum
}

// isBasicTokenAlnum reports whether c is an ASCII lowercase letter or digit.
func isBasicTokenAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// isBasicTokenSep reports whether c may separate alphanumeric runs in a basic
// token.
func isBasicTokenSep(c byte) bool {
	return c == '.' || c == '_' || c == '-'
}
