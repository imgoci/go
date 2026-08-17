package index

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Validate applies spec §6 rules 1–9 to v. Each error names the violated rule.
func Validate(v *Value) error {
	if v == nil {
		return ruleError(specRuleRoot, "index value is nil")
	}
	if err := validateRule1(v); err != nil {
		return err
	}
	if err := validateRule2(v); err != nil {
		return err
	}
	if err := validateRule3(v); err != nil {
		return err
	}
	if err := validateRule4(v); err != nil {
		return err
	}
	if err := validateRule5(v); err != nil {
		return err
	}
	if err := validateRule6(v); err != nil {
		return err
	}
	if err := validateRule7(v); err != nil {
		return err
	}
	if err := validateRule8(v); err != nil {
		return err
	}
	return validateRule9(v)
}

// validateRule1 checks spec §6 rule 1: root identity and required annotations.
func validateRule1(v *Value) error {
	if !v.schemaVersionSet || v.SchemaVersion != schemaVersionV2 {
		return ruleError(specRuleRoot, "schemaVersion must be 2")
	}
	if !v.mediaTypeSet || !equalMediaType(v.MediaType, MediaTypeIndex) {
		return ruleError(specRuleRoot, "mediaType must identify %s", MediaTypeIndex)
	}
	if !v.artifactTypeSet || !equalMediaType(v.ArtifactType, ArtifactTypeRelease) {
		return ruleError(specRuleRoot, "artifactType must identify %s", ArtifactTypeRelease)
	}
	if !v.manifestsSet || len(v.Manifests) == 0 {
		return ruleError(specRuleRoot, "manifests must contain at least one file-entry descriptor")
	}
	if !v.annotationsSet {
		return ruleError(specRuleRoot, "annotations must be present")
	}
	if _, ok := v.Annotations[AnnotationName]; !ok {
		return ruleError(specRuleRoot, "annotations must contain %s", AnnotationName)
	}
	if _, ok := v.Annotations[AnnotationVersion]; !ok {
		return ruleError(specRuleRoot, "annotations must contain %s", AnnotationVersion)
	}
	if !isReleaseVersion(v.Annotations[AnnotationVersion]) {
		return ruleError(specRuleRoot, "%s must contain 1 to %d printable ASCII characters without whitespace",
			AnnotationVersion, maxReleaseVersion)
	}
	return nil
}

// validateRule2 checks spec §6 rule 2: descriptor required members.
func validateRule2(v *Value) error {
	for i, d := range v.Manifests {
		if err := validateDescriptorRule2(i, d); err != nil {
			return err
		}
	}
	return nil
}

// validateDescriptorRule2 checks spec §6 rule 2 for one descriptor.
func validateDescriptorRule2(i int, d Descriptor) error {
	if !d.mediaTypeSet || !equalMediaType(d.MediaType, MediaTypeManifest) {
		return ruleError(specRuleDescriptor, "manifests[%d] mediaType must identify %s", i, MediaTypeManifest)
	}
	if !d.digestSet || !isSHA256Digest(d.Digest.String()) {
		return ruleError(
			specRuleDescriptor,
			"manifests[%d] digest must be sha256: followed by %d lowercase hex digits",
			i,
			sha256HexLength,
		)
	}
	if !d.sizeSet || d.Size < minManifestSize || d.Size > maxManifestSize {
		return ruleError(
			specRuleDescriptor,
			"manifests[%d] size must be a JSON integer from %d through %d",
			i,
			minManifestSize,
			maxManifestSize,
		)
	}
	if !d.artifactTypeSet {
		return ruleError(specRuleDescriptor, "manifests[%d] artifactType is required", i)
	}
	if !d.annotationsSet {
		return ruleError(specRuleDescriptor, "manifests[%d] annotations is required", i)
	}
	for _, key := range requiredFileAnnotationKeys() {
		if _, ok := d.Annotations[key]; !ok {
			return ruleError(specRuleDescriptor, "manifests[%d] annotations must contain %s", i, key)
		}
	}
	return nil
}

