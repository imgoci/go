package transfer

import "sync"

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
// On Publish, TotalBytes is filled after pass 1. CompletedFiles and
// CompletedBytes only increase. WireBytes counts raw standard-path blob
// bytes read (fetch) or actually pushed (publish; Exists-skip is 0).
// BigOCI stored-file wire bytes and retries are unreported until slice 6
// unifies them (PLAN PR6.2). Retries is 0 until then. Fallbacks counts
// unique blobs that requested multipart publication and used the standard
// path because the part plan was fewer than two parts.
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
	// WireBytes is the count of raw standard-path blob bytes transferred.
	// BigOCI transfers are excluded until slice 6 (PLAN PR6.2).
	WireBytes int64
	// Retries is 0 in this slice.
	Retries int
	// Fallbacks is how many unique stored blobs planned for BigOCI
	// publication used the standard path instead because ceil(storedSize /
	// partSize) was fewer than two parts (spec §8 / ARCHITECTURE.md §5.1).
	// Absolute; only increases. Zero on fetch.
	Fallbacks int
}

// reporter serializes absolute progress snapshots.
type reporter struct {
	// mu guards current and terminal.
	mu sync.Mutex
	// emit is the caller callback; nil means no snapshots.
	emit func(Progress)
	// current is the last committed snapshot state.
	current Progress
	// terminal reports whether the terminal snapshot has been emitted.
	terminal bool
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

// addWire records n raw blob bytes read off the wire.
func (r *reporter) addWire(n int64) {
	if n <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.WireBytes += n
}

// entryVerified records one fully verified entry and emits a snapshot.
func (r *reporter) entryVerified(contentSize int64) {
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
	if n <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Fallbacks += n
}
