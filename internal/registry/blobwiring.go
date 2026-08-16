package registry

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/opencontainers/go-digest"

	blob "github.com/imgoci/go-oci-blob"

	"github.com/imgoci/go/internal/retry"
	"github.com/imgoci/go/internal/transfer"
)

// blobAdapter implements [transfer.Blobs] with a go-oci-blob client whose
// inner retry policy is one attempt. Exists and Pull run under [retry.Do]
// using the owning [Client]'s policy. Push does not: adapter-level retry
// cannot rewind the caller's reader. The verifying wrapper forwards Seek
// when the source is an [io.Seeker] so go-oci-blob can set req.GetBody and
// the auth stack can replay a 401 on the commit PUT.
type blobAdapter struct {
	// inner is the go-oci-blob client.
	inner *blob.Client
	// repo is the bound host and repository name.
	repo blob.Repository
	// owner is the [Client] whose retry policy Exists and Pull share.
	owner *Client
}

// newBlobAdapter constructs the go-oci-blob client with an authenticated
// path-scoped registry transport, unconditionally identity-wrapped storage
// transport, RetryPolicy{} (one attempt), write redirects off, and PlainHTTP
// from cfg.
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
// content from r. This method does not retry at the adapter: a non-seekable
// source cannot be rewound. A seekable source is wrapped as an
// [io.ReadSeeker] so go-oci-blob can replay the commit PUT after a 401.
//
// The reader is wrapped so bytes actually streamed are hashed and counted. A
// mismatch with dgst or size fails wrapping [transfer.ErrDigestMismatch] as
// source-mutation detection. go-oci-blob and a conforming registry also verify;
// this wrapper makes wrong bytes under a declared digest impossible even
// against a registry that skips commit checks. A rewind to offset 0 resets the
// running hash so a replay is checked independently of the refused attempt.
func (b *blobAdapter) Push(ctx context.Context, dgst digest.Digest, size int64, r io.Reader) error {
	return wrapBlobError(b.inner.Push(ctx, b.repo, dgst, size, newVerifyingReader(r, dgst, size)))
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

// verifyingReader hashes bytes as the wire consumes them and, at EOF,
// requires the digest and byte count to match the values declared at Push.
// Extra bytes past size fail immediately so a long source cannot hide
// behind go-oci-blob's trailing-byte probe.
type verifyingReader struct {
	// r is the caller source. Nil is treated as an empty stream.
	r io.Reader
	// dgst is the pass-1 digest the streamed bytes must match.
	dgst digest.Digest
	// size is the pass-1 byte count the streamed bytes must match.
	size int64
	// n is the number of bytes observed so far.
	n int64
	// h is the running SHA-256 of observed bytes.
	h hash.Hash
}

// verifyingReadSeeker is a [verifyingReader] that forwards Seek when the
// wrapped source is an [io.Seeker]. go-oci-blob type-asserts [io.ReadSeeker]
// to set req.GetBody; without this method a 401 on the blob commit PUT fails
// with a non-replayable body instead of re-authenticating.
type verifyingReadSeeker struct {
	*verifyingReader
}

// newVerifyingReader wraps r so Push can detect a source that mutates after
// pass 1. This is defense-in-depth, not a substitute for the documented
// source-stability precondition. When r is an [io.Seeker], the wrapper is
// itself an [io.ReadSeeker] so auth replay can rewind to offset 0.
func newVerifyingReader(r io.Reader, dgst digest.Digest, size int64) io.Reader {
	inner := &verifyingReader{r: r, dgst: dgst, size: size, h: sha256.New()}
	if _, ok := r.(io.Seeker); ok {
		return &verifyingReadSeeker{verifyingReader: inner}
	}

	return inner
}

// Read copies from the source, hashes the bytes, and checks digest and
// count when the stream ends or overruns the declared size.
func (r *verifyingReader) Read(p []byte) (int, error) {
	if r.r == nil {
		return 0, r.check(io.EOF)
	}
	n, err := r.r.Read(p)
	if n > 0 {
		if _, werr := r.h.Write(p[:n]); werr != nil {
			return n, werr
		}
		r.n += int64(n)
		if r.n > r.size {
			return 0, r.diverged()
		}
	}
	if err == nil {
		return n, nil
	}

	return n, r.check(err)
}

// check verifies digest and count at EOF and otherwise returns err.
func (r *verifyingReader) check(err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	if r.n != r.size {
		return r.diverged()
	}
	got := digest.NewDigest(digest.SHA256, r.h)
	if got != r.dgst {
		return r.diverged()
	}

	return io.EOF
}

// diverged names a source-mutation detection wrapping [transfer.ErrDigestMismatch].
func (r *verifyingReader) diverged() error {
	return fmt.Errorf("bytes streamed diverged from pass-1 digest: %w", transfer.ErrDigestMismatch)
}

// Seek forwards to the wrapped [io.Seeker]. A rewind to offset 0 resets the
// hash and byte count so a replay is verified from scratch. Any other
// offset is rejected: go-oci-blob only replays from the captured start,
// which for a Push source is offset 0.
func (r *verifyingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := r.r.(io.Seeker)
	if !ok {
		return 0, errors.New("verifyingReader: source does not support Seek")
	}
	abs, err := seeker.Seek(offset, whence)
	if err != nil {
		return abs, err
	}
	if abs != 0 {
		return abs, fmt.Errorf("verifyingReader: replay supports rewind to offset 0 only, got %d", abs)
	}
	r.n = 0
	r.h.Reset()

	return 0, nil
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
