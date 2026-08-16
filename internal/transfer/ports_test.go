package transfer

import (
	"errors"
	"testing"

	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

func TestSentinelsDistinct(t *testing.T) {
	t.Parallel()
	if errors.Is(ErrNotFound, ErrUnauthorized) ||
		errors.Is(ErrNotFound, ErrDigestMismatch) ||
		errors.Is(ErrNotFound, ErrInvalidDocument) ||
		errors.Is(ErrNotFound, ErrSharedBlob) ||
		errors.Is(ErrDigestMismatch, ErrInvalidDocument) ||
		errors.Is(ErrSharedBlob, ErrInvalidDocument) {
		t.Fatal("sentinels must be distinct")
	}
}

func TestMocksSatisfyPorts(t *testing.T) {
	t.Parallel()
	var _ Manifests = (*regmocks.MockManifests)(nil)
	var _ Blobs = (*regmocks.MockBlobs)(nil)
}
