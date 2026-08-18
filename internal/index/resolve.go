package index

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// ResolveQuery is a resolve request over a validated index. Architecture,
// Target, Representation, and Usage are required exact matches. Nil and empty
// Usage both request the empty usage set. A nil Roles slice applies the spec
// section 7.3 default-role rule; a non-nil Roles slice must be non-empty and
// selects exactly those roles. Compressions is a required preference-ordered
// list. Capabilities is the effective capability set, already defaulted by
// the caller.
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
	// Capabilities is the effective capability set, already defaulted by the caller.
	Capabilities []string
}

// CapabilityError reports a selected role with no supported file-manifest type.
type CapabilityError struct {
	// Role is the selected role that has no remaining supported file-manifest type.
	Role string
}

// Error returns the capability-filter failure text for e.Role.
func (e *CapabilityError) Error() string {
	return fmt.Sprintf("role %q has no supported file-manifest type", e.Role)
}

// Resolve returns the chosen entry for every selected role, in role order.
// Selection is atomic: each step completes for every selected role before the
// next step starts, and any failure returns a nil result with no partial
// entries.
//
// Offline selection failures (no deliverable, a selected role absent from that
// deliverable, or no accepted compression) return a descriptive error without a
// matchable sentinel. Only the capability filter returns a [CapabilityError].
//
// Resolve validates q completely before it inspects a single entry, which
// is after the caller has retrieved the index rather than before it. That
// ordering is a deviation from spec section 7.1.
func Resolve(entries []Entry, q ResolveQuery) ([]Entry, error) {
	usage, err := ValidateResolveQuery(q)
	if err != nil {
		return nil, err
	}

	candidates := matchingDeliverable(entries, q, usage)
	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"resolve: no deliverable for architecture %q target %q representation %q %s",
			q.Architecture, q.Target, q.Representation, FormatUsage(usage),
		)
	}

	roles := selectedRoles(q, candidates)

	byRole := groupByRole(candidates, roles)
	if err = requireRolesPresent(roles, byRole); err != nil {
		return nil, err
	}
	if err = filterByCapabilities(roles, byRole, q.Capabilities); err != nil {
		return nil, err
	}
	chosen, err := pickCompressions(roles, byRole, q.Compressions)
	if err != nil {
		return nil, err
	}
	return chosen, nil
}

// ValidateResolveQuery reports whether q satisfies spec section 7.1 and
// returns the canonical usage string so [Resolve] can compare it once.
func ValidateResolveQuery(q ResolveQuery) (string, error) {
	if q.Architecture == "" {
		return "", errors.New("resolve query: architecture is required")
	}
	if err := validateArchitecture(q.Architecture); err != nil {
		return "", fmt.Errorf("resolve query architecture: %w", err)
	}
	if q.Target == "" {
		return "", errors.New("resolve query: target is required")
	}
	if err := validateBasicToken(q.Target); err != nil {
		return "", fmt.Errorf("resolve query target: %w", err)
	}
	if q.Representation == "" {
		return "", errors.New("resolve query: representation is required")
	}
	if err := validateBasicToken(q.Representation); err != nil {
		return "", fmt.Errorf("resolve query representation: %w", err)
	}
	usage, err := canonicalUsageQuery(q.Usage, "resolve query usage")
	if err != nil {
		return "", err
	}
	if err := validateRoleList(q.Roles); err != nil {
		return "", fmt.Errorf("resolve query roles: %w", err)
	}
	if len(q.Compressions) == 0 {
		return "", errors.New("resolve query: compressions must be non-empty")
	}
	if err := validateUniqueBasicTokens(q.Compressions, "compression"); err != nil {
		return "", fmt.Errorf("resolve query compressions: %w", err)
	}
	for _, compression := range q.Compressions {
		if !allowedResolveCompression(compression) {
			return "", fmt.Errorf(
				"resolve query compressions: %q is outside the spec section 5.4 set {%s, %s, %s, %s}",
				compression,
				compressionNone,
				compressionGzip,
				compressionXz,
				compressionZstd,
			)
		}
	}
	return usage, nil
}

// allowedResolveCompression reports whether s is one of the spec section 5.4 v1
// tokens {none, gzip, xz, zstd}. This is the spec set, not a probe of whether a
// decoder is linked in this build.
func allowedResolveCompression(s string) bool {
	switch s {
	case compressionNone, compressionGzip, compressionXz, compressionZstd:
		return true
	default:
		return false
	}
}