// validateRule3 checks spec §6 rule 3: syntax of names, selectors, and sizes.
func validateRule3(v *Value) error {
	if !isBasicToken(v.Annotations[AnnotationName]) {
		return ruleError(specRuleSyntax, "%s must be a basic token", AnnotationName)
	}
	for i, d := range v.Manifests {
		if err := validateDescriptorRule3(i, d); err != nil {
			return err
		}
	}
	return nil
}

// validateDescriptorRule3 checks spec §6 rule 3 for one descriptor.
func validateDescriptorRule3(i int, d Descriptor) error {
	if !isMediaType(d.ArtifactType) {
		return ruleError(specRuleSyntax, "manifests[%d] artifactType must be an RFC 6838 type/subtype", i)
	}
	sel := d.Selector()
	if !isArchitecture(sel.Architecture) {
		return ruleError(
			specRuleSyntax,
			"manifests[%d] %s must be one or two basic tokens separated by /",
			i,
			AnnotationArchitecture,
		)
	}
	if !isBasicToken(sel.Target) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must be a basic token", i, AnnotationTarget)
	}
	if !isBasicToken(sel.Representation) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must be a basic token", i, AnnotationRepresentation)
	}
	if !isBasicToken(sel.Role) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must be a basic token", i, AnnotationRole)
	}
	if !isBasicToken(sel.Compression) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must be a basic token", i, AnnotationCompression)
	}
	if usage, present := d.Annotations[AnnotationUsage]; present {
		if err := ValidateUsage(usage); err != nil {
			return ruleError(specRuleSyntax, "manifests[%d] %s: %s", i, AnnotationUsage, err)
		}
	}
	if !isSHA256Digest(d.annotation(AnnotationContentDigest)) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must be sha256: followed by %d lowercase hex digits",
			i, AnnotationContentDigest, sha256HexLength)
	}
	if _, ok := parseContentSize(d.annotation(AnnotationContentSize)); !ok {
		return ruleError(
			specRuleSyntax,
			"manifests[%d] %s must be a decimal string matching ^(0|[1-9][0-9]*)$ and at most 2^63-1",
			i,
			AnnotationContentSize,
		)
	}
	if !isFilename(d.annotation(AnnotationFilename)) {
		return ruleError(specRuleSyntax, "manifests[%d] %s must match the filename grammar", i, AnnotationFilename)
	}
	return nil
}

// validateRule4 checks spec §6 rule 4: required roles, forbidden targets, and
// usage-value relationships for each exact usage set.
func validateRule4(v *Value) error {
	roles := make(map[deliverableKey]map[string]struct{})
	order := make([]deliverableKey, 0, len(v.Manifests))
	for i, d := range v.Manifests {
		sel := d.Selector()
		if err := ValidateUsageRelationship(sel.Usage); err != nil {
			return ruleError(
				specRuleRoles,
				"manifests[%d] usage set %s: %s",
				i,
				FormatUsage(sel.Usage),
				err,
			)
		}
		key := deliverableKey{sel.Architecture, sel.Target, sel.Representation, sel.Usage}
		set, ok := roles[key]
		if !ok {
			set = make(map[string]struct{})
			roles[key] = set
			order = append(order, key)
		}
		set[sel.Role] = struct{}{}
	}
	for _, key := range order {
		have := roles[key]
		if key.representation == representationIncusVM && key.target != targetIncus {
			return ruleError(
				specRuleRoles,
				"incus-vm deliverable %s, %s, %s must use target incus",
				key.architecture,
				key.target,
				FormatUsage(key.usage),
			)
		}
		for _, role := range requiredRoles(key.representation) {
			if _, ok := have[role]; !ok {
				return ruleError(specRuleRoles, "deliverable %s, %s, %s, %s must contain the %s role",
					key.architecture, key.target, key.representation, FormatUsage(key.usage), role)
			}
		}
	}
	return nil
}

