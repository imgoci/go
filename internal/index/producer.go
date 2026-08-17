package index

import (
	"fmt"
	"strings"
)

// producerTargets is the spec §5.4 public target registry at the pinned spec commit.
var producerTargets = map[string]struct{}{
	"aliyun":       {},
	"applehv":      {},
	"aws":          {},
	"azure":        {},
	"azurestack":   {},
	"digitalocean": {},
	"exoscale":     {},
	"gcp":          {},
	"hetzner":      {},
	"hyperv":       {},
	"ibmcloud":     {},
	"incus":        {},
	"kubevirt":     {},
	"metal":        {},
	"nutanix":      {},
	"openstack":    {},
	"oraclecloud":  {},
	"powervs":      {},
	"proxmoxve":    {},
	"qemu":         {},
	"virtualbox":   {},
	"vmware":       {},
	"vultr":        {},
}

// producerRepresentations is the spec §5.4 public representation registry at the pinned spec commit.
var producerRepresentations = map[string]struct{}{
	"raw":           {},
	"raw-4kn":       {},
	"qcow2":         {},
	"incus-vm":      {},
	"iso":           {},
	"linux-netboot": {},
}

// producerRoles is the spec §5.4 public role registry at the pinned spec commit.
var producerRoles = map[string]struct{}{
	"disk":      {},
	"kernel":    {},
	"initramfs": {},
	"metadata":  {},
	"rootfs":    {},
}

// producerCompressions is the spec §5.4 public compression registry at the pinned spec commit.
var producerCompressions = map[string]struct{}{
	"none": {},
	"gzip": {},
	"xz":   {},
	"zstd": {},
}

// producerRootOnlyAnnotations are defined only on the release-index root.
var producerRootOnlyAnnotations = map[string]struct{}{
	AnnotationName:    {},
	AnnotationVersion: {},
}

// producerDescriptorOnlyAnnotations are defined only on file-entry descriptors.
var producerDescriptorOnlyAnnotations = map[string]struct{}{
	AnnotationArchitecture:   {},
	AnnotationTarget:         {},
	AnnotationRepresentation: {},
	AnnotationRole:           {},
	AnnotationCompression:    {},
	AnnotationContentDigest:  {},
	AnnotationContentSize:    {},
	AnnotationFilename:       {},
}

// validateProducerModel applies producer-only selector-registry and
// annotation-location rules to m. [Validate] does not apply these rules.
func validateProducerModel(m *Model) error {
	if err := validateProducerAnnotationMap("root annotations", m.Annotations, producerDescriptorOnlyAnnotations, "descriptor-only"); err != nil {
		return err
	}
	for i, e := range m.Entries {
		if err := validateProducerSelector(i, e.Selector); err != nil {
			return err
		}
		if err := validateProducerAnnotationMap(
			fmt.Sprintf("entries[%d] annotations", i),
			e.Annotations,
			producerRootOnlyAnnotations,
			"root-only",
		); err != nil {
			return err
		}
	}
	return nil
}

// validateProducerSelector checks the four imgoci-owned selector fields against
// their §5.4 registries. Architecture is syntax-only and is not checked here.
func validateProducerSelector(i int, sel Selector) error {
	if err := validateProducerRegistryValue(i, AnnotationTarget, sel.Target, producerTargets); err != nil {
		return err
	}
	if err := validateProducerRegistryValue(i, AnnotationRepresentation, sel.Representation, producerRepresentations); err != nil {
		return err
	}
	if err := validateProducerRegistryValue(i, AnnotationRole, sel.Role, producerRoles); err != nil {
		return err
	}
	return validateProducerRegistryValue(i, AnnotationCompression, sel.Compression, producerCompressions)
}

// validateProducerRegistryValue accepts a public registry spelling or a
// well-formed private x-<owner>-<name> token.
func validateProducerRegistryValue(i int, field, value string, registry map[string]struct{}) error {
	if _, ok := registry[value]; ok {
		return nil
	}
	if isPrivateSelector(value) {
		return nil
	}
	return producerError("entries[%d] %s %q is not a public value or x-<owner>-<name>", i, field, value)
}

// validateProducerAnnotationMap rejects defined keys that belong at the other
// annotation location.
func validateProducerAnnotationMap(path string, annotations map[string]string, forbidden map[string]struct{}, kind string) error {
	for key := range annotations {
		if _, ok := forbidden[key]; ok {
			return producerError("%s: %s is %s", path, key, kind)
		}
	}
	return nil
}

// isPrivateSelector reports whether s matches spec §5.3 private selector form
// x-<owner>-<name>, with owner and name each non-empty, and s a basic token.
func isPrivateSelector(s string) bool {
	if !isBasicToken(s) {
		return false
	}
	rest, ok := strings.CutPrefix(s, "x-")
	if !ok {
		return false
	}
	owner, name, ok := strings.Cut(rest, "-")
	return ok && owner != "" && name != ""
}

// producerError wraps [ErrRule] so publish maps the failure to ErrInvalidSpec.
func producerError(format string, args ...any) error {
	return fmt.Errorf("producer: %s: %w", fmt.Sprintf(format, args...), ErrRule)
}
