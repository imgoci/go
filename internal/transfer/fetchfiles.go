package transfer

import (
	"context"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync"

	"github.com/opencontainers/go-digest"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/file"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
)

const (
	// defaultWorkers is the FetchFiles worker count when Workers is not positive.
	defaultWorkers = 4
	// compressionNone is the spec compression name that stores decoded bytes.
	compressionNone = "none"
)

// Entry is one selected file-entry descriptor [FetchFiles] retrieves.
type Entry struct {
	// Role is io.imgoci.role, the staging/commit key.
	Role string
	// MediaType is the descriptor mediaType as written.
	MediaType string
	// ArtifactType is the descriptor artifactType as written.
	//
	// Resolve and Capabilities classify form by this field: the imgoci
	// standard file type vs [index.ArtifactTypeBigOCI].
	ArtifactType string
	// Compression is io.imgoci.compression.
	Compression string
	// Digest is the SHA-256 digest of the referenced file manifest.
	Digest digest.Digest
	// Size is the byte length of the referenced file manifest.
	Size int64
	// ContentDigest is the SHA-256 digest of the decoded content.
	ContentDigest digest.Digest
	// ContentSize is the byte length of the decoded content.
	ContentSize int64
	// Filename is the producer-chosen filename for the decoded content.
	Filename string
}

// FetchFilesRequest is the input to [FetchFiles].
type FetchFilesRequest struct {
	// Manifests is the repository manifest port.
	Manifests Manifests
	// Blobs is the repository blob port.
	Blobs Blobs
	// Multipart is the BigOCI pull port. Required when any entry is BigOCI
	// form; a nil value is a wiring bug and fails that entry with a
	// descriptive error, not [ErrUnsupportedType].
	Multipart Multipart
	// Repository is the repository-only reference (registry/name) that
	// [Multipart.PullTo] addresses. Ignored on the standard path.
	Repository string
	// Entries are the selected file entries, in commit order.
	Entries []Entry
	// ByRole maps each role to its final destination path.
	ByRole map[string]string
	// Workers is the maximum concurrent entry fetches. Values <= 0 become 4.
	// The effective count never exceeds len(Entries).
	Workers int
	// Progress receives serialized absolute snapshots. It may be nil.
	Progress func(Progress)
	// DecoderMaxWindow caps the working set one decompressor may allocate:
	// the zstd window or xz LZMA2 dictionary a stored file declares. Zero
	// means [decomp.DefaultDecoderMaxWindow]; the root package always sets
	// it from the client option.
	DecoderMaxWindow uint64
}

// FetchFiles retrieves and verifies every entry, then commits destinations.
//
// Spec §8: [file.NewPlan] runs first so an invalid ByRole produces zero
// registry Get calls. Each entry GETs the manifest by digest and checks
// digest, size, and Content-Type. Form is the entry's ArtifactType compared
// with [index.EqualMediaType] against [index.ArtifactTypeBigOCI].
//
// Standard entries Pull the layer through [decomp.NewBoundedReader] and
// [decomp.Decoder] into a [decomp.CountingHashWriter] over staging. BigOCI
// entries populate a [file.StoredCache] keyed by io.bigoci.file.digest, then
// decode the stored file into staging. On both paths compression "none" is
// prechecked: the manifest's stored digest and size must already equal the
// entry ContentDigest and ContentSize. WireBytes and Retries from both paths
// merge into one serialized stream, latest-absolute per transfer. Cache
// entries are removed best-effort after a successful commit and retained on
// any failure.
//
// Commit runs only when every role verified, in input entry order. Any
// failure before commit cancels outstanding work, commits nothing, and
// calls plan.Cleanup. A [*file.CommitError] is returned unwrapped.
func FetchFiles(ctx context.Context, req FetchFilesRequest) error {
	plan, err := file.NewPlan(req.ByRole)
	if err != nil {
		return fmt.Errorf("destination plan: %w", err)
	}
	defer func() { _ = plan.Cleanup() }()

	progress := newReporter(req.Entries, req.Progress)
	ctx = progress.bindContext(ctx)
	if len(req.Entries) == 0 {
		if err := plan.Commit(nil); err != nil {
			return err
		}
		progress.finish()
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg     sync.WaitGroup
		first  firstFailure
		caches storedCaches
	)
	record := func(err error) {
		if err == nil {
			return
		}
		first.record(err)
		cancel()
	}

	sem := make(chan struct{}, workerCount(req.Workers, len(req.Entries)))
	for i := range req.Entries {
		entry := req.Entries[i]
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			record(fetchEntry(ctx, req, plan, progress, &caches, entry))
		})
	}
	wg.Wait()

	if err := first.result(); err != nil {
		return err
	}

	order := make([]string, len(req.Entries))
	for i, e := range req.Entries {
		order[i] = e.Role
	}
	if err := plan.Commit(order); err != nil {
		return err
	}
	// Post-commit cache removal is best-effort. A held lock or canceled ctx must
	// not fail a committed fetch; the terminal snapshot always fires.
	_ = caches.remove(ctx)
	progress.finish()
	return nil
}

