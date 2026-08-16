package retry

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// The policy the design fixes, and the value every unset [Policy] field
// takes. Zero-value [Policy] is this table: four attempts, one second of
// base backoff doubling to a thirty-second ceiling, full jitter.
const (
	// DefaultAttempts is how many times an operation is tried in total,
	// counting the first. Four covers the transient failures registries
	// actually produce without turning a real outage into a minute of
	// waiting.
	DefaultAttempts = 4
	// DefaultBase is the ceiling of the first wait. It doubles every further
	// attempt, so the three waits of a default run are drawn from one, two,
	// and four seconds.
	DefaultBase = time.Second
	// DefaultCap is the longest ceiling the doubling reaches, and the bound
	// on every wait including one a registry asked for. A pause past half a
	// minute belongs to a transfer that should fail and be started again
	// rather than one that is still trying.
	DefaultCap = 30 * time.Second
)

// Sleep waits for d and returns nil, or gives up early and returns ctx's
// error.
//
// A Sleep that ignores ctx makes a transfer outlive its cancellation by up to
// [Policy.Cap], so an implementation must select on both. It is called even
// for a zero d, which is what lets a test count the pauses a run took without
// owning a clock.
type Sleep func(ctx context.Context, d time.Duration) error

// Rand returns a pseudo-random value in [0,n), with the contract of
// [math/rand/v2.Int64N].
//
// It is what makes the backoff full jitter: a wait is drawn uniformly from
// zero up to the attempt's ceiling, so workers that failed together do not
// come back together. It is never called with a non-positive n.
type Rand func(n int64) int64

// Policy is how an operation is retried: how many times, how the waits
// between attempts grow, and the two seams that make both testable.
//
// A field that is not positive takes its default, so the zero Policy is the
// policy the design fixes and a caller with nothing to say about retries says
// nothing. [Default] returns the same policy spelled out.
//
// One Policy is shared by every worker of a transfer, so Sleep and Rand must
// be safe for concurrent use. The defaults are.
type Policy struct {
	// Attempts is the total number of tries, counting the first. One means no
	// retry at all.
	Attempts int
	// Base is the ceiling of the wait after the first failure. Every further
	// attempt doubles it, up to Cap.
	Base time.Duration
	// Cap is the largest ceiling the doubling reaches, and the bound on a
	// wait a far end asked for.
	Cap time.Duration
	// Sleep is how the loop waits between attempts.
	Sleep Sleep
	// Rand is where the jitter in each wait comes from.
	Rand Rand
}

// Default returns the retry policy from the design's defaults table: four
// attempts, one second of base backoff doubling to a thirty second ceiling,
// full jitter, and a sleep that gives up when the context does.
func Default() Policy {
	return Policy{
		Attempts: DefaultAttempts,
		Base:     DefaultBase,
		Cap:      DefaultCap,
		Sleep:    sleep,
		Rand:     rand.Int64N,
	}
}

// Observer is notified once per retry of [Do]: each attempt after the first
// that actually begins. Cancellation during backoff is not a retry.
// A nil Observer is ignored. Implementations must be safe for concurrent use
// when one Observer is shared across workers of a transfer.
type Observer func()

// observerContextKey is the [context.Context] value key for a per-operation
// [Observer]. It is unexported so only [WithObserver] can install one.
type observerContextKey struct{}

// WithObserver installs observe on ctx so [Do] can report retries without a
// package-level hook. The last observer wins. A nil observe leaves ctx
// unchanged so a quiet transfer does not allocate a derived context.
func WithObserver(ctx context.Context, observe Observer) context.Context {
	if observe == nil {
		return ctx
	}

	return context.WithValue(ctx, observerContextKey{}, observe)
}

// observerFrom returns the [Observer] installed on ctx, or nil when none is.
func observerFrom(ctx context.Context) Observer {
	observe, _ := ctx.Value(observerContextKey{}).(Observer)

	return observe
}

// Do runs op until it succeeds, until it fails in a way repeating cannot fix,
// or until the policy runs out of attempts.
//
// op must be safe to run again: it opens whatever it reads from, so a retried
// upload streams from a fresh reader into a fresh session rather than from a
// spent one. Do hands op the context it was given and nothing else, so an
// operation that must not outlive the transfer does not have to be told
// twice, and a cancellation reaches an attempt in flight and a wait between
// attempts alike.
//
// A failure is repeated only when some layer under it called [Transient] or
// the error implements Retryable() bool returning true. Anything else comes
// back on the first attempt: this package does not guess that an unrecognized
// failure might be temporary. Cancellation outranks any tag, and it is read
// off ctx itself and never off the failure's shape: Go's transport renders
// an ordinary dial or header timeout as an error that matches
// [context.DeadlineExceeded], and a transfer that mistook one of those for
// the caller ending it would refuse to retry exactly the failure a retry
// exists for. Only ctx knows whether the transfer is over.
//
// The wait between attempts is the policy's jittered backoff, raised to meet
// a wait the failure carried from the far end. A registry's Retry-After is
// therefore a floor and never a ceiling: it cannot shorten the escalation
// that keeps retrying workers apart, and it cannot park a transfer past
// [Policy.Cap], which bounds every wait this package takes.
//
// The error Do returns is the last one op produced. A failure that ended the
// run on its first attempt comes back exactly as op returned it, so nothing
// reads as retry bookkeeping that never happened; attempts running out wraps
// it with the count, which is the one thing the caller could not otherwise
// know. A context that ends between attempts comes back wrapped together
// with the failure the run was retrying, on one line, and both match under
// [errors.Is]. An [Observer] installed with [WithObserver] is notified once
// per attempt after the first that actually begins, including when later
// attempts fail or the budget runs out. Cancellation during backoff is not
// a retry.
func Do(ctx context.Context, p Policy, op func(ctx context.Context) error) error {
	p = p.normalized()

	var err error

	for attempt := 1; attempt <= p.Attempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return interrupted(ctxErr, attempt-1, err)
		}

		notifyRetry(ctx, attempt)

		err = op(ctx)
		if err == nil {
			return nil
		}

		if done, terminal := terminalFailure(ctx, err); done {
			return terminal
		}

		if attempt == p.Attempts {
			break
		}

		if waitErr := waitBeforeRetry(ctx, p, attempt, err); waitErr != nil {
			return waitErr
		}
	}

	return exhausted(p.Attempts, err)
}

