package imgoci

import "github.com/opencontainers/go-digest"

// Release is a fetched, fully validated release index pinned to one
// repository.
//
// Digest is the SHA-256 of the original index bytes and equals
// [Index.Digest]. After Fetch, later [Client.FetchFiles] calls
// address the same registry host and repository; file manifests are named by
// digest, not by the tag that Fetch may have started from. A tag mutation
// between Fetch and FetchFiles therefore cannot redirect retrieval.
//
// A Release is immutable and safe for concurrent use.
type Release struct {
	// digest is the SHA-256 of the original index bytes.
	digest digest.Digest
	// index is the validated public view of those bytes.
	index *Index
	// host is the registry domain the index was fetched from.
	host string
	// repository is the path under /v2 the index was fetched from.
	repository string
}

// Digest returns the SHA-256 of the original index bytes. It equals
// [Index.Digest].
func (r *Release) Digest() digest.Digest {
	if r == nil {
		return ""
	}

	return r.digest
}

// Index returns the validated public view of the fetched release index.
func (r *Release) Index() *Index {
	if r == nil {
		return nil
	}

	return r.index
}
