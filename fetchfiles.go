package imgoci

import (
	"context"
	"errors"
	"fmt"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/file"
	"github.com/imgoci/go/internal/transfer"
)

// FetchOption configures one call to [Client.FetchFiles].
//
// The interface is sealed by an unexported method: the options are the ones
// this package ships, so a fetch cannot be handed a knob the library does
// not know how to honor.
type FetchOption interface {
	applyFetch(*fetchSettings)
}

// fetchSettings are the knobs one FetchFiles call runs with, once the
// defaults and the caller's options have been applied.
type fetchSettings struct {
	// workers is how many entries download at once. Zero means the
	// orchestrator default.
	workers int
	// workersSet reports whether [WithWorkers] was named, so a non-positive
	// count can be rejected before any I/O.
	workersSet bool
	// progress receives absolute snapshots, nil when nobody is watching.
	progress func(Progress)
}

// WithProgress delivers serialized absolute snapshots of the transfer to fn.
// A nil fn is ignored. The same option applies to [Client.FetchFiles] and
// [Client.Publish].
func WithProgress(
	fn func(Progress),
) progressOption { //nolint:revive // returns an unexported option type on purpose: it satisfies both FetchOption and PublishOption and callers never name it
	return progressOption(fn)
}

// WithWorkers moves n selected files at once. n must be positive, which
// [Client.FetchFiles] and [Client.Publish] check before constructing a
// registry adapter. Omitting the option leaves the orchestrator default
// (four workers).
func WithWorkers(
	n int,
) workersOption { //nolint:revive // returns an unexported option type on purpose: it satisfies both FetchOption and PublishOption and callers never name it
	return workersOption(n)
}

// progressOption carries [WithProgress].
type progressOption func(Progress)

// applyFetch records the progress callback.
func (o progressOption) applyFetch(s *fetchSettings) {
	if o != nil {
		s.progress = o
	}
}

// applyPublish records the progress callback.
func (o progressOption) applyPublish(s *publishSettings) {
	if o != nil {
		s.progress = o
	}
}

// workersOption carries [WithWorkers].
type workersOption int

// applyFetch records how many entries download at once.
func (o workersOption) applyFetch(s *fetchSettings) {
	s.workers = int(o)
	s.workersSet = true
}

// applyPublish records how many unique blobs upload at once.
func (o workersOption) applyPublish(s *publishSettings) {
	s.workers = int(o)
	s.workersSet = true
}

// FetchFiles retrieves and verifies every entry in sel into dest.
//
// Preconditions are checked before any adapter construction or network I/O:
//
//   - sel.IndexDigest() == rel.Digest(), else [ErrSelectionMismatch]
//   - every selected entry's ArtifactType is in [Client.Capabilities], else
//     [ErrUnsupportedType]
//   - dest maps onto the selected roles, else [ErrInvalidDest]
//
// [ToDir] joins the directory with each entry's Filename (already validated
// by the index rules). [ToFiles] requires every selected role to be present
// and rejects extras.
//
// Errors from the orchestrator are mapped onto the public sentinels:
// [file.ErrInvalidPlan] becomes [ErrInvalidDest]; [transfer.ErrInvalidDocument]
// becomes [ErrInvalidIndex]; [transfer.ErrDigestMismatch] and
// [decomp.ErrSizeExceeded] become [ErrDigestMismatch] (a size bound is
// digest discipline); [decomp.ErrDecode] becomes [ErrDecode];
// [transfer.ErrNotFound] and [transfer.ErrUnauthorized] become the matching
// public sentinels. A [*file.CommitError] is wrapped so the message names the
// committed roles while [errors.As] still finds the detail.
func (c *Client) FetchFiles(
	ctx context.Context,
	rel *Release,
	sel *Resolved,
	dest Dest,
	opts ...FetchOption,
) error {
	settings, entries, byRole, err := prepareFetchFiles(c, rel, sel, dest, opts)
	if err != nil {
		return err
	}

	ports, err := c.portsFor(rel.host, rel.repository)
	if err != nil {
		return err
	}

	return mapFetchError(transfer.FetchFiles(ctx, transfer.FetchFilesRequest{
		Manifests: ports.manifests,
		Blobs:     ports.blobs,
		Entries:   transferEntries(entries),
		ByRole:    byRole,
		Workers:   settings.workers,
		Progress:  convertProgress(settings.progress),
	}))
}