// notifyRetry reports a retry to the [Observer] on ctx, if any. The first
// attempt is not a retry, so it is silent.
func notifyRetry(ctx context.Context, attempt int) {
	if attempt <= 1 {
		return
	}

	if observe := observerFrom(ctx); observe != nil {
		observe()
	}
}

// terminalFailure reports whether err ends the run without another attempt.
// Cancellation is read off ctx, not the error: a transport timeout matches
// [context.DeadlineExceeded] by design in net and net/http, and only the
// context can say whether the transfer itself is over.
func terminalFailure(ctx context.Context, err error) (bool, error) {
	if ctx.Err() != nil {
		return true, err
	}

	if _, transient := IsTransient(err); !transient {
		return true, err
	}

	return false, nil
}

// waitBeforeRetry sleeps the jittered backoff, raised to meet a wait the
// failure carried from the far end. The far end's wait is a floor under the
// jittered backoff, bounded by Cap like every other wait: a hostile header
// cannot park a transfer for a day, and a modest one cannot send every
// rate-limited worker back at the same instant by replacing the escalation.
// Cancellation during the wait is not a retry.
func waitBeforeRetry(ctx context.Context, p Policy, attempt int, err error) error {
	after, _ := IsTransient(err)
	wait := p.backoff(attempt)
	if after > 0 {
		wait = max(wait, min(after, p.Cap))
	}

	if waitErr := p.Sleep(ctx, wait); waitErr != nil {
		return interrupted(waitErr, attempt, err)
	}

	return nil
}

// exhausted wraps the last failure when attempts ran out. A one-attempt
// policy never retried, so the failure comes back exactly as op returned it.
func exhausted(attempts int, err error) error {
	if attempts > 1 {
		return fmt.Errorf("after %d attempts: %w", attempts, err)
	}

	return err
}

// normalized returns p with every unset field filled from [Default]. [Do]
// calls it once, so the loop reads fields without checking them.
func (p Policy) normalized() Policy {
	filled := Default()

	if p.Attempts > 0 {
		filled.Attempts = p.Attempts
	}

	if p.Base > 0 {
		filled.Base = p.Base
	}

	if p.Cap > 0 {
		filled.Cap = p.Cap
	}

	if p.Sleep != nil {
		filled.Sleep = p.Sleep
	}

	if p.Rand != nil {
		filled.Rand = p.Rand
	}

	return filled
}

// backoff returns the wait after the given attempt, counting from one: a
// value drawn uniformly from zero up to that attempt's ceiling. Drawing from
// zero rather than from half the ceiling is what "full jitter" means, and it
// is the variant that spreads a thundering herd widest.
//
// The ceiling starts at Base, or at Cap when Base is already past it, and
// doubles once per attempt already made. The doubling runs as a guarded loop
// rather than a shift so that no attempt count, however large, can overflow
// it into a negative wait: a ceiling more than halfway to Cap goes straight
// to Cap, which is where the doubling was heading anyway.
//
// A ceiling of zero or less — a Policy built by hand with no room to wait in
// — draws nothing and waits nothing.
func (p Policy) backoff(attempt int) time.Duration {
	ceiling := min(p.Base, p.Cap)

	for range attempt - 1 {
		if ceiling >= p.Cap {
			break
		}

		if ceiling > p.Cap/2 {
			ceiling = p.Cap

			break
		}

		ceiling *= 2
	}

	if ceiling <= 0 {
		return 0
	}

	return time.Duration(p.Rand(int64(ceiling)))
}

// sleep is the default [Policy.Sleep]: it waits for d, or returns as soon as
// ctx is done, whichever comes first. A non-positive d returns at once, after
// one look at ctx.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// interrupted renders a run the context ended: the reason it stopped wrapped
// together with the failure it was retrying, on one line, and both reachable
// under [errors.Is]. A run that had not failed yet — ended before its first
// attempt — reports the cause alone.
func interrupted(cause error, attempts int, last error) error {
	if last == nil {
		return cause
	}

	return fmt.Errorf("%w after %d attempts: %w", cause, attempts, last)
}
