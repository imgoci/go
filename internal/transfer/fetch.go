package transfer

import (
	"context"
	"fmt"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/index"
)

// IndexBytes is the original index document [FetchIndex] retrieved.
//
// Parse and ten-rule validation live in the root package. FetchIndex cannot
// call ParseIndex: the root package will import transfer, and a reverse
// import would cycle. The HTTP Content-Type identity check lives here and
// wraps [ErrInvalidDocument].
type IndexBytes struct {
	// Bytes are the original registry bytes. Identity is these bytes, never a
	// re-encoding.
	Bytes []byte
	// Digest is SHA-256 of Bytes, computed with [digest.FromBytes].
	Digest digest.Digest
	// ContentType is the parameter-free Content-Type the adapter reported.
	ContentType string
}

// FetchIndex performs spec §7.1 through hashing.
//
// It GETs ref with Accept set to the release-index media type, requires the
// returned Content-Type to identify that same type under
// [index.EqualMediaType] ([ErrInvalidDocument]), hashes the original bytes,
// and when pinned is not empty requires the digest to equal pinned
// ([ErrDigestMismatch]).
func FetchIndex(ctx context.Context, manifests Manifests, ref string, pinned digest.Digest) (*IndexBytes, error) {
	raw, contentType, err := manifests.Get(ctx, ref, index.MediaTypeIndex)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	if !index.EqualMediaType(contentType, index.MediaTypeIndex) {
		return nil, fmt.Errorf(
			"index content type %q does not identify %s: %w",
			contentType, index.MediaTypeIndex, ErrInvalidDocument,
		)
	}
	got := digest.FromBytes(raw)
	if pinned != "" && got != pinned {
		return nil, fmt.Errorf("index digest %s does not match pin %s: %w", got, pinned, ErrDigestMismatch)
	}
	return &IndexBytes{Bytes: raw, Digest: got, ContentType: contentType}, nil
}