// prepareFetchFiles applies options and the pre-network preconditions.
// Adapter construction must not run until this returns successfully.
func prepareFetchFiles(
	c *Client,
	rel *Release,
	sel *Resolved,
	dest Dest,
	opts []FetchOption,
) (fetchSettings, []FileEntry, map[string]string, error) {
	var settings fetchSettings
	if err := applyFetchOptions(&settings, opts); err != nil {
		return fetchSettings{}, nil, nil, err
	}
	if c == nil {
		return fetchSettings{}, nil, nil, errors.New("fetch files: nil client")
	}
	if rel == nil {
		return fetchSettings{}, nil, nil, errors.New("fetch files: nil release")
	}
	if sel == nil {
		return fetchSettings{}, nil, nil, errors.New("fetch files: nil selection")
	}
	if sel.IndexDigest() != rel.Digest() {
		return fetchSettings{}, nil, nil, fmt.Errorf(
			"%w: resolved digest %s is not release %s",
			ErrSelectionMismatch, sel.IndexDigest(), rel.Digest(),
		)
	}

	entries := sel.Entries()
	caps := c.Capabilities()
	for _, entry := range entries {
		if !caps.supports(entry.ArtifactType) {
			return fetchSettings{}, nil, nil, fmt.Errorf(
				"%w: artifact type %q", ErrUnsupportedType, entry.ArtifactType,
			)
		}
	}

	byRole, err := dest.mapByRole(entries)
	if err != nil {
		return fetchSettings{}, nil, nil, err
	}

	return settings, entries, byRole, nil
}

// applyFetchOptions records opts onto settings and rejects a non-positive
// worker count before any I/O.
func applyFetchOptions(settings *fetchSettings, opts []FetchOption) error {
	for _, opt := range opts {
		if opt != nil {
			opt.applyFetch(settings)
		}
	}
	if settings.workersSet && settings.workers <= 0 {
		return fmt.Errorf("worker count must be positive, got %d", settings.workers)
	}

	return nil
}

// transferEntries copies public file entries onto the orchestrator type.
func transferEntries(entries []FileEntry) []transfer.Entry {
	out := make([]transfer.Entry, len(entries))
	for i, entry := range entries {
		out[i] = transfer.Entry{
			Role:          entry.Selector.Role,
			MediaType:     entry.MediaType,
			ArtifactType:  entry.ArtifactType,
			Compression:   entry.Selector.Compression,
			Digest:        entry.Digest,
			Size:          entry.Size,
			ContentDigest: entry.ContentDigest,
			ContentSize:   entry.ContentSize,
			Filename:      entry.Filename,
		}
	}

	return out
}

// mapFetchError maps orchestrator and adapter sentinels onto the public
// error surface. An error that matches nothing public comes back unchanged.
func mapFetchError(err error) error {
	if err == nil {
		return nil
	}

	var commit *file.CommitError
	if errors.As(err, &commit) {
		return fmt.Errorf(
			"commit failed; committed roles %v; failing role %q: %w",
			commit.Committed, commit.Role, err,
		)
	}

	switch {
	case errors.Is(err, file.ErrInvalidPlan):
		return fmt.Errorf("%w: %w", ErrInvalidDest, err)
	case errors.Is(err, transfer.ErrInvalidDocument):
		return fmt.Errorf("%w: %w", ErrInvalidIndex, err)
	case errors.Is(err, decomp.ErrSizeExceeded), errors.Is(err, transfer.ErrDigestMismatch):
		return fmt.Errorf("%w: %w", ErrDigestMismatch, err)
	case errors.Is(err, decomp.ErrDecode):
		return fmt.Errorf("%w: %w", ErrDecode, err)
	case errors.Is(err, transfer.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, transfer.ErrUnauthorized):
		return fmt.Errorf("%w: %w", ErrUnauthorized, err)
	default:
		return err
	}
}