// firstFailure holds the first worker error. A later non-context error replaces
// a [context.Canceled] or [context.DeadlineExceeded] already in the slot so a
// verification failure is not lost to cancellation racing into the slot.
type firstFailure struct {
	// mu guards err.
	mu sync.Mutex
	// err is the preferred first failure, or nil.
	err error
}

// record stores err if the slot is empty, or if the slot holds a
// context-derived error and err is not context-derived. The caller must
// invoke record before canceling sibling workers.
func (f *firstFailure) record(err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err == nil || (contextDerived(f.err) && !contextDerived(err)) {
		f.err = err
	}
}

// result returns the preferred first failure. It is safe after all workers
// have returned.
func (f *firstFailure) result() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.err
}

// contextDerived reports whether err is or wraps context cancellation or
// deadline expiry.
func contextDerived(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// workerCount returns the effective worker pool size.
func workerCount(requested, entries int) int {
	n := requested
	if n <= 0 {
		n = defaultWorkers
	}
	if n > entries {
		n = entries
	}
	if n < 1 {
		return 1
	}
	return n
}

// decoderMaxWindow returns the effective decoder working-set ceiling.
//
// Zero on a request means the field was never set — the root package always
// sets it from the client option — and resolves to
// [decomp.DefaultDecoderMaxWindow]. Zero is never a request for an unbounded
// decoder.
func decoderMaxWindow(requested uint64) uint64 {
	if requested == 0 {
		return decomp.DefaultDecoderMaxWindow
	}
	return requested
}

// cacheUse is one verified stored-cache entry to delete after a successful commit.
type cacheUse struct {
	// cache holds the per-parent stored cache.
	cache *file.StoredCache
	// key is io.bigoci.file.digest.
	key digest.Digest
}

// storedCaches reuses one [file.StoredCache] per destination parent and
// records keys to [file.StoredCache.Remove] after a successful commit.
type storedCaches struct {
	// mu guards byParent and used.
	mu sync.Mutex
	// byParent maps resolved destination parent to its cache.
	byParent map[string]*file.StoredCache
	// used is the verified keys of this call, in first-success order.
	used []cacheUse
}

// forParent returns the stored cache for parent, creating it on first use.
func (s *storedCaches) forParent(parent string) (*file.StoredCache, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.byParent[parent]; ok {
		return c, nil
	}
	c, err := file.NewStoredCache(parent)
	if err != nil {
		return nil, err
	}
	if s.byParent == nil {
		s.byParent = make(map[string]*file.StoredCache)
	}
	s.byParent[parent] = c

	return c, nil
}

// note records a verified cache key for post-commit removal.
func (s *storedCaches) note(c *file.StoredCache, key digest.Digest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used = append(s.used, cacheUse{cache: c, key: key})
}

// remove deletes every noted cache entry. Duplicate (cache, key) pairs are
// removed once. A missing entry is not an error. Failures are returned so
// the caller can ignore them; they never fail a committed fetch.
func (s *storedCaches) remove(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[cacheUse]struct{}, len(s.used))
	var first error
	for _, u := range s.used {
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		if err := u.cache.Remove(ctx, u.key); err != nil && first == nil {
			first = err
		}
	}

	return first
}