// validateRule5 checks spec §6 rule 5: unique six-field selectors.
func validateRule5(v *Value) error {
	seen := make(map[Selector]int)
	for i, d := range v.Manifests {
		sel := d.Selector()
		if prev, ok := seen[sel]; ok {
			return ruleError(
				specRuleSelector,
				"transport alternative %s is duplicated at manifests[%d] and manifests[%d]",
				formatSelector(sel),
				prev,
				i,
			)
		}
		seen[sel] = i
	}
	return nil
}

// validateRule6 checks spec §6 rule 6: same-file content identity.
func validateRule6(v *Value) error {
	// fileID is the spec §2 file key. usage is the canonical serialized usage
	// value, empty when the annotation is absent.
	type fileID struct {
		architecture, target, representation, usage, role string
	}
	type fileContent struct {
		digest, size, filename string
		index                  int
	}
	seen := make(map[fileID]fileContent)
	for i, d := range v.Manifests {
		sel := d.Selector()
		id := fileID{sel.Architecture, sel.Target, sel.Representation, sel.Usage, sel.Role}
		got := fileContent{
			digest:   d.annotation(AnnotationContentDigest),
			size:     d.annotation(AnnotationContentSize),
			filename: d.annotation(AnnotationFilename),
			index:    i,
		}
		if prev, ok := seen[id]; ok {
			if prev.digest != got.digest || prev.size != got.size || prev.filename != got.filename {
				return ruleError(
					specRuleFileIdentity,
					"transport alternatives for file %s, %s, %s, %s, %s must have the same content digest, content size, and filename",
					sel.Architecture,
					sel.Target,
					sel.Representation,
					FormatUsage(sel.Usage),
					sel.Role,
				)
			}
			continue
		}
		seen[id] = got
	}
	return nil
}

// validateRule7 checks spec §6 rule 7: distinct filenames across roles.
func validateRule7(v *Value) error {
	type nameKey struct {
		deliverableKey

		filename string
	}
	owner := make(map[nameKey]string)
	for _, d := range v.Manifests {
		sel := d.Selector()
		key := nameKey{
			deliverableKey: deliverableKey{sel.Architecture, sel.Target, sel.Representation, sel.Usage},
			filename:       d.annotation(AnnotationFilename),
		}
		if prev, ok := owner[key]; ok && prev != sel.Role {
			return ruleError(
				specRuleFilename,
				"different roles in deliverable %s, %s, %s, %s must have different filenames",
				sel.Architecture,
				sel.Target,
				sel.Representation,
				FormatUsage(sel.Usage),
			)
		}
		owner[key] = sel.Role
	}
	return nil
}

// validateRule8 checks spec §6 rule 8: shared file-manifest digest agreement.
func validateRule8(v *Value) error {
	type manifestMeta struct {
		mediaType, artifactType, compression, contentDigest, contentSize string
		size                                                             int64
		index                                                            int
	}
	seen := make(map[string]manifestMeta)
	for i, d := range v.Manifests {
		id := d.Digest.String()
		got := manifestMeta{
			mediaType:     d.MediaType,
			artifactType:  d.ArtifactType,
			compression:   d.Selector().Compression,
			contentDigest: d.annotation(AnnotationContentDigest),
			contentSize:   d.annotation(AnnotationContentSize),
			size:          d.Size,
			index:         i,
		}
		prev, ok := seen[id]
		if !ok {
			seen[id] = got
			continue
		}
		if !equalMediaType(prev.mediaType, got.mediaType) ||
			!equalMediaType(prev.artifactType, got.artifactType) ||
			prev.size != got.size ||
			prev.compression != got.compression ||
			prev.contentDigest != got.contentDigest ||
			prev.contentSize != got.contentSize {
			return ruleError(
				specRuleSharedManifest,
				"descriptors for file manifest %s must agree on media type, descriptor size, artifact type, compression, content digest, and content size",
				id,
			)
		}
	}
	return nil
}

// validateRule9 checks spec §6 rule 9: canonical descriptor order.
func validateRule9(v *Value) error {
	if !manifestsInCanonicalOrder(v.Manifests) {
		return ruleError(
			specRuleOrder,
			"manifests must be ordered by architecture, target, representation, usage, role, and compression in ascending UTF-8 byte order",
		)
	}
	return nil
}

