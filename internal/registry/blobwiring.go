package registry

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	blob "github.com/imgoci/go-oci-blob"

	"github.com/imgoci/go/internal/retry"
	"github.com/imgoci/go/internal/transfer"
)

// blobAdapter implements [transfer.Blobs] with a go-oci-blob client whose
// inner retry policy is one attempt. Exists and Pull run under [retry.Do]
// using the owning [Client]'s policy. Push does not: the port's reader is
// consumed once and never rewound.
type blobAdapter struct {
	// inner is the go-oci-blob client.
	inner *blob.Client
	// repo is the bound host and repository name.
	repo blob.Repository
	// owner is the [Client] whose retry policy Exists and Pull share.
	owner *Client
}

// newBlobAdapter constructs the go-oci-blob client bigoci-style
// (ARCHITECTURE.md §6.6.2): authenticated path-scoped registry transport,
// unconditionally identity-wrapped storage transport, RetryPolicy{} (one
// attempt), write redirects off, and PlainHTTP from cfg.
func newBlobAdapter(cfg Config, stacks transportStacks, owner *Client) *blobAdapter {
	return &blobAdapter{
		inner: blob.New(
			blob.WithTransport(stacks.registry),
			blob.WithStorageTransport(stacks.storage),
			blob.WithRetryPolicy(blob.RetryPolicy{}),
			blob.WithWriteRedirects(false),
			blob.WithPlainHTTP(cfg.PlainHTTP),
		),
		repo: blob.Repository{
			Host: cfg.Host,
			Name: cfg.Repository,
		},
		owner: owner,
	}
}

// Exists reports whether the repository holds the blob with digest dgst.
func (b *blobAdapter) Exists(ctx context.Context, dgst digest.Digest) (bool, error) {
	var ok bool
	err := retry.Do(ctx, b.owner.retry, func(ctx context.Context) error {
		var err error
		ok, err = b.inner.Exists(ctx, b.repo, dgst)

		return wrapBlobError(err)
	})

	return ok, err
}

// Push uploads the blob with digest dgst, size bytes long, reading its
// content from r. r is consumed once and never rewound, so this method
// does not retry.
func (b *blobAdapter) Push(ctx context.Context, dgst digest.Digest, size int64, r io.Reader) error {
	return wrapBlobError(b.inner.Push(ctx, b.repo, dgst, size, r))
}

// Pull opens the blob dgst names for reading. The returned reader verifies
// the digest at EOF the way go-oci-blob's verified reader does, mapping
// [blob.ErrDigestMismatch] onto [transfer.ErrDigestMismatch].
func (b *blobAdapter) Pull(ctx context.Context, dgst digest.Digest) (io.ReadCloser, error) {
	var rc io.ReadCloser
	err := retry.Do(ctx, b.owner.retry, func(ctx context.Context) error {
		var err error
		rc, err = b.inner.Pull(ctx, b.repo, dgst)
		if err != nil {
			return wrapBlobError(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &blobReader{rc: rc}, nil
}

// blobReader maps go-oci-blob sentinels on the verified stream onto the
// transfer port's sentinels. [io.EOF] passes through so a successful digest
// check stays a normal end of stream.
type blobReader struct {
	// rc is the go-oci-blob verified reader.
	rc io.ReadCloser
}

// Read maps a digest mismatch or retryable failure onto transfer sentinels
// and [retry.Transient]. EOF is unchanged.
func (r *blobReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if err == nil || errors.Is(err, io.EOF) {
		return n, err
	}

	return n, wrapBlobError(err)
}

// Close closes the underlying stream.
func (r *blobReader) Close() error {
	return r.rc.Close()
}

// wrapBlobError maps go-oci-blob sentinels onto [transfer] sentinels and
// tags retryable remainder with [retry.Transient]. Identity and auth
// classification runs first: a content-coding rejection from our wrapper
// is terminal even when go-oci-blob marked the transport error retryable,
// and [auth.ErrAuth] wraps [transfer.ErrUnauthorized]. Sentinel mapping
// then runs: a storage-origin 404 is retryable in go-oci-blob's table, but
// a missing blob is [transfer.ErrNotFound], not another attempt.
func wrapBlobError(err error) error {
	if err == nil {
		return nil
	}
	if ok, classified := classifyAdapterError(err); ok {
		return classified
	}
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return fmt.Errorf("blob: %w", transfer.ErrNotFound)
	case errors.Is(err, blob.ErrUnauthorized):
		return fmt.Errorf("blob: %w", transfer.ErrUnauthorized)
	case errors.Is(err, blob.ErrDigestMismatch):
		return fmt.Errorf("blob: %w", transfer.ErrDigestMismatch)
	}
	if after, ok := blob.Retryable(err); ok {
		return retry.Transient(err, after)
	}

	return err
}
