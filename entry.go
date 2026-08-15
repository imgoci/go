package imgoci

import "github.com/opencontainers/go-digest"

// Selector is the five-field identity of one file-entry descriptor. Values are
// compared exactly and case-sensitively, as spec section 5.3 requires.
type Selector struct {
	// Architecture is the io.imgoci.architecture value.
	Architecture string
	// Target is the io.imgoci.target value.
	Target string
	// Representation is the io.imgoci.representation value.
	Representation string
	// Role is the io.imgoci.role value.
	Role string
	// Compression is the io.imgoci.compression value.
	Compression string
}

// FileEntry is one descriptor in a validated release index. MediaType and
// ArtifactType are preserved as written; compare them with [EqualMediaType].
type FileEntry struct {
	// MediaType is the descriptor mediaType as written.
	MediaType string
	// ArtifactType is the descriptor artifactType as written. It declares the
	// referenced file-manifest type and is capability metadata, not a selector.
	ArtifactType string
	// Digest is the SHA-256 digest of the referenced file manifest.
	Digest digest.Digest
	// Size is the byte length of the referenced file manifest.
	Size int64
	// Selector is the five-field file-entry identity.
	Selector Selector
	// ContentDigest is the SHA-256 digest of the decoded content.
	ContentDigest digest.Digest
	// ContentSize is the byte length of the decoded content.
	ContentSize int64
	// Filename is the producer-chosen filename for the decoded content.
	Filename string
	// Annotations is a copy of every descriptor annotation, including unknown
	// keys. [Index.Entries] and [Resolved.Entries] copy the map on every call,
	// so mutating a returned [FileEntry] cannot change the source view.
	Annotations map[string]string
}
