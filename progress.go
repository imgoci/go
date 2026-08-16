package imgoci

import "github.com/imgoci/go/internal/transfer"

// Progress is an absolute snapshot of one [Client.FetchFiles] call.
//
// The fields match [transfer.Progress] one-for-one. Snapshots are serialized:
// a mutex in the orchestrator orders every emit. TotalFiles and TotalBytes
// are fixed up front (TotalBytes is the sum of ContentSize). CompletedFiles
// and CompletedBytes only increase. WireBytes counts raw blob bytes read off
// the wire. Retries is unified across retry domains in a later slice; it is
// 0 until then.
//
// Direction is "fetch" on the consumer path. Phase is "staging" while entries
// are downloaded and verified, then "commit" for the terminal snapshot after
// every role has been renamed onto its final path.
//
// A [WithProgress] callback must store or print and return. It runs on the
// transfer's goroutines and blocks them for as long as it takes, so a channel
// send, a network call, or a call back into the client belongs in a render
// loop, never here.
type Progress struct {
	// Direction is "fetch" on the consumer path.
	Direction string
	// Phase is "staging" until commit, then "commit".
	Phase string
	// TotalFiles is the number of selected entries.
	TotalFiles int
	// CompletedFiles is how many entries have fully verified.
	CompletedFiles int
	// TotalBytes is the sum of ContentSize across selected entries.
	TotalBytes int64
	// CompletedBytes is the sum of ContentSize of verified entries.
	CompletedBytes int64
	// WireBytes is the count of raw blob bytes read from the registry.
	WireBytes int64
	// Retries is 0 in this slice.
	Retries int
}

// convertProgress adapts a public callback to the orchestrator's snapshot
// type. A nil callback stays nil so an unwatched transfer is free of the
// accounting rather than merely quiet about it.
func convertProgress(fn func(Progress)) func(transfer.Progress) {
	if fn == nil {
		return nil
	}

	return func(p transfer.Progress) {
		fn(Progress{
			Direction:      p.Direction,
			Phase:          p.Phase,
			TotalFiles:     p.TotalFiles,
			CompletedFiles: p.CompletedFiles,
			TotalBytes:     p.TotalBytes,
			CompletedBytes: p.CompletedBytes,
			WireBytes:      p.WireBytes,
			Retries:        p.Retries,
		})
	}
}