// deliverableKey identifies a (architecture, target, representation, usage) deliverable.
type deliverableKey struct {
	architecture   string
	target         string
	representation string
	// usage is the canonical serialized io.imgoci.usage value. It is empty when
	// the annotation is absent, which is the empty usage set.
	usage string
}

// requiredFileAnnotationKeys returns the eight required file-entry annotation keys.
func requiredFileAnnotationKeys() []string {
	return []string{
		AnnotationArchitecture,
		AnnotationTarget,
		AnnotationRepresentation,
		AnnotationRole,
		AnnotationCompression,
		AnnotationContentDigest,
		AnnotationContentSize,
		AnnotationFilename,
	}
}

// Role names from the spec §5.4 public role registry.
const (
	// roleDisk is the role every disk-image representation must include.
	roleDisk = "disk"
	// roleKernel is the role the linux-netboot representation requires.
	roleKernel = "kernel"
	// roleInitramfs is the optional linux-netboot initial-RAM-filesystem role.
	roleInitramfs = "initramfs"
	// roleMetadata is the additional role the incus-vm representation requires.
	roleMetadata = "metadata"
	// roleRootfs is the optional linux-netboot root-filesystem role.
	roleRootfs = "rootfs"
)

// requiredRoles returns the roles a standard representation must include.
func requiredRoles(representation string) []string {
	switch representation {
	case representationRaw, representationRaw4kn, representationQcow2, representationISO:
		return []string{roleDisk}
	case representationIncusVM:
		return []string{roleDisk, roleMetadata}
	case representationLinuxNetboot:
		return []string{roleKernel}
	default:
		return nil
	}
}

// EqualMediaType reports whether a and b identify the same parameter-free
// media type under spec section 4. Comparison is ASCII case-insensitive and
// allocates nothing. HTTP Content-Type headers, which may carry parameters,
// must be stripped by the registry adapter before they reach this helper.
func EqualMediaType(a, b string) bool {
	return equalMediaType(a, b)
}

// equalMediaType compares media or artifact types ASCII case-insensitively.
// Comparison folds only 'A'..'Z' so Unicode look-alikes such as U+017F and
// U+212A do not match ASCII letters.
func equalMediaType(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if asciiFold(a[i]) != asciiFold(b[i]) {
			return false
		}
	}
	return true
}

// ASCIILower returns s with ASCII letters folded to lowercase. Media types
// in this format are ASCII, so this is the case folding spec section 4 uses.
func ASCIILower(s string) string {
	for i := range len(s) {
		if s[i] >= 'A' && s[i] <= 'Z' {
			out := []byte(s)
			for j := i; j < len(s); j++ {
				out[j] = asciiFold(s[j])
			}
			return string(out)
		}
	}
	return s
}

