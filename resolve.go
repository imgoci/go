package imgoci

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/opencontainers/go-digest"
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

// The v1 compression tokens are the fixed spec section 5.4 set. Query
// validation accepts only these values. internal/decomp implements the
// corresponding decoders in later slices; this list is the spec set, not a
// build capability.
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
// Target, and Representation are required exact matches. A nil Roles slice
// applies the spec section 7.3 default-role rule; a non-nil Roles slice must
// be non-empty and selects exactly those roles. Compressions is a required
// preference-ordered list. A zero Capabilities means [StandardCapabilities].
type ResolveQuery struct {
	// Architecture is the required exact architecture selector.
	Architecture string
	// Target is the required exact target selector.
	Target string
	// Representation is the required exact representation selector.
	Representation string
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
// selection was derived from. That digest is the spec section 6.3 binding.
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
// matchable sentinel in v1. Only the capability filter wraps
// [ErrUnsupportedType]. A sentinel for the other failures is additive later if
// callers need it.
func (x *Index) Resolve(q ResolveQuery) (*Resolved, error) {
	if x == nil {
		return nil, errors.New("resolve: nil index")
	}
	if err := validateResolveQuery(q); err != nil {
		return nil, err
	}

	candidates := matchingDeliverable(x.entries, q)
	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"resolve: no deliverable for architecture %q target %q representation %q",
			q.Architecture, q.Target, q.Representation,
		)
	}

	roles := selectedRoles(q, candidates)

	byRole := groupByRole(candidates, roles)
	if err := requireRolesPresent(roles, byRole); err != nil {
		return nil, err
	}
	if err := filterByCapabilities(roles, byRole, q.Capabilities.effective()); err != nil {
		return nil, err
	}
	chosen, err := pickCompressions(roles, byRole, q.Compressions)
	if err != nil {
		return nil, err
	}
	return &Resolved{digest: x.digest, entries: chosen}, nil
}

// validateResolveQuery reports whether q satisfies spec section 7.1.
func validateResolveQuery(q ResolveQuery) error {
	if q.Architecture == "" {
		return errors.New("resolve query: architecture is required")
	}
	if err := validateArchitecture(q.Architecture); err != nil {
		return fmt.Errorf("resolve query architecture: %w", err)
	}
	if q.Target == "" {
		return errors.New("resolve query: target is required")
	}
	if err := validateBasicToken(q.Target); err != nil {
		return fmt.Errorf("resolve query target: %w", err)
	}
	if q.Representation == "" {
		return errors.New("resolve query: representation is required")
	}
	if err := validateBasicToken(q.Representation); err != nil {
		return fmt.Errorf("resolve query representation: %w", err)
	}
	if err := validateRoleList(q.Roles); err != nil {
		return fmt.Errorf("resolve query roles: %w", err)
	}
	if len(q.Compressions) == 0 {
		return errors.New("resolve query: compressions must be non-empty")
	}
	if err := validateUniqueBasicTokens(q.Compressions, "compression"); err != nil {
		return fmt.Errorf("resolve query compressions: %w", err)
	}
	for _, compression := range q.Compressions {
		if !allowedResolveCompression(compression) {
			return fmt.Errorf(
				"resolve query compressions: %q is outside the spec section 5.4 set {%s, %s, %s, %s}",
				compression,
				compressionNone,
				compressionGzip,
				compressionXZ,
				compressionZstd,
			)
		}
	}
	return nil
}

// allowedResolveCompression reports whether s is one of the spec section 5.4
// v1 tokens {none, gzip, xz, zstd}. This is the spec set, not a probe of
// whether internal/decomp is linked in this build.
func allowedResolveCompression(s string) bool {
	switch s {
	case compressionNone, compressionGzip, compressionXZ, compressionZstd:
		return true
	default:
		return false
	}
}

// matchingDeliverable returns every entry whose deliverable key equals q.
func matchingDeliverable(entries []FileEntry, q ResolveQuery) []FileEntry {
	out := make([]FileEntry, 0)
	for _, entry := range entries {
		if entry.Selector.Architecture == q.Architecture &&
			entry.Selector.Target == q.Target &&
			entry.Selector.Representation == q.Representation {
			out = append(out, entry)
		}
	}
	return out
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

// filterByCapabilities removes alternatives whose artifactType is outside the
// effective capability set. It completes this step for every role before
// returning. Failure wraps [ErrUnsupportedType] and yields no remaining map
// mutation that callers can observe as a partial result: the input map is
// replaced in place only after every role still has at least one alternative.
func filterByCapabilities(roles []string, byRole map[string][]FileEntry, caps Capabilities) error {
	filtered := make(map[string][]FileEntry, len(roles))
	for _, role := range roles {
		kept := make([]FileEntry, 0, len(byRole[role]))
		for _, entry := range byRole[role] {
			if supportsType(caps.types, entry.ArtifactType) {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			return fmt.Errorf("%w: role %q has no supported file-manifest type", ErrUnsupportedType, role)
		}
		filtered[role] = kept
	}
	maps.Copy(byRole, filtered)
	return nil
}

// pickCompressions chooses, for every role, the first accepted compression
// that remains after capability filtering. It completes this step for every
// role before returning any entries.
func pickCompressions(roles []string, byRole map[string][]FileEntry, compressions []string) ([]FileEntry, error) {
	chosen := make([]FileEntry, 0, len(roles))
	for _, role := range roles {
		entry, ok := firstAccepted(byRole[role], compressions)
		if !ok {
			return nil, fmt.Errorf("resolve: role %q has no accepted compression", role)
		}
		chosen = append(chosen, entry)
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
