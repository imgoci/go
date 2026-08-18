package index

import "github.com/opencontainers/go-digest"

// Entry is one file-entry descriptor with its selector and typed descriptor
// values materialized, so a query reads fields instead of re-deriving them
// from the annotation map on every access.
type Entry struct {
	// MediaType is the descriptor media type as written.
	MediaType string
	// ArtifactType is the referenced manifest's artifact type as written.
	ArtifactType string
	// Digest is the SHA-256 digest of the referenced file manifest.
	Digest digest.Digest
	// Size is the byte length of the referenced file manifest.
	Size int64
	// Selector is the six-field identity materialized from the descriptor annotations.
	Selector Selector
	// ContentDigest is io.imgoci.content.digest parsed as a digest.
	ContentDigest digest.Digest
	// ContentSize is io.imgoci.content.size parsed as an int64.
	ContentSize int64
	// Filename is the io.imgoci.filename annotation.
	Filename string
	// Annotations is the descriptor annotation map, including unknown keys.
	// The map is shared with the source descriptor; public exit paths must clone it.
	Annotations map[string]string
}

// EntriesOf materializes every descriptor in v, in canonical document order.
func EntriesOf(v *Value) []Entry {
	out := make([]Entry, len(v.Manifests))
	for i, d := range v.Manifests {
		out[i] = Entry{
			MediaType:     d.MediaType,
			ArtifactType:  d.ArtifactType,
			Digest:        d.Digest,
			Size:          d.Size,
			Selector:      d.Selector(),
			ContentDigest: d.ContentDigest(),
			ContentSize:   d.ContentSize(),
			Filename:      d.Filename(),
			Annotations:   d.Annotations,
		}
	}
	return out
}