// fetchEntry retrieves and verifies one file entry into plan staging.
func fetchEntry(
	ctx context.Context,
	req FetchFilesRequest,
	plan *file.Plan,
	progress *reporter,
	caches *storedCaches,
	entry Entry,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	raw, contentType, err := req.Manifests.Get(ctx, entry.Digest.String(), entry.MediaType)
	if err != nil {
		return roleError(entry.Role, err)
	}
	if digest.FromBytes(raw) != entry.Digest || int64(len(raw)) != entry.Size {
		return roleError(entry.Role, fmt.Errorf("manifest: %w", ErrDigestMismatch))
	}

	if bigOCIEntry(entry) {
		return fetchBigOCI(ctx, req, plan, progress, caches, entry, raw, contentType)
	}

	return fetchStandard(ctx, req, plan, progress, entry, raw, contentType)
}

// bigOCIEntry reports whether entry is BigOCI form. Capabilities and
// Resolve classify by ArtifactType; media types are compared with
// [index.EqualMediaType].
func bigOCIEntry(entry Entry) bool {
	return index.EqualMediaType(entry.ArtifactType, index.ArtifactTypeBigOCI)
}

// fetchStandard validates the standard file manifest, prechecks compression
// "none", and streams the layer into staging.
func fetchStandard(
	ctx context.Context,
	req FetchFilesRequest,
	plan *file.Plan,
	progress *reporter,
	entry Entry,
	raw []byte,
	contentType string,
) error {
	std, err := verifyManifestDocument(entry, raw, contentType)
	if err != nil {
		return roleError(entry.Role, err)
	}

	if entry.Compression == compressionNone {
		if std.Layer.Digest != entry.ContentDigest || std.Layer.Size != entry.ContentSize {
			return roleError(entry.Role, fmt.Errorf("none precheck: %w", ErrDigestMismatch))
		}
	}

	if err = copyLayer(ctx, req.Blobs, plan, progress, entry, std.Layer, decoderMaxWindow(req.DecoderMaxWindow)); err != nil {
		return err
	}
	progress.entryVerified(entry.ContentSize)
	return nil
}

// fetchBigOCI profile-reads the retrieved manifest, populates the stored cache,
// and decodes the stored file into staging. For compression "none" the profile
// FileDigest and FileSize must equal the entry ContentDigest and ContentSize;
// a mismatch wraps [ErrDigestMismatch].
func fetchBigOCI(
	ctx context.Context,
	req FetchFilesRequest,
	plan *file.Plan,
	progress *reporter,
	caches *storedCaches,
	entry Entry,
	raw []byte,
	contentType string,
) error {
	profile, err := verifyBigOCIDocument(entry, raw, contentType)
	if err != nil {
		return roleError(entry.Role, err)
	}
	if entry.Compression == compressionNone {
		if profile.FileDigest != entry.ContentDigest || profile.FileSize != entry.ContentSize {
			return roleError(entry.Role, fmt.Errorf("none precheck: %w", ErrDigestMismatch))
		}
	}
	if err = copyStored(ctx, req, plan, progress, caches, entry, profile); err != nil {
		return err
	}

	progress.entryVerified(entry.ContentSize)
	return nil
}

