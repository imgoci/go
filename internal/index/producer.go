package index

import (
	"fmt"
	"strings"
)

// Target names from the spec §5.4 public target registry. Every registry in
// this file tracks the spec revision in testdata/conformance/SPEC_COMMIT.
const (
	targetAliyun       = "aliyun"
	targetApplehv      = "applehv"
	targetAws          = "aws"
	targetAzure        = "azure"
	targetAzurestack   = "azurestack"
	targetDigitalocean = "digitalocean"
	targetExoscale     = "exoscale"
	targetGcp          = "gcp"
	targetHetzner      = "hetzner"
	targetHyperv       = "hyperv"
	targetIbmcloud     = "ibmcloud"
	targetIncus        = "incus"
	targetKubevirt     = "kubevirt"
	targetMetal        = "metal"
	targetNutanix      = "nutanix"
	targetOpenstack    = "openstack"
	targetOraclecloud  = "oraclecloud"
	targetPowervs      = "powervs"
	targetProxmoxve    = "proxmoxve"
	targetQemu         = "qemu"
	targetVirtualbox   = "virtualbox"
	targetVmware       = "vmware"
	targetVultr        = "vultr"
)

// Representation names from the spec §5.4 public representation registry.
const (
	representationRaw          = "raw"
	representationRaw4kn       = "raw-4kn"
	representationQcow2        = "qcow2"
	representationIncusVM      = "incus-vm"
	representationISO          = "iso"
	representationLinuxNetboot = "linux-netboot"
)

// Compression names from the spec §5.4 public compression registry.
const (
	compressionNone = "none"
	compressionGzip = "gzip"
	compressionXz   = "xz"
	compressionZstd = "zstd"
)

// producerTargets returns the spec §5.4 public target registry.
func producerTargets() map[string]struct{} {
	return map[string]struct{}{
		targetAliyun:       {},
		targetApplehv:      {},
		targetAws:          {},
		targetAzure:        {},
		targetAzurestack:   {},
		targetDigitalocean: {},
		targetExoscale:     {},
		targetGcp:          {},
		targetHetzner:      {},
		targetHyperv:       {},
		targetIbmcloud:     {},
		targetIncus:        {},
		targetKubevirt:     {},
		targetMetal:        {},
		targetNutanix:      {},
		targetOpenstack:    {},
		targetOraclecloud:  {},
		targetPowervs:      {},
		targetProxmoxve:    {},
		targetQemu:         {},
		targetVirtualbox:   {},
		targetVmware:       {},
		targetVultr:        {},
	}
}

// producerRepresentations returns the spec §5.4 public representation registry.
func producerRepresentations() map[string]struct{} {
	return map[string]struct{}{
		representationRaw:          {},
		representationRaw4kn:       {},
		representationQcow2:        {},
		representationIncusVM:      {},
		representationISO:          {},
		representationLinuxNetboot: {},
	}
}

// producerRoles returns the spec §5.4 public role registry.
func producerRoles() map[string]struct{} {
	return map[string]struct{}{
		roleDisk:      {},
		roleKernel:    {},
		roleInitramfs: {},
		roleMetadata:  {},
		roleRootfs:    {},
	}
}

// producerCompressions returns the spec §5.4 public compression registry.
func producerCompressions() map[string]struct{} {
	return map[string]struct{}{
		compressionNone: {},
		compressionGzip: {},
		compressionXz:   {},
		compressionZstd: {},
	}
}

// producerRegistries holds the spec §5.4 public selector registries for one
// [Build] call.
type producerRegistries struct {
	// targets is the public target registry.
	targets map[string]struct{}
	// representations is the public representation registry.
	representations map[string]struct{}
	// roles is the public role registry.
	roles map[string]struct{}
	// compressions is the public compression registry.
	compressions map[string]struct{}
}

// newProducerRegistries builds the spec §5.4 public selector registries.
func newProducerRegistries() producerRegistries {
	return producerRegistries{
		targets:         producerTargets(),
		representations: producerRepresentations(),
		roles:           producerRoles(),
		compressions:    producerCompressions(),
	}
}

// isRootOnlyAnnotation reports whether key is defined only on the release-index
// root.
func isRootOnlyAnnotation(key string) bool {
	switch key {
	case AnnotationName, AnnotationVersion:
		return true
	default:
		return false
	}
}

// isDescriptorOnlyAnnotation reports whether key is defined only on file-entry
// descriptors.
func isDescriptorOnlyAnnotation(key string) bool {
	switch key {
	case AnnotationArchitecture,
		AnnotationTarget,
		AnnotationRepresentation,
		AnnotationRole,
		AnnotationCompression,
		AnnotationContentDigest,
		AnnotationContentSize,
		AnnotationFilename:
		return true
	default:
		return false
	}
}

// validateProducerModel applies the producer-only selector-registry and
// annotation-location rules to m.
func validateProducerModel(m *Model) error {
	if err := validateProducerAnnotationMap(
		"root annotations",
		m.Annotations,
		isDescriptorOnlyAnnotation,
		"descriptor-only",
	); err != nil {
		return err
	}
	registries := newProducerRegistries()
	for i, e := range m.Entries {
		if err := validateProducerSelector(i, e.Selector, registries); err != nil {
			return err
		}
		if err := validateProducerAnnotationMap(
			fmt.Sprintf("entries[%d] annotations", i),
			e.Annotations,
			isRootOnlyAnnotation,
			"root-only",
		); err != nil {
			return err
		}
	}
	return nil
}

// validateProducerSelector checks the four imgoci-owned selector fields against
// their spec §5.4 registries. Architecture has no registry and is not checked
// here.
func validateProducerSelector(i int, sel Selector, registries producerRegistries) error {
	if err := validateProducerRegistryValue(i, AnnotationTarget, sel.Target, registries.targets); err != nil {
		return err
	}
	if err := validateProducerRegistryValue(
		i,
		AnnotationRepresentation,
		sel.Representation,
		registries.representations,
	); err != nil {
		return err
	}
	if err := validateProducerRegistryValue(i, AnnotationRole, sel.Role, registries.roles); err != nil {
		return err
	}
	return validateProducerRegistryValue(i, AnnotationCompression, sel.Compression, registries.compressions)
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
// annotation location, as reported by forbidden.
func validateProducerAnnotationMap(
	path string,
	annotations map[string]string,
	forbidden func(key string) bool,
	kind string,
) error {
	for key := range annotations {
		if forbidden(key) {
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
