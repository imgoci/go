package retry

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"
)

// retryable is the go-oci-blob-style metadata this package honors without
// importing go-oci-blob. An error implementing Retryable() bool is transient
// when that method returns true. [IsTransient] finds it through [errors.As].
type retryable interface {
	Retryable() bool
}

// retryAfterer is optional metadata pairing with [retryable]: the wait the
// far end asked for through Retry-After, or zero when it asked for none.
type retryAfterer interface {
	RetryAfter() time.Duration
}

// Transient marks err as a failure that repeating the request could fix: a
// connection that dropped, a registry that answered 429 or 5xx, a blob
// operation go-oci-blob classified as retryable.
//
// after is how long the far end asked the caller to wait before the next
// attempt, and zero when it asked for nothing. A wait that arrives this way is
// a floor under the policy's own backoff rather than a replacement for it.
// Bounding such a wait is [Do]'s job, not the caller's: whoever tags a failure
// reports what the far end actually said. Parse the header with
// [ParseRetryAfter] and pass the duration here.
//
// The tag is a wrapper. [errors.Is] and [errors.As] see through it, and the
// message it renders is err's own. Only the layer that diagnosed a failure
// should tag it.
//
// A nil err returns nil, so a caller may tag a result unconditionally.
func Transient(err error, after time.Duration) error {
	if err == nil {
		return nil
	}

	return &fault{err: err, after: after}
}

// IsTransient reports whether some layer under err marked it worth repeating,
// and returns the wait the far end asked for — zero when it asked for none, and
// zero for an untagged error. An untagged error is terminal.
//
// Classification is consumed from two sources, in this order:
//
//  1. A [Transient] tag. Nested tags do not conflict: the walk [errors.As]
//     performs finds the outermost one, which is the verdict of the layer
//     closest to the caller.
//  2. go-oci-blob-style metadata via interface assertion. An error
//     implementing Retryable() bool is transient when that method returns
//     true. A RetryAfter() [time.Duration] method, when present, is the hint.
func IsTransient(err error) (time.Duration, bool) {
	var tagged *fault
	if errors.As(err, &tagged) {
		return tagged.after, true
	}

	var marker retryable
	if !errors.As(err, &marker) || !marker.Retryable() {
		return 0, false
	}

	var hint retryAfterer
	if errors.As(err, &hint) {
		return hint.RetryAfter(), true
	}

	return 0, true
}

// ParseRetryAfter reads the wait a response asked for out of a Retry-After
// header value.
//
// RFC 9110 gives the header two spellings and registries use both: a count of
// seconds, and an HTTP-date the wait ends at. A date is turned into a wait
// against now. Clock skew costs an attempt's timing; the value is advice about
// a wait, not a deadline. All three date formats [http.ParseTime] accepts are
// accepted here.
//
// Anything unusable is zero: an empty value, a value that is neither form, a
// count of zero or less, a date already past, and a count too large to be a
// duration at all. Zero tells [Do] the far end asked for nothing and its own
// backoff applies. This function does not clamp: the adapter reports what the
// registry said, and bounding it is the policy's job.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}

	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
			return 0
		}

		return time.Duration(seconds) * time.Second
	}

	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}

	wait := when.Sub(now)
	if wait <= 0 {
		return 0
	}

	return wait
}

// fault is the tag [Transient] attaches. It is unexported so other packages
// cannot construct one without the constructor's contract.
type fault struct {
	// err is the failure being classified.
	err error
	// after is the wait the far end asked for, zero when it asked for none.
	after time.Duration
}

// Error renders the underlying failure unchanged.
func (f *fault) Error() string {
	return f.err.Error()
}

// Unwrap exposes the underlying failure to [errors.Is] and [errors.As].
func (f *fault) Unwrap() error {
	return f.err
}

// Retryable reports that a [Transient] tag is always worth another attempt.
func (f *fault) Retryable() bool {
	return true
}

// RetryAfter returns the wait the far end asked for, zero when it asked for
// none.
func (f *fault) RetryAfter() time.Duration {
	return f.after
}
