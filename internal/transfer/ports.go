package transfer

import (
	"context"
	"errors"
	"io"

	"github.com/opencontainers/go-digest"
)

// Sentinel errors the orchestrator and its adapters share. Adapters wrap
// registry outcomes so [errors.Is] matches these values; the public root
// package maps them onto the stable imgoci sentinels.
var (
	// ErrNotFound reports that a registry does not hold the requested
	// manifest or blob.
	ErrNotFound = errors.New("not found")

	// ErrUnauthorized reports that a registry refused a request for lack of
	// credentials or insufficient permission.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrDigestMismatch reports that retrieved bytes did not match a declared
	// digest or size.
	ErrDigestMismatch = errors.New("digest mismatch")

	// ErrInvalidDocument reports that a retrieved release index or file
	// manifest failed a consumer identity or validation check.
	ErrInvalidDocument = errors.New("invalid document")

	// ErrSharedBlob reports that sources hashed to the same stored digest
	// but disagreed on decoded content or compression. On the producer path
	// that is spec §6 rule 8, before a file-manifest digest exists.
	ErrSharedBlob = errors.New("shared blob disagreement")
)

// Manifests is the OCI Distribution manifest surface of one repository.
//
// Get fetches a manifest or index by tag or digest reference with an exact
// Accept value and returns the original response bytes plus the
// parameter-free Content-Type. Put publishes bytes at a digest or tag with
// an exact Content-Type.
//
// Addressing is per call so Fetch can name a tag and later fetches can name
// a digest. An implementation is still bound to one repository at
// construction; ref is the tag or "sha256:…" digest within that repository,
// not a registry/name string.
//
// Implementations own their own bounded retry (internal/retry); the
// orchestrator must not wrap these calls. Every method must be safe for
// concurrent use.
type Manifests interface {
	// Get retrieves the manifest or index at ref, sending Accept as the
	// request Accept header. It returns the original bytes and the
	// parameter-stripped Content-Type.
	Get(ctx context.Context, ref, accept string) (raw []byte, contentType string, err error)

	// Put publishes raw at ref (a digest or a tag) with the given mediaType
	// as Content-Type.
	Put(ctx context.Context, ref, mediaType string, raw []byte) error
}

// Blobs is the distribution-spec blob surface of one repository, matching
// the go-oci-blob Exists/Push/Pull shape with the repository bound at
// construction so no method names one.
//
// Implementations own their own bounded retry (internal/retry); the
// orchestrator must not wrap these calls. Every method must be safe for
// concurrent use.
type Blobs interface {
	// Exists reports whether the repository holds the blob with digest dgst.
	// A registry answering that it does not hold the blob is (false, nil).
	Exists(ctx context.Context, dgst digest.Digest) (bool, error)

	// Push uploads the blob with digest dgst, size bytes long, reading its
	// content from r. r is consumed once and never rewound.
	Push(ctx context.Context, dgst digest.Digest, size int64, r io.Reader) error

	// Pull opens the blob dgst names for reading. The caller owns the
	// returned reader and must close it. A missing blob is an error matching
	// [ErrNotFound].
	Pull(ctx context.Context, dgst digest.Digest) (io.ReadCloser, error)
}

// Ports is the Manifests and Blobs pair one repository transfer consumes.
type Ports struct {
	// Manifests is the OCI Distribution manifest surface. Required.
	Manifests Manifests
	// Blobs is the distribution-spec blob surface. Required.
	Blobs Blobs
}
