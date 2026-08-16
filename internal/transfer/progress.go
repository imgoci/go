package transfer

import (
	"context"
	"sync"

	"github.com/imgoci/go/internal/retry"
)

const (
	// DirectionFetch is Progress.Direction for consumer retrieval.
	DirectionFetch = "fetch"
	// DirectionPublish is Progress.Direction for producer publication.
	DirectionPublish = "publish"
	// PhaseStaging is Progress.Phase while entries are downloaded and verified.
	PhaseStaging = "staging"
	// PhaseCommit is Progress.Phase after every role has verified, including
	// the terminal snapshot emitted after plan.Commit.
	PhaseCommit = "commit"
	// PhaseHashing is Progress.Phase while Publish hashes unique sources.
	PhaseHashing = "hashing"
	// PhaseUpload is Progress.Phase while unique stored blobs and file
	// manifests are pushed.
	PhaseUpload = "upload"
	// PhaseIndex is Progress.Phase of the terminal snapshot after the index
	// tag PUT.
	PhaseIndex = "index"
)

// Progress is an absolute snapshot of one FetchFiles or Publish call.
//
// Snapshots are serialized: a mutex orders every emit. TotalFiles and
// TotalBytes are fixed up front (TotalBytes is the sum of ContentSize).
// On Publish, TotalBytes is filled after pass 1. CompletedFiles,
// CompletedBytes, WireBytes, Retries, and Fallbacks only increase.
// WireBytes is standard-path blob bytes plus the latest-absolute BigOCI
// WireBytes of each distinct multipart transfer. Retries is standard-path
// [retry.Do] attempts after the first plus the latest-absolute BigOCI
// Retries of each distinct multipart transfer. Repeated snapshots from one
// transfer replace that transfer's contribution; they are never summed.
// Fallbacks counts unique blobs that requested multipart publication and
// used the standard path because the part plan was fewer than two parts.
type Progress struct {
	// Direction is [DirectionFetch] or [DirectionPublish].
	Direction string
	// Phase is [PhaseStaging] then [PhaseCommit] on fetch, or [PhaseHashing],
	// [PhaseUpload], then [PhaseIndex] on publish.
	Phase string
	// TotalFiles is the number of entries in the request.
	TotalFiles int
	// CompletedFiles is how many entries have fully verified.
	CompletedFiles int
	// TotalBytes is the sum of ContentSize across entries.
	TotalBytes int64
	// CompletedBytes is the sum of ContentSize of verified entries.
	CompletedBytes int64
	// WireBytes is raw standard-path blob bytes plus each BigOCI transfer's
	// latest WireBytes.
	WireBytes int64
	// Retries is standard-path attempts after the first plus each BigOCI
	// transfer's latest Retries.
	Retries int
	// Fallbacks is how many unique stored blobs planned for BigOCI
	// publication used the standard path instead because ceil(storedSize /
	// partSize) was fewer than two parts (spec §8 / ARCHITECTURE.md §5.1).
	// Absolute; only increases. Zero on fetch.
	Fallbacks int
}

// reporter serializes absolute progress snapshots.
type reporter struct {
	// mu guards current, terminal, and the accounting fields below.
	mu sync.Mutex
	// emit is the caller callback; nil means no snapshots and no optional
	// accounting.
	emit func(Progress)
	// current is the last committed snapshot state.
	current Progress
	// terminal reports whether the terminal snapshot has been emitted.
	terminal bool
	// standardWire is raw standard-path blob bytes transferred.
	standardWire int64
	// standardRetries is [retry.Do] attempts after the first.
	standardRetries int
	// nextMultipartID is the next distinct BigOCI transfer identifier.
	nextMultipartID uint64
	// multipart is the latest-absolute wire and retry counts of each
	// distinct transfer.
	multipart map[uint64]multipartLatest
}

// multipartLatest is one transfer's latest-absolute WireBytes and Retries.
type multipartLatest struct {
	wireBytes int64
	retries   int
}

// newReporter builds a staging snapshot with totals fixed and emits it.
func newReporter(entries []Entry, emit func(Progress)) *reporter {
	var totalBytes int64
	for _, e := range entries {
		totalBytes += e.ContentSize
	}
	r := &reporter{
		emit: emit,
		current: Progress{
			Direction:  DirectionFetch,
			Phase:      PhaseStaging,
			TotalFiles: len(entries),
			TotalBytes: totalBytes,
		},
	}
	r.snapshot()
	return r
}

