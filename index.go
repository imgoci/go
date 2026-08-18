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
	// entries are the materialized file entries in canonical descriptor order.
	entries []index.Entry
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
	return fileEntriesFrom(x.entries)
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
	annotations := cloneAnnotations(v.Annotations)
	return &Index{
		digest:      dgst,
		name:        annotations[index.AnnotationName],
		version:     annotations[index.AnnotationVersion],
		entries:     index.EntriesOf(v),
		annotations: annotations,
	}
}

// fileEntryFrom copies one internal entry into a public FileEntry.
func fileEntryFrom(e index.Entry) FileEntry {
	return FileEntry{
		MediaType:    e.MediaType,
		ArtifactType: e.ArtifactType,
		Digest:       e.Digest,
		Size:         e.Size,
		Selector: Selector{
			Architecture:   e.Selector.Architecture,
			Target:         e.Selector.Target,
			Representation: e.Selector.Representation,
			Usage:          usageFromCanonical(e.Selector.Usage),
			Role:           e.Selector.Role,
			Compression:    e.Selector.Compression,
		},
		ContentDigest: e.ContentDigest,
		ContentSize:   e.ContentSize,
		Filename:      e.Filename,
		Annotations:   cloneAnnotations(e.Annotations),
	}
}

// fileEntriesFrom maps entries into public FileEntry values.
// A nil input stays nil. Each annotation map is copied.
func fileEntriesFrom(entries []index.Entry) []FileEntry {
	if entries == nil {
		return nil
	}
	out := make([]FileEntry, len(entries))
	for i := range entries {
		out[i] = fileEntryFrom(entries[i])
	}
	return out
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