// asciiFold returns c with ASCII 'A'..'Z' folded to 'a'..'z'. Other bytes,
// including UTF-8 for U+017F and U+212A, are unchanged.
func asciiFold(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// isReleaseVersion reports whether s is 1–128 printable ASCII characters without whitespace.
func isReleaseVersion(s string) bool {
	if len(s) < 1 || len(s) > maxReleaseVersion {
		return false
	}
	for i := range len(s) {
		if s[i] < '!' || s[i] > '~' {
			return false
		}
	}
	return true
}

// IsBasicToken reports whether s matches the spec §5.3 basic-token grammar.
func IsBasicToken(s string) bool {
	return isBasicToken(s)
}

// isBasicToken reports whether s matches the spec §5.3 basic-token grammar.
func isBasicToken(s string) bool {
	if len(s) < 1 || len(s) > maxBasicTokenBytes {
		return false
	}
	i := 0
	if !isASCIIAlnum(s[i]) {
		return false
	}
	i++
	for i < len(s) {
		if isASCIIAlnum(s[i]) {
			i++
			continue
		}
		if s[i] == '.' || s[i] == '_' || s[i] == '-' {
			i++
			if i >= len(s) || !isASCIIAlnum(s[i]) {
				return false
			}
			i++
			continue
		}
		return false
	}
	return true
}

// isArchitecture reports whether s is one or two basic tokens separated by /.
func isArchitecture(s string) bool {
	left, right, split := strings.Cut(s, "/")
	if !split {
		return isBasicToken(s)
	}
	if strings.Contains(right, "/") {
		return false
	}
	return isBasicToken(left) && isBasicToken(right)
}

// isFilename reports whether s matches the spec §5.3 filename grammar.
func isFilename(s string) bool {
	n := len(s)
	if n < 1 || n > maxFilenameBytes {
		return false
	}
	if !isASCIIAlnum(s[0]) {
		return false
	}
	if n == 1 {
		return true
	}
	if !isASCIIAlnum(s[n-1]) {
		return false
	}
	for i := 1; i < n-1; i++ {
		c := s[i]
		if isASCIIAlnum(c) || c == '.' || c == '_' || c == '+' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// isSHA256Digest reports whether s is sha256: followed by 64 lowercase hex digits.
func isSHA256Digest(s string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	hex := s[len(prefix):]
	if len(hex) != sha256HexLength {
		return false
	}
	for i := range len(hex) {
		c := hex[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

// parseContentSize parses a content.size decimal string into an int64.
func parseContentSize(s string) (int64, bool) {
	if !isNonNegativeDecimal(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// isNonNegativeDecimal reports whether s matches ^(0|[1-9][0-9]*)$.
func isNonNegativeDecimal(s string) bool {
	if s == "0" {
		return true
	}
	if s == "" || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// IsMediaType reports whether s is an RFC 6838 type/subtype with no parameters.
func IsMediaType(s string) bool {
	return isMediaType(s)
}

// isMediaType reports whether s is an RFC 6838 type/subtype with no parameters.
func isMediaType(s string) bool {
	typeName, subtype, ok := strings.Cut(s, "/")
	if !ok || strings.Contains(subtype, "/") {
		return false
	}
	return isRestrictedName(typeName) && isRestrictedName(subtype)
}

// isRestrictedName reports whether s is an RFC 6838 restricted-name.
func isRestrictedName(s string) bool {
	if len(s) < 1 || len(s) > 127 {
		return false
	}
	if !isRestrictedNameFirst(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isRestrictedNameRest(s[i]) {
			return false
		}
	}
	return true
}

// isRestrictedNameFirst reports whether c may start an RFC 6838 restricted-name.
func isRestrictedNameFirst(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// isRestrictedNameRest reports whether c may appear after the first restricted-name byte.
func isRestrictedNameRest(c byte) bool {
	if isRestrictedNameFirst(c) {
		return true
	}
	switch c {
	case '!', '#', '$', '&', '^', '_', '.', '+', '-':
		return true
	default:
		return false
	}
}

// isASCIIAlnum reports whether c is a lowercase ASCII letter or digit.
func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// formatSelector renders a six-field selector for error messages.
func formatSelector(s Selector) string {
	return strings.Join(
		[]string{s.Architecture, s.Target, s.Representation, FormatUsage(s.Usage), s.Role, s.Compression},
		", ",
	)
}

// FormatUsage renders a usage set for error messages. A present value is quoted
// because it is itself comma-separated, and the surrounding messages join their
// fields with commas; the empty set is usage=<empty>, which no basic token can
// spell.
func FormatUsage(usage string) string {
	if usage == "" {
		return "usage=<empty>"
	}
	return `usage="` + usage + `"`
}

// ErrRule is the sentinel wrapped by [ruleError] so callers can match spec §6
// rule failures with [errors.Is] without depending on error text.
var ErrRule = errors.New("spec rule")

// ruleError names the violated spec §6 rule number in the error text.
func ruleError(rule int, format string, args ...any) error {
	return fmt.Errorf("spec §6 rule %d: %s: %w", rule, fmt.Sprintf(format, args...), ErrRule)
}