// verifyManifestDocument applies spec §8 identity and §3.1 validation to a
// retrieved file manifest. Failures wrap [ErrInvalidDocument].
func verifyManifestDocument(entry Entry, raw []byte, contentType string) (*filemanifest.Standard, error) {
	if !index.EqualMediaType(contentType, entry.MediaType) {
		return nil, fmt.Errorf(
			"manifest content type %q does not identify %q: %w",
			contentType,
			entry.MediaType,
			ErrInvalidDocument,
		)
	}
	std, err := filemanifest.ValidateStandard(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	if !index.EqualMediaType(std.MediaType, entry.MediaType) {
		return nil, fmt.Errorf(
			"manifest mediaType %q does not identify %q: %w",
			std.MediaType,
			entry.MediaType,
			ErrInvalidDocument,
		)
	}
	if !index.EqualMediaType(std.ArtifactType, entry.ArtifactType) {
		return nil, fmt.Errorf(
			"manifest artifactType %q does not identify %q: %w",
			std.ArtifactType,
			entry.ArtifactType,
			ErrInvalidDocument,
		)
	}

	return std, nil
}

// verifyBigOCIDocument applies spec §8 identity and the imgoci BigOCI
// profile to a retrieved file manifest. Profile violations wrap
// [ErrInvalidDocument] (retrieved-document rule).
func verifyBigOCIDocument(entry Entry, raw []byte, contentType string) (*filemanifest.BigOCIProfile, error) {
	if !index.EqualMediaType(contentType, entry.MediaType) {
		return nil, fmt.Errorf(
			"manifest content type %q does not identify %q: %w",
			contentType,
			entry.MediaType,
			ErrInvalidDocument,
		)
	}
	profile, err := filemanifest.ValidateBigOCI(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDocument, err)
	}
	if !index.EqualMediaType(profile.MediaType, entry.MediaType) {
		return nil, fmt.Errorf(
			"manifest mediaType %q does not identify %q: %w",
			profile.MediaType,
			entry.MediaType,
			ErrInvalidDocument,
		)
	}
	if !index.EqualMediaType(profile.ArtifactType, entry.ArtifactType) {
		return nil, fmt.Errorf(
			"manifest artifactType %q does not identify %q: %w",
			profile.ArtifactType,
			entry.ArtifactType,
			ErrInvalidDocument,
		)
	}

	return profile, nil
}

// copyLayer pulls, bounds, decodes, and hashes one file layer into staging.
func copyLayer(
	ctx context.Context,
	blobs Blobs,
	plan *file.Plan,
	progress *reporter,
	entry Entry,
	layer filemanifest.Layer,
	maxWindow uint64,
) error {
	staged, err := plan.Stage(entry.Role)
	if err != nil {
		return roleError(entry.Role, err)
	}
	defer func() { _ = staged.Close() }()

	rc, err := blobs.Pull(ctx, layer.Digest)
	if err != nil {
		return roleError(entry.Role, err)
	}
	defer func() { _ = rc.Close() }()

	br := decomp.NewBoundedReader(&countingReader{r: rc, add: progress.addWire}, layer.Size)
	dec, err := decomp.Decoder(entry.Compression, maxWindow)(br)
	if err != nil {
		return roleError(entry.Role, err)
	}
	defer func() { _ = dec.Close() }()

	hw := decomp.NewCountingHashWriter(staged, entry.ContentSize)
	if _, err := io.Copy(hw, dec); err != nil {
		return roleError(entry.Role, err)
	}
	if hw.Digest() != entry.ContentDigest || hw.Size() != entry.ContentSize {
		return roleError(entry.Role, fmt.Errorf("content: %w", ErrDigestMismatch))
	}
	return nil
}

// copyStored pulls the BigOCI stored file through the cache and decodes it
// into staging. A nil Multipart port is a wiring bug.
func copyStored(
	ctx context.Context,
	req FetchFilesRequest,
	plan *file.Plan,
	progress *reporter,
	caches *storedCaches,
	entry Entry,
	profile *filemanifest.BigOCIProfile,
) error {
	if req.Multipart == nil {
		return roleError(entry.Role, errors.New("bigoci retrieval not configured"))
	}
	parent, err := plan.Parent(entry.Role)
	if err != nil {
		return roleError(entry.Role, err)
	}
	cache, err := caches.forParent(parent)
	if err != nil {
		return roleError(entry.Role, err)
	}

	err = cache.With(ctx, profile.FileDigest,
		func(dst string) error {
			return req.Multipart.PullTo(ctx, req.Repository, entry.Digest, dst, progress.multipartReport())
		},
		func(path string) error {
			return decodeStored(ctx, plan, entry, profile, path, decoderMaxWindow(req.DecoderMaxWindow))
		},
	)

	if err != nil {
		return roleError(entry.Role, mapStoredCacheErr(err))
	}
	caches.note(cache, profile.FileDigest)
	return nil
}