// newPublishReporter builds a hashing snapshot with TotalFiles fixed and emits it.
// TotalBytes is filled after pass 1 via [reporter.setTotalBytes].
func newPublishReporter(files int, emit func(Progress)) *reporter {
	r := &reporter{
		emit: emit,
		current: Progress{
			Direction:  DirectionPublish,
			Phase:      PhaseHashing,
			TotalFiles: files,
		},
	}
	r.snapshot()
	return r
}

// watching reports whether a caller callback is installed.
func (r *reporter) watching() bool {
	return r.emit != nil
}

// bindContext installs a [retry.Observer] on ctx so standard-path [retry.Do]
// calls count as Retries. A quiet reporter leaves ctx unchanged.
func (r *reporter) bindContext(ctx context.Context) context.Context {
	if !r.watching() {
		return ctx
	}

	return retry.WithObserver(ctx, r.addRetry)
}

// multipartReport returns a callback bound to one distinct BigOCI transfer.
// A quiet reporter returns nil so the adapter skips conversion.
func (r *reporter) multipartReport() func(wireBytes int64, retries int) {
	if !r.watching() {
		return nil
	}

	r.mu.Lock()
	r.nextMultipartID++
	id := r.nextMultipartID
	r.mu.Unlock()

	return func(wireBytes int64, retries int) {
		r.mergeMultipart(id, wireBytes, retries)
	}
}

// snapshot emits a copy of current. The caller must not hold mu unless it
// is snapshotLocked.
func (r *reporter) snapshot() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshotLocked()
}

// snapshotLocked emits a copy of current. r.mu must be held.
func (r *reporter) snapshotLocked() {
	if r.emit == nil || r.terminal {
		return
	}
	r.emit(r.current)
}

// recomputeLocked writes WireBytes and Retries from the standard counters
// plus each transfer's latest snapshot. r.mu must be held.
func (r *reporter) recomputeLocked() {
	wire := r.standardWire
	retries := r.standardRetries
	for _, snap := range r.multipart {
		wire += snap.wireBytes
		retries += snap.retries
	}
	r.current.WireBytes = wire
	r.current.Retries = retries
}

// addWire records n raw standard-path blob bytes.
func (r *reporter) addWire(n int64) {
	if n <= 0 || !r.watching() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.standardWire += n
	r.recomputeLocked()
}

// addRetry records one standard-path attempt after the first and emits
// so callers observe registry backoff immediately. WireBytes stay silent
// per chunk; retries are bounded and must surface while a wait is in
// progress.
func (r *reporter) addRetry() {
	if !r.watching() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.standardRetries++
	r.recomputeLocked()
	r.snapshotLocked()
}

// mergeMultipart records the latest-absolute counts of one BigOCI
// transfer and emits so WireBytes and Retries appear in the same stream as
// standard-path progress.
func (r *reporter) mergeMultipart(id uint64, wireBytes int64, retries int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal {
		return
	}
	if r.multipart == nil {
		r.multipart = make(map[uint64]multipartLatest)
	}
	r.multipart[id] = multipartLatest{wireBytes: wireBytes, retries: retries}
	r.recomputeLocked()
	r.snapshotLocked()
}

// entryVerified records one fully verified entry and emits a snapshot.
func (r *reporter) entryVerified(contentSize int64) {
	if !r.watching() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.CompletedFiles++
	r.current.CompletedBytes += contentSize
	r.snapshotLocked()
}

// finish emits the terminal commit-phase snapshot exactly once.
func (r *reporter) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal {
		return
	}
	r.current.Phase = PhaseCommit
	r.snapshotLocked()
	r.terminal = true
}

// setTotalBytes records the decoded-content total after pass 1. It does not emit.
func (r *reporter) setTotalBytes(n int64) {
	if !r.watching() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.TotalBytes = n
}

// setPhase records phase and emits a snapshot unless the reporter is terminal.
func (r *reporter) setPhase(phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal {
		return
	}
	r.current.Phase = phase
	r.snapshotLocked()
}

// finishIndex emits the terminal index-phase snapshot exactly once.
func (r *reporter) finishIndex() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal {
		return
	}
	r.current.Phase = PhaseIndex
	r.snapshotLocked()
	r.terminal = true
}

// addFallbacks records n unique blobs that fell back from multipart to the
// standard path. It does not emit; the next snapshot carries the count.
func (r *reporter) addFallbacks(n int) {
	if n <= 0 || !r.watching() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Fallbacks += n
}
