// Package retry is THE loop for this repository's own adapters.
//
// Exactly two retry domains exist, and they never nest. This package retries
// the registry adapter and go-oci-blob operations (where RetryPolicy{} means
// one attempt). bigoci owns its own internal budget for multipart calls; those
// calls are never wrapped here, because wrapping would multiply whole-transfer
// attempts (ARCHITECTURE.md §6.5). If upstream later exposes retry control,
// collapsing to one domain is a small follow-up.
//
// The package holds two halves of one subject. The first is a vocabulary:
// [Transient] marks a failure that repeating the request could fix, and
// [IsTransient] reads that mark back off an error however deeply it has since
// been wrapped. Adapters may also attach a Retry-After hint on the mark.
// [IsTransient] additionally honors go-oci-blob-style metadata via an
// interface assertion: an error implementing Retryable() bool is treated as
// transient when the method returns true, with an optional RetryAfter()
// [time.Duration] hint. The second half is the loop that acts on the mark,
// [Do], together with the [Policy] that says how many attempts it makes and
// how the waits between them grow. A transfer that wants a retry count
// installs a per-operation [Observer] on the context with [WithObserver];
// [Do] notifies it once per attempt after the first that actually begins.
// There is no package-level hook.
//
// A mark is produced by whichever layer diagnosed the failure. In practice
// that is an adapter: only the code that spoke to the far end can tell a
// dropped connection from a refused request. Marks are consumed in exactly
// one place, [Do], which is the only thing in this repository that repeats an
// operation against our own adapters.
//
// An error nobody marked is terminal. That default is load-bearing: a failure
// no layer recognized is one this package does not understand, and sending it
// three more times turns an immediate answer into a slow one without making
// it any more correct. Retrying is opted into per failure, by the layer that
// knows enough to opt in.
//
// Nothing here performs I/O. Waiting and randomness reach the loop as the
// [Policy.Sleep] and [Policy.Rand] fields, so a test reads an entire backoff
// schedule out of a slice with no clock anywhere in the frame. Context
// cancellation aborts waits.
package retry
