package retry

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// Default values for every unset [Policy] field. Zero-value [Policy] is this
// table: four attempts, one second of base backoff doubling to a thirty-second
// ceiling, full jitter.
const (
	// DefaultAttempts is how many times an operation is tried in total, counting
	// the first.
	DefaultAttempts = 4
	// DefaultBase is the ceiling of the first wait. It doubles every further
	// attempt, up to [DefaultCap].
	DefaultBase = time.Second
	// DefaultCap is the longest ceiling the doubling reaches, and the bound on
	// every wait including one a registry asked for.
	DefaultCap = 30 * time.Second
)

// Sleep waits for d and returns nil, or gives up early and returns ctx's
// error.
//
// An implementation must select on both d and ctx: ignoring ctx lets a transfer
// outlive cancellation by up to [Policy.Cap]. Sleep is called even for a zero d
// so tests can count pauses without a clock.
type Sleep func(ctx context.Context, d time.Duration) error

// Rand returns a pseudo-random value in [0,n), with the contract of
// [math/rand/v2.Int64N].
//
// Backoff is full jitter: a wait is drawn uniformly from zero up to the
// attempt's ceiling. It is never called with a non-positive n.
type Rand func(n int64) int64

// Policy is how an operation is retried: how many times, how the waits between
// attempts grow, and the Sleep and Rand seams that make both testable.
//
// A field that is not positive takes its default, so the zero Policy is
// [Default]. One Policy is shared by every worker of a transfer, so Sleep and
// Rand must be safe for concurrent use. The defaults are.
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

// Default returns a [Policy] of four attempts, one second of base backoff
// doubling to a thirty-second ceiling, full jitter, and a sleep that gives up
// when the context does.
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
// [Observer].
type observerContextKey struct{}

// WithObserver installs observe on ctx so [Do] can report retries. The last
// observer wins. A nil observe leaves ctx unchanged so a quiet transfer does
// not allocate a derived context.
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
// op must be safe to run again: a retried upload must stream from a fresh
// reader into a fresh session. Do hands op the context it was given;
// cancellation reaches an attempt in flight and a wait between attempts.
//
// A failure is repeated only when some layer under it called [Transient] or the
// error implements Retryable() bool returning true. Anything else comes back on
// the first attempt. Cancellation outranks any tag and is read off ctx itself,
// never off the failure's shape: a transport timeout matches
// [context.DeadlineExceeded] without the transfer having ended.
//
// The wait between attempts is the policy's jittered backoff, raised to meet a
// wait the failure carried from the far end. Retry-After is a floor under the
// jittered backoff, not a replacement, and [Policy.Cap] bounds every wait.
//
// The error Do returns is the last one op produced. A first-attempt terminal
// failure comes back as op returned it. Exhausted attempts wrap it with the
// count. A context that ends between attempts comes back wrapped with the
// failure being retried; both match under [errors.Is]. An [Observer] installed
// with [WithObserver] is notified once per attempt after the first that
// actually begins.
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

// notifyRetry reports a retry to the [Observer] on ctx, if any.
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
// [context.DeadlineExceeded], and only the context can say whether the transfer
// itself is over.
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
// failure carried from the far end. That wait is a floor under the jittered
// backoff, bounded by Cap.
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

// normalized returns p with every unset field filled from [Default].
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

// backoff returns the wait after the given attempt, counting from one: a value
// drawn uniformly from zero up to that attempt's ceiling. Full jitter means the
// draw starts at zero, not at half the ceiling.
//
// The ceiling starts at Base, or at Cap when Base is already past it, and
// doubles once per attempt already made. Doubling is a guarded loop rather than
// a shift so a large attempt count cannot overflow into a negative wait: a
// ceiling more than halfway to Cap goes straight to Cap.
//
// A ceiling of zero or less draws nothing and waits nothing.
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

// interrupted wraps the context's end together with the failure being retried;
// both match under [errors.Is]. A run that had not failed yet reports the cause
// alone.
func interrupted(cause error, attempts int, last error) error {
	if last == nil {
		return cause
	}

	return fmt.Errorf("%w after %d attempts: %w", cause, attempts, last)
}
