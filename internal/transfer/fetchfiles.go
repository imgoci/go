package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// Entries are the selected file entries, in commit order.
	Entries []Entry
	// ByRole maps each role to its final destination path.
	ByRole map[string]string
	// Workers is the maximum concurrent entry fetches. Values <= 0 become 4.
	// The effective count never exceeds len(Entries).
	Workers int
	// Progress receives serialized absolute snapshots. It may be nil.
	Progress func(Progress)
}

// FetchFiles retrieves and verifies every entry, then commits destinations.
//
// Spec §5.3: [file.NewPlan] runs first so an invalid ByRole produces zero
// registry Get calls. Each entry GETs the manifest by digest, checks
// digest/size/Content-Type, [filemanifest.ValidateStandard], and
// mediaType/artifactType identity with the entry. Compression "none"
// prechecks that the layer digest and size equal the content digest and
// size. The layer is then Pull'd through [decomp.NewBoundedReader], opened
// with [decomp.Decoder], and copied into [decomp.NewCountingHashWriter]
// wrapping the staged file. Commit runs only when every role verified, in
// input entry order. Any failure before commit cancels outstanding work,
// commits nothing, and calls plan.Cleanup. A [*file.CommitError] is
// returned unwrapped.
func FetchFiles(ctx context.Context, req FetchFilesRequest) error {
	plan, err := file.NewPlan(req.ByRole)
	if err != nil {
		return fmt.Errorf("destination plan: %w", err)
	}
	defer func() { _ = plan.Cleanup() }()

	progress := newReporter(req.Entries, req.Progress)
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
		wg    sync.WaitGroup
		first firstFailure
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
			record(fetchEntry(ctx, req, plan, progress, entry))
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
	// finish runs on the success path regardless of the deferred Cleanup.
	progress.finish()
	return nil
}

// firstFailure holds the first worker error. A later non-context error
// replaces a [context.Canceled] or [context.DeadlineExceeded] placeholder so a
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

// fetchEntry retrieves and verifies one file entry into plan staging.
func fetchEntry(
	ctx context.Context,
	req FetchFilesRequest,
	plan *file.Plan,
	progress *reporter,
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
	std, err := verifyManifestDocument(entry, raw, contentType)
	if err != nil {
		return roleError(entry.Role, err)
	}

	if entry.Compression == compressionNone {
		if std.Layer.Digest != entry.ContentDigest || std.Layer.Size != entry.ContentSize {
			return roleError(entry.Role, fmt.Errorf("none precheck: %w", ErrDigestMismatch))
		}
	}

	if err = copyLayer(ctx, req.Blobs, plan, progress, entry, std.Layer); err != nil {
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

// copyLayer pulls, bounds, decodes, and hashes one file layer into staging.
func copyLayer(
	ctx context.Context,
	blobs Blobs,
	plan *file.Plan,
	progress *reporter,
	entry Entry,
	layer filemanifest.Layer,
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
	dec, err := decomp.Decoder(entry.Compression)(br)
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
