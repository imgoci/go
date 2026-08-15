package index

import (
	"slices"
	"strings"
)

// sortManifests orders descriptors by the spec §9 five-field UTF-8 tuple.
func sortManifests(manifests []Descriptor) {
	slices.SortStableFunc(manifests, descriptorOrder)
}

// manifestsInCanonicalOrder reports whether manifests already follow spec §9 order.
func manifestsInCanonicalOrder(manifests []Descriptor) bool {
	return slices.IsSortedFunc(manifests, descriptorOrder)
}

// descriptorOrder compares two descriptors by architecture, target,
// representation, role, and compression in ascending UTF-8 byte order.
func descriptorOrder(a, b Descriptor) int {
	as, bs := a.Selector(), b.Selector()
	if c := strings.Compare(as.Architecture, bs.Architecture); c != 0 {
		return c
	}
	if c := strings.Compare(as.Target, bs.Target); c != 0 {
		return c
	}
	if c := strings.Compare(as.Representation, bs.Representation); c != 0 {
		return c
	}
	if c := strings.Compare(as.Role, bs.Role); c != 0 {
		return c
	}
	return strings.Compare(as.Compression, bs.Compression)
}
