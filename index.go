package imgoci

import (
	"maps"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

// Index is an immutable view of a fully validated release index. [ParseIndex]
// records the SHA-256 digest of the original input bytes; that digest is the
// identity of the encoded release.
type Index struct {
	// digest is the SHA-256 of the canonical input bytes.
	digest digest.Digest
	// name is the io.imgoci.name root annotation.
	name string
	// version is the org.opencontainers.image.version root annotation.
	version string
	// entries are the file entries in canonical descriptor order.
	entries []FileEntry
	// annotations is the root annotation map, including unknown keys.
	annotations map[string]string
}

// Digest returns the SHA-256 digest of the canonical input bytes that produced
// this index. The digest is computed from those original bytes; [ParseIndex]
// never re-encodes for identity.
func (x *Index) Digest() digest.Digest {
	if x == nil {
		return ""
	}
	return x.digest
}

// Name returns the io.imgoci.name root annotation.
func (x *Index) Name() string {
	if x == nil {
		return ""
	}
	return x.name
}

// Version returns the org.opencontainers.image.version root annotation.
func (x *Index) Version() string {
	if x == nil {
		return ""
	}
	return x.version
}

// Entries returns the file entries in canonical descriptor order. The slice
// and every entry's Annotations map are freshly copied on every call.
func (x *Index) Entries() []FileEntry {
	if x == nil {
		return nil
	}
	return cloneEntries(x.entries)
}

// Annotations returns a copy of the root annotation map, including unknown
// keys. The map is freshly copied on every call.
func (x *Index) Annotations() map[string]string {
	if x == nil {
		return nil
	}
	return cloneAnnotations(x.annotations)
}

// indexFromValue maps a validated codec value onto the public immutable view.
func indexFromValue(v *index.Value, dgst digest.Digest) *Index {
	entries := make([]FileEntry, 0, len(v.Manifests))
	for i := range v.Manifests {
		entries = append(entries, fileEntryFromDescriptor(v.Manifests[i]))
	}
	annotations := cloneAnnotations(v.Annotations)
	return &Index{
		digest:      dgst,
		name:        annotations[index.AnnotationName],
		version:     annotations[index.AnnotationVersion],
		entries:     entries,
		annotations: annotations,
	}
}

// fileEntryFromDescriptor copies one codec descriptor into a public FileEntry.
func fileEntryFromDescriptor(d index.Descriptor) FileEntry {
	sel := d.Selector()
	return FileEntry{
		MediaType:    d.MediaType,
		ArtifactType: d.ArtifactType,
		Digest:       d.Digest,
		Size:         d.Size,
		Selector: Selector{
			Architecture:   sel.Architecture,
			Target:         sel.Target,
			Representation: sel.Representation,
			Usage:          usageFromCanonical(sel.Usage),
			Role:           sel.Role,
			Compression:    sel.Compression,
		},
		ContentDigest: d.ContentDigest(),
		ContentSize:   d.ContentSize(),
		Filename:      d.Filename(),
		Annotations:   cloneAnnotations(d.Annotations),
	}
}

// cloneEntries returns a new slice whose Annotation maps are also copied.
func cloneEntries(entries []FileEntry) []FileEntry {
	if entries == nil {
		return nil
	}
	out := make([]FileEntry, len(entries))
	for i, entry := range entries {
		out[i] = entry
		out[i].Annotations = cloneAnnotations(entry.Annotations)
	}
	return out
}

// cloneAnnotations returns a shallow copy of m. A nil input stays nil.
func cloneAnnotations(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}