// matchingDeliverable returns every entry whose deliverable key equals q and
// usage. usage is the canonical serialized set, compared for exact equality.
func matchingDeliverable(entries []Entry, q ResolveQuery, usage string) []Entry {
	out := make([]Entry, 0)
	for _, entry := range entries {
		if entry.Selector.Architecture == q.Architecture &&
			entry.Selector.Target == q.Target &&
			entry.Selector.Representation == q.Representation &&
			entry.Selector.Usage == usage {
			out = append(out, entry)
		}
	}
	return out
}

// selectedRoles returns the role list to resolve, applying the default-role
// rule when q.Roles is nil.
func selectedRoles(q ResolveQuery, candidates []Entry) []string {
	if q.Roles != nil {
		return slices.Clone(q.Roles)
	}
	return defaultRoles(q.Representation, presentRoles(candidates))
}

// presentRoles returns the unique roles in entries, sorted in UTF-8 byte order.
func presentRoles(entries []Entry) []string {
	seen := make(map[string]struct{}, len(entries))
	roles := make([]string, 0)
	for _, entry := range entries {
		role := entry.Selector.Role
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles
}

// defaultRoles implements spec section 7.3 steps 4 and 5.
func defaultRoles(representation string, present []string) []string {
	switch representation {
	case representationLinuxNetboot:
		return slices.Clone(present)
	case representationRaw, representationRaw4kn, representationQcow2, representationISO:
		return []string{roleDisk}
	case representationIncusVM:
		return []string{roleDisk, roleMetadata}
	default:
		return slices.Clone(present)
	}
}

// groupByRole maps each selected role to its candidate entries. Roles that
// have no entries are still present as empty slices so later steps can fail
// uniformly.
func groupByRole(candidates []Entry, roles []string) map[string][]Entry {
	byRole := make(map[string][]Entry, len(roles))
	for _, role := range roles {
		byRole[role] = nil
	}
	wanted := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		wanted[role] = struct{}{}
	}
	for _, entry := range candidates {
		if _, ok := wanted[entry.Selector.Role]; !ok {
			continue
		}
		byRole[entry.Selector.Role] = append(byRole[entry.Selector.Role], entry)
	}
	return byRole
}

// requireRolesPresent fails if any selected role is absent from the deliverable.
func requireRolesPresent(roles []string, byRole map[string][]Entry) error {
	for _, role := range roles {
		if len(byRole[role]) == 0 {
			return fmt.Errorf("resolve: selected role %q is absent", role)
		}
	}
	return nil
}

// filterByCapabilities removes, for every selected role, the alternatives whose
// descriptor artifactType is outside the effective capability set. It
// implements spec section 7.3 steps 8 and 9, whose barrier requires that
// removal to complete for every selected role before any role is failed for
// having nothing left. Failure returns a [CapabilityError] and leaves byRole
// untouched, so no caller observes a partial result.
func filterByCapabilities(roles []string, byRole map[string][]Entry, caps []string) error {
	filtered := make(map[string][]Entry, len(roles))
	for _, role := range roles {
		kept := make([]Entry, 0, len(byRole[role]))
		for _, entry := range byRole[role] {
			if SupportsMediaType(caps, entry.ArtifactType) {
				kept = append(kept, entry)
			}
		}
		filtered[role] = kept
	}
	for _, role := range roles {
		if len(filtered[role]) == 0 {
			return &CapabilityError{Role: role}
		}
	}
	maps.Copy(byRole, filtered)

	return nil
}

// pickCompressions returns, for every selected role and in role order, the
// first accepted compression that remains after capability filtering. It
// implements spec section 7.3 steps 10 and 11, whose barrier requires that
// choice to be made for every selected role before any role is failed for
// having no accepted alternative. Failure returns no entries, so no caller
// observes a partial selection.
func pickCompressions(roles []string, byRole map[string][]Entry, compressions []string) ([]Entry, error) {
	chosen := make([]Entry, len(roles))
	accepted := make([]bool, len(roles))
	for i, role := range roles {
		chosen[i], accepted[i] = firstAccepted(byRole[role], compressions)
	}
	for i, role := range roles {
		if !accepted[i] {
			return nil, fmt.Errorf("resolve: role %q has no accepted compression", role)
		}
	}

	return chosen, nil
}

// firstAccepted returns the entry whose compression is the earliest value in
// compressions that exists among remaining.
func firstAccepted(remaining []Entry, compressions []string) (Entry, bool) {
	byCompression := make(map[string]Entry, len(remaining))
	for _, entry := range remaining {
		byCompression[entry.Selector.Compression] = entry
	}
	for _, compression := range compressions {
		if entry, ok := byCompression[compression]; ok {
			return entry, true
		}
	}
	return Entry{}, false
}
