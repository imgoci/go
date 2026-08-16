package transfer

import "sync"

const (
	// DirectionFetch is Progress.Direction for consumer retrieval.
	DirectionFetch = "fetch"
	// PhaseStaging is Progress.Phase while entries are downloaded and verified.
	PhaseStaging = "staging"
	// PhaseCommit is Progress.Phase after every role has verified, including
	// the terminal snapshot emitted after plan.Commit.
	PhaseCommit = "commit"
)

// Progress is an absolute snapshot of one FetchFiles call.
//
// Snapshots are serialized: a mutex orders every emit. TotalFiles and
// TotalBytes are fixed up front (TotalBytes is the sum of ContentSize).
// CompletedFiles and CompletedBytes only increase. WireBytes counts raw
// blob bytes read off the wire. Retries is 0 until slice 6 unifies retry
// accounting.
type Progress struct {
	// Direction is always [DirectionFetch] on the consumer path.
	Direction string
	// Phase is [PhaseStaging] until commit, then [PhaseCommit].
	Phase string
	// TotalFiles is the number of entries in the request.
	TotalFiles int
	// CompletedFiles is how many entries have fully verified.
	CompletedFiles int
	// TotalBytes is the sum of ContentSize across entries.
	TotalBytes int64
	// CompletedBytes is the sum of ContentSize of verified entries.
	CompletedBytes int64
	// WireBytes is the count of raw blob bytes read from [Blobs.Pull].
	WireBytes int64
	// Retries is 0 in this slice.
	Retries int
}

// reporter serializes absolute progress snapshots.
type reporter struct {
	// mu guards current and terminal.
	mu sync.Mutex
	// emit is the caller callback; nil means no snapshots.
	emit func(Progress)
	// current is the last committed snapshot state.
	current Progress
	// terminal reports whether the commit-phase snapshot has been emitted.
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
