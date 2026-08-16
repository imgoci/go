// Package retry is the retry loop for this repository's own adapters.
//
// Exactly two retry domains exist, and they never nest. This package retries
// the registry adapter and go-oci-blob operations (where RetryPolicy{} means
// one attempt). bigoci owns its own internal budget for multipart calls; those
// calls are never wrapped here, because wrapping would multiply whole-transfer
// attempts.
//
// [Transient] marks a failure that repeating the request could fix, and
// [IsTransient] reads that mark back off an error however deeply it has since
// been wrapped. Adapters may also attach a Retry-After hint on the mark.
// [IsTransient] honors go-oci-blob-style metadata via an interface assertion:
// an error implementing Retryable() bool is treated as transient when the
// method returns true, with an optional RetryAfter() [time.Duration] hint.
//
// [Do] is the loop that acts on the mark, together with the [Policy] that says
// how many attempts it makes and how the waits between them grow. A transfer
// installs a per-operation [Observer] on the context with [WithObserver]; [Do]
// notifies it once per attempt after the first that actually begins. There is
// no package-level hook.
//
// A mark is produced by the layer that diagnosed the failure. In practice that
// is an adapter: only the code that spoke to the far end can tell a dropped
// connection from a refused request. Marks are consumed in exactly one place,
// [Do]. An unmarked error is terminal: retrying is opted into per failure by
// the layer that classified it.
//
// Nothing here performs I/O. Waiting and randomness reach the loop as the
// [Policy.Sleep] and [Policy.Rand] fields. Context cancellation aborts waits.
package retry
