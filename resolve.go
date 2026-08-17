package imgoci

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

const (
	// representationLinuxNetboot is the coordinated Linux network-boot set.
	representationLinuxNetboot = "linux-netboot"
	// representationRaw is a raw disk image with 512-byte logical sectors.
	representationRaw = "raw"
	// representationRaw4kn is a raw disk image with 4096-byte logical sectors.
	representationRaw4kn = "raw-4kn"
	// representationQcow2 is a standalone QCOW2 disk image.
	representationQcow2 = "qcow2"
	// representationIncusVM is a split Incus virtual-machine image.
	representationIncusVM = "incus-vm"
	// representationISO is an ECMA-119 optical-disc image.
	representationISO = "iso"
	// roleDisk is a disk or optical-media image.
	roleDisk = "disk"
	// roleMetadata is metadata required to import or run the other files.
	roleMetadata = "metadata"
)

// Compression tokens are the fixed spec section 5.4 set. Query validation
// accepts only these values. This list is the spec set, not a build capability.
const (
	// compressionNone is the uncompressed transport encoding.
	compressionNone = "none"
	// compressionGzip is the gzip transport encoding.
	compressionGzip = "gzip"
	// compressionXZ is the xz transport encoding.
	compressionXZ = "xz"
	// compressionZstd is the zstd transport encoding.
	compressionZstd = "zstd"
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
	usage, err := validateResolveQuery(q)
	if err != nil {
		return nil, err
	}

	candidates := matchingDeliverable(x.entries, q, usage)
	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"resolve: no deliverable for architecture %q target %q representation %q %s",
			q.Architecture, q.Target, q.Representation, formatResolveUsage(usage),
		)
	}

	roles := selectedRoles(q, candidates)

	byRole := groupByRole(candidates, roles)
	if err = requireRolesPresent(roles, byRole); err != nil {
		return nil, err
	}
	if err = filterByCapabilities(roles, byRole, q.Capabilities.effective()); err != nil {
		return nil, err
	}
	chosen, err := pickCompressions(roles, byRole, q.Compressions)
	if err != nil {
		return nil, err
	}
	return &Resolved{digest: x.digest, entries: chosen}, nil
}

// validateResolveQuery reports whether q satisfies spec section 7.1 and
// returns the canonical usage string so [Index.Resolve] can compare it once.
func validateResolveQuery(q ResolveQuery) (string, error) {
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
				compressionXZ,
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
	case compressionNone, compressionGzip, compressionXZ, compressionZstd:
		return true
	default:
		return false
	}
}

// matchingDeliverable returns every entry whose deliverable key equals q and
// usage. usage is the canonical serialized set, compared for exact equality.
func matchingDeliverable(entries []FileEntry, q ResolveQuery, usage string) []FileEntry {
	out := make([]FileEntry, 0)
	for _, entry := range entries {
		if entry.Selector.Architecture == q.Architecture &&
			entry.Selector.Target == q.Target &&
			entry.Selector.Representation == q.Representation &&
			entry.Selector.Usage.String() == usage {
			out = append(out, entry)
		}
	}
	return out
}

// formatResolveUsage renders a canonical usage set for a resolve error. A
// present value is quoted because it is itself comma-separated; the empty set
// is usage=<empty>, which no basic token can spell.
func formatResolveUsage(usage string) string {
	return index.FormatUsage(usage)
}

// selectedRoles returns the role list to resolve, applying the default-role
// rule when q.Roles is nil.
func selectedRoles(q ResolveQuery, candidates []FileEntry) []string {
	if q.Roles != nil {
		return slices.Clone(q.Roles)
	}
	return defaultRoles(q.Representation, presentRoles(candidates))
}

// presentRoles returns the unique roles in entries, sorted in UTF-8 byte order.
func presentRoles(entries []FileEntry) []string {
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
func groupByRole(candidates []FileEntry, roles []string) map[string][]FileEntry {
	byRole := make(map[string][]FileEntry, len(roles))
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
func requireRolesPresent(roles []string, byRole map[string][]FileEntry) error {
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
// having nothing left. Failure wraps [ErrUnsupportedType] and leaves byRole
// untouched, so no caller observes a partial result.
func filterByCapabilities(roles []string, byRole map[string][]FileEntry, caps Capabilities) error {
	filtered := make(map[string][]FileEntry, len(roles))
	for _, role := range roles {
		kept := make([]FileEntry, 0, len(byRole[role]))
		for _, entry := range byRole[role] {
			if supportsType(caps.types, entry.ArtifactType) {
				kept = append(kept, entry)
			}
		}
		filtered[role] = kept
	}
	for _, role := range roles {
		if len(filtered[role]) == 0 {
			return fmt.Errorf("%w: role %q has no supported file-manifest type", ErrUnsupportedType, role)
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
func pickCompressions(roles []string, byRole map[string][]FileEntry, compressions []string) ([]FileEntry, error) {
	chosen := make([]FileEntry, len(roles))
	accepted := make([]bool, len(roles))
	for i, role := range roles {
		chosen[i], accepted[i] = firstAccepted(byRole[role], compressions)
	}
	for i, role := range roles {
		if !accepted[i] {
			return nil, fmt.Errorf("resolve: role %q has no accepted compression", role)
		}
	}

	return cloneEntries(chosen), nil
}

// firstAccepted returns the entry whose compression is the earliest value in
// compressions that exists among remaining.
func firstAccepted(remaining []FileEntry, compressions []string) (FileEntry, bool) {
	byCompression := make(map[string]FileEntry, len(remaining))
	for _, entry := range remaining {
		byCompression[entry.Selector.Compression] = entry
	}
	for _, compression := range compressions {
		if entry, ok := byCompression[compression]; ok {
			return entry, true
		}
	}
	return FileEntry{}, false
}
