package imgoci

import "github.com/imgoci/go/internal/transfer"

// Progress is an absolute snapshot of one [Client.FetchFiles] or
// [Client.Publish] call.
//
// The fields match [transfer.Progress] one-for-one. Snapshots are serialized:
// a mutex in the orchestrator orders every emit. TotalFiles and TotalBytes
// are fixed up front (TotalBytes is the sum of ContentSize; on Publish they
// are filled after pass 1). CompletedFiles, CompletedBytes, WireBytes,
// Retries, and Fallbacks only increase. WireBytes is raw standard-path blob
// bytes plus each BigOCI transfer's latest WireBytes. Retries is standard-
// path attempts after the first plus each BigOCI transfer's latest Retries.
// Repeated snapshots from one transfer replace that transfer's contribution;
// they are never summed. Fallbacks counts unique blobs that requested
// multipart publication and used the standard path because the part plan
// was fewer than two parts.
//
// Direction is "fetch" on the consumer path and "publish" on the producer
// path. Fetch phases are "staging" then "commit". Publish phases are
// "hashing", then "upload", then "index" for the terminal snapshot after
// the tag PUT.
//
// A [WithProgress] callback must store or print and return. It runs on the
// transfer's goroutines and blocks them for as long as it takes, so a channel
// send, a network call, or a call back into the client belongs in a render
// loop, never here.
type Progress struct {
	// Direction is "fetch" or "publish".
	Direction string
	// Phase is "staging"/"commit" on fetch, or "hashing"/"upload"/"index" on publish.
	Phase string
	// TotalFiles is the number of selected entries.
	TotalFiles int
	// CompletedFiles is how many entries have fully verified.
	CompletedFiles int
	// TotalBytes is the sum of ContentSize across selected entries.
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
	// publication used the standard path instead because the part plan was
	// fewer than two parts. Absolute; only increases. Zero on fetch.
	Fallbacks int
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
			Fallbacks:      p.Fallbacks,
		})
	}
}