// decodeStored is the cache use callback: one read of the stored file that
// hashes raw bytes (require digest and size) while feeding the strict
// decoder into staging with a content digest/size ceiling. The copy aborts
// when ctx is done.
func decodeStored(
	ctx context.Context,
	plan *file.Plan,
	entry Entry,
	profile *filemanifest.BigOCIProfile,
	path string,
	maxWindow uint64,
) error {
	staged, err := plan.Stage(entry.Role)
	if err != nil {
		return err
	}
	defer func() { _ = staged.Close() }()

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	digester := digest.SHA256.Digester()
	counted := &storedCounter{r: &ctxReader{ctx: ctx, r: f}, h: digester.Hash()}
	dec, err := decomp.Decoder(entry.Compression, maxWindow)(counted)
	if err != nil {
		return err
	}
	defer func() { _ = dec.Close() }()

	hw := decomp.NewCountingHashWriter(staged, entry.ContentSize)
	if _, err := io.Copy(hw, dec); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return err
	}
	if digester.Digest() != profile.FileDigest || counted.n != profile.FileSize {
		return fmt.Errorf("stored file: %w", ErrDigestMismatch)
	}
	if hw.Digest() != entry.ContentDigest || hw.Size() != entry.ContentSize {
		return fmt.Errorf("content: %w", ErrDigestMismatch)
	}
	return nil
}

// mapStoredCacheErr maps [file.StoredCache.With] outcomes onto transfer
// sentinels. Post-fetch digest re-verification ([file.ErrCacheVerify])
// becomes [ErrDigestMismatch]. Already-classified errors, including PullTo
// and decode failures, pass through. A nil Multipart is handled before With.
func mapStoredCacheErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrDigestMismatch),
		errors.Is(err, ErrInvalidDocument),
		errors.Is(err, ErrNotFound),
		errors.Is(err, ErrUnauthorized),
		errors.Is(err, decomp.ErrDecode),
		errors.Is(err, decomp.ErrSizeExceeded),
		errors.Is(err, decomp.ErrUnsupported),
		errors.Is(err, file.ErrInvalidPlan),
		contextDerived(err):
		return err
	case errors.Is(err, file.ErrCacheVerify):
		return fmt.Errorf("stored file: %w: %w", ErrDigestMismatch, err)
	default:
		return err
	}
}

// roleError wraps err with the entry role so callers can see which file failed.
func roleError(role string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("role %s: %w", role, err)
}

// countingReader counts bytes read from r.
type countingReader struct {
	// r is the underlying reader.
	r io.Reader
	// add records a positive byte count.
	add func(int64)
}

// Read counts bytes from the underlying reader toward WireBytes.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.add(int64(n))
	}
	return n, err
}

// storedCounter hashes and counts raw stored-file bytes while they are
// decoded.
type storedCounter struct {
	// r is the stored file.
	r io.Reader
	// h is the SHA-256 of bytes read.
	h hash.Hash
	// n is the count of bytes read.
	n int64
}

// Read hashes and counts bytes from the stored file.
func (s *storedCounter) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		_, _ = s.h.Write(p[:n])
		s.n += int64(n)
	}
	return n, err
}

// ctxReader aborts Read when ctx is done.
type ctxReader struct {
	// ctx is checked before each Read.
	ctx context.Context
	// r is the underlying reader.
	r io.Reader
}

// Read returns ctx.Err when the context is done, otherwise reads from r.
func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
