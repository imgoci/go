package retry

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

// drawnCeiling is what a recording Rand reports for a draw it was never
// asked to make, so a row can say "the policy drew nothing" without a second
// field.
const drawnCeiling = -1

func TestPolicyBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		policy      Policy
		attempt     int
		wantCeiling int64
	}{
		{
			name:        "the first wait is drawn under the base",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     1,
			wantCeiling: int64(time.Second),
		},
		{
			name:        "the second wait doubles the first",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     2,
			wantCeiling: int64(2 * time.Second),
		},
		{
			name:        "the third wait doubles again",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     3,
			wantCeiling: int64(4 * time.Second),
		},
		{
			name:        "the doubling keeps going while it fits under the cap",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     5,
			wantCeiling: int64(16 * time.Second),
		},
		{
			name:        "a doubling that would pass the cap lands on it",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     6,
			wantCeiling: int64(DefaultCap),
		},
		{
			name:        "an attempt count far past the cap still returns the cap",
			policy:      Policy{Base: DefaultBase, Cap: DefaultCap},
			attempt:     1000,
			wantCeiling: int64(DefaultCap),
		},
		{
			name:        "a base already past the cap is the cap",
			policy:      Policy{Base: time.Second, Cap: 500 * time.Millisecond},
			attempt:     1,
			wantCeiling: int64(500 * time.Millisecond),
		},
		{
			name:        "a base too large to double cannot overflow the ceiling",
			policy:      Policy{Base: math.MaxInt64, Cap: DefaultCap},
			attempt:     1000,
			wantCeiling: int64(DefaultCap),
		},
		{
			name:        "a cap too large to reach cannot overflow the ceiling",
			policy:      Policy{Base: DefaultBase, Cap: math.MaxInt64},
			attempt:     1000,
			wantCeiling: math.MaxInt64,
		},
		{
			name:        "a policy with no room to wait draws nothing",
			policy:      Policy{Base: DefaultBase},
			attempt:     1,
			wantCeiling: drawnCeiling,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			asked := int64(drawnCeiling)
			policy := tc.policy
			policy.Rand = func(n int64) int64 {
				asked = n
				return n - 1
			}

			wait := policy.backoff(tc.attempt)

			if asked != tc.wantCeiling {
				t.Fatalf("ceiling = %d, want %d", asked, tc.wantCeiling)
			}

			if tc.wantCeiling == drawnCeiling {
				if wait != 0 {
					t.Fatalf("wait = %v, want 0", wait)
				}
				return
			}

			wantWait := time.Duration(tc.wantCeiling - 1)
			if wait != wantWait {
				t.Fatalf("wait = %v, want %v", wait, wantWait)
			}
		})
	}
}

func TestPolicyNormalized(t *testing.T) {
	t.Parallel()

	got := Policy{}.normalized()
	if got.Attempts != DefaultAttempts {
		t.Fatalf("Attempts = %d, want %d", got.Attempts, DefaultAttempts)
	}
	if got.Base != DefaultBase {
		t.Fatalf("Base = %v, want %v", got.Base, DefaultBase)
	}
	if got.Cap != DefaultCap {
		t.Fatalf("Cap = %v, want %v", got.Cap, DefaultCap)
	}
	if got.Sleep == nil {
		t.Fatal("Sleep is nil")
	}
	if got.Rand == nil {
		t.Fatal("Rand is nil")
	}
}

func TestPolicyNormalizedKeepsTheSeamsItWasGiven(t *testing.T) {
	t.Parallel()

	sleepCalls := 0
	customSleep := func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}
	customRand := func(n int64) int64 { return n - 1 }

	got := Policy{
		Attempts: 2,
		Base:     10 * time.Millisecond,
		Cap:      20 * time.Millisecond,
		Sleep:    customSleep,
		Rand:     customRand,
	}.normalized()

	if got.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", got.Attempts)
	}
	if got.Base != 10*time.Millisecond {
		t.Fatalf("Base = %v", got.Base)
	}
	if got.Cap != 20*time.Millisecond {
		t.Fatalf("Cap = %v", got.Cap)
	}
	if err := got.Sleep(t.Context(), 0); err != nil {
		t.Fatalf("Sleep: %v", err)
	}
	if sleepCalls != 1 {
		t.Fatalf("Sleep calls = %d, want 1", sleepCalls)
	}
	if got.Rand(8) != 7 {
		t.Fatalf("Rand did not keep the injected draw")
	}
}

func TestDefaultPinsTheDocumentedPolicy(t *testing.T) {
	t.Parallel()

	got := Default()
	if got.Attempts != DefaultAttempts || got.Base != DefaultBase || got.Cap != DefaultCap {
		t.Fatalf("Default() = %+v", got)
	}
}

func TestDefaultRandDrawsWithinTheCeilingFromEveryWorker(t *testing.T) {
	t.Parallel()

	const workers = 4
	const draws = 200

	policy := Default()
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range draws {
				n := policy.Rand(int64(time.Second))
				if n < 0 || n >= int64(time.Second) {
					errCh <- errors.New("draw outside [0, n)")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestDefaultSleepIsCutShortByCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := sleep(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep = %v, want context.Canceled", err)
	}
}

func TestDefaultSleepZeroLooksAtContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep(0) = %v, want context.Canceled", err)
	}
}

func TestDo(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("registry returned 503 Service Unavailable")
	errRefused := errors.New("registry returned 400 Bad Request")
	errLast := errors.New("registry returned 502 Bad Gateway")

	tests := []doCase{
		{
			name:      "success on the first attempt waits nothing",
			script:    []error{nil},
			wantCalls: 1,
		},
		{
			name:        "a terminal failure is not retried",
			script:      []error{errRefused},
			wantCalls:   1,
			wantErrIs:   errRefused,
			wantMessage: "registry returned 400 Bad Request",
		},
		{
			name: "a transient failure retries until it succeeds",
			script: []error{
				Transient(errUnwell, 0),
				Transient(errUnwell, 0),
				nil,
			},
			wantCalls:    3,
			wantWaits:    []time.Duration{500 * time.Millisecond, time.Second},
			wantCeilings: []int64{int64(time.Second), int64(2 * time.Second)},
		},
		{
			name: "Retry-After is a floor under the jitter",
			script: []error{
				Transient(errUnwell, 3*time.Second),
				nil,
			},
			wantCalls:    2,
			wantWaits:    []time.Duration{3 * time.Second},
			wantCeilings: []int64{int64(time.Second)},
		},
		{
			name: "Retry-After cannot exceed the cap",
			script: []error{
				Transient(errUnwell, time.Hour),
				nil,
			},
			wantCalls:    2,
			wantWaits:    []time.Duration{DefaultCap},
			wantCeilings: []int64{int64(time.Second)},
		},
		{
			name: "Retryable metadata is consumed without a Transient tag",
			script: []error{
				&blobRetryableError{err: errUnwell, retry: true},
				nil,
			},
			wantCalls:    2,
			wantWaits:    []time.Duration{500 * time.Millisecond},
			wantCeilings: []int64{int64(time.Second)},
		},
		{
			name: "attempts running out wraps the last failure",
			script: []error{
				Transient(errUnwell, 0),
				Transient(errLast, 0),
			},
			attempts:     2,
			wantCalls:    2,
			wantWaits:    []time.Duration{500 * time.Millisecond},
			wantCeilings: []int64{int64(time.Second)},
			wantErrIs:    errLast,
			wantMessage:  "after 2 attempts: registry returned 502 Bad Gateway",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertDo(t, tc)
		})
	}
}

// doCase is one [Do] table row.
type doCase struct {
	// name is the subtest name.
	name string
	// script is the error each attempt returns, in order.
	script []error
	// attempts overrides [Policy.Attempts] when positive.
	attempts int
	// wantCalls is how many times op must run.
	wantCalls int
	// wantWaits are the Sleep durations, in order.
	wantWaits []time.Duration
	// wantCeilings are the Rand bounds, in order.
	wantCeilings []int64
	// wantErrIs is the sentinel [errors.Is] must find, or nil on success.
	wantErrIs error
	// wantMessage is the exact error text when wantErrIs is set.
	wantMessage string
}

// assertDo runs one [Do] row against a recorded clock.
func assertDo(t *testing.T, tc doCase) {
	t.Helper()

	recorded := &clock{draw: halved}
	policy := recorded.policy()
	if tc.attempts > 0 {
		policy.Attempts = tc.attempts
	}

	op, calls := scripted(t, tc.script)
	err := Do(t.Context(), policy, op)

	if *calls != tc.wantCalls {
		t.Fatalf("calls = %d, want %d", *calls, tc.wantCalls)
	}
	if !durationsEqual(recorded.waits, tc.wantWaits) {
		t.Fatalf("waits = %v, want %v", recorded.waits, tc.wantWaits)
	}
	if !int64sEqual(recorded.ceilings, tc.wantCeilings) {
		t.Fatalf("ceilings = %v, want %v", recorded.ceilings, tc.wantCeilings)
	}

	if tc.wantErrIs == nil {
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		return
	}

	if !errors.Is(err, tc.wantErrIs) {
		t.Fatalf("Do error = %v, want Is(%v)", err, tc.wantErrIs)
	}
	if err.Error() != tc.wantMessage {
		t.Fatalf("message = %q, want %q", err.Error(), tc.wantMessage)
	}
}

func TestDoStopsWhenTheTransferEndsDuringAnAttempt(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("registry returned 503 Service Unavailable")
	ctx, cancel := context.WithCancel(t.Context())
	recorded := &clock{draw: halved}

	calls := 0
	op := func(context.Context) error {
		calls++
		cancel()
		return Transient(errUnwell, 0)
	}

	err := Do(ctx, recorded.policy(), op)
	if !errors.Is(err, errUnwell) {
		t.Fatalf("Do = %v, want %v", err, errUnwell)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if len(recorded.waits) != 0 {
		t.Fatalf("waits = %v, want none", recorded.waits)
	}
}

func TestDoReportsACancellationBetweenAttemptsWithTheFailureInHand(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("registry returned 503 Service Unavailable")
	ctx, cancel := context.WithCancel(t.Context())
	recorded := &clock{draw: halved, during: cancel}
	op, calls := scripted(t, []error{Transient(errUnwell, 0)})

	err := Do(ctx, recorded.policy(), op)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want Canceled", err)
	}
	if !errors.Is(err, errUnwell) {
		t.Fatalf("Do = %v, want failure in hand", err)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d, want 1", *calls)
	}
}

func TestDoStopsBeforeTheFirstAttemptWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	err := Do(ctx, Policy{}, func(context.Context) error {
		calls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestDoReportsAnInterruptedWaitWithTheFailureThatCausedIt(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("registry returned 503 Service Unavailable")
	recorded := &clock{draw: halved, interrupt: context.Canceled, interruptAt: 1}
	op, calls := scripted(t, []error{Transient(errUnwell, 0)})

	err := Do(t.Context(), recorded.policy(), op)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want Canceled", err)
	}
	if !errors.Is(err, errUnwell) {
		t.Fatalf("Do = %v, want failure in hand", err)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d, want 1", *calls)
	}
}

func TestDoRunsAPolicyThatSaysNothingAsTheDefaultOne(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("registry returned 503 Service Unavailable")
	calls := 0
	recorded := &clock{draw: halved}
	err := Do(t.Context(), Policy{Sleep: recorded.sleep, Rand: recorded.rand}, func(context.Context) error {
		calls++
		if calls < DefaultAttempts {
			return Transient(errUnwell, 0)
		}
		return Transient(errUnwell, 0)
	})
	if calls != DefaultAttempts {
		t.Fatalf("calls = %d, want %d", calls, DefaultAttempts)
	}
	if !errors.Is(err, errUnwell) {
		t.Fatalf("Do = %v", err)
	}
}

func TestDoHonorsATransportTimeoutThatMatchesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	recorded := &clock{draw: halved}
	calls := 0
	err := Do(t.Context(), recorded.policy(), func(context.Context) error {
		calls++
		if calls == 1 {
			return Transient(timeoutError{}, 0)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (transport timeout is retried)", calls)
	}
}

func TestDoObserverCountsAttemptsAfterTheFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		script      []error
		attempts    int
		interrupt   error
		interruptAt int
		wantCount   int
	}{
		{
			name:      "first-attempt success is not a retry",
			script:    []error{nil},
			wantCount: 0,
		},
		{
			name:      "terminal first failure is not a retry",
			script:    []error{errors.New("refused")},
			wantCount: 0,
		},
		{
			name: "one transient then success is one retry",
			script: []error{
				Transient(errors.New("unwell"), 0),
				nil,
			},
			wantCount: 1,
		},
		{
			name: "attempts running out still counts each retry",
			script: []error{
				Transient(errors.New("unwell"), 0),
				Transient(errors.New("still unwell"), 0),
				Transient(errors.New("last"), 0),
			},
			attempts:  3,
			wantCount: 2,
		},
		{
			name: "cancellation during the first backoff is not a retry",
			script: []error{
				Transient(errors.New("unwell"), 0),
			},
			interrupt:   context.Canceled,
			interruptAt: 1,
			wantCount:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			recorded := &clock{
				draw:        halved,
				interrupt:   tc.interrupt,
				interruptAt: tc.interruptAt,
			}
			policy := recorded.policy()
			if tc.attempts > 0 {
				policy.Attempts = tc.attempts
			}

			var count int
			ctx := WithObserver(t.Context(), func() { count++ })
			op, _ := scripted(t, tc.script)
			_ = Do(ctx, policy, op)

			if count != tc.wantCount {
				t.Fatalf("retries = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

func TestDoObserverDoesNotCountWhenContextEndsAfterBackoff(t *testing.T) {
	t.Parallel()

	errUnwell := errors.New("unwell")
	ctx, cancel := context.WithCancel(t.Context())
	recorded := &clock{draw: halved, during: cancel}
	var count int
	ctx = WithObserver(ctx, func() { count++ })
	op, calls := scripted(t, []error{Transient(errUnwell, 0)})

	err := Do(ctx, recorded.policy(), op)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want Canceled", err)
	}
	if !errors.Is(err, errUnwell) {
		t.Fatalf("Do = %v, want failure in hand", err)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d, want 1", *calls)
	}
	if count != 0 {
		t.Fatalf("retries = %d, want 0", count)
	}
}

func TestWithObserverNilLeavesContextUnchanged(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if got := WithObserver(ctx, nil); got != ctx {
		t.Fatal("nil observer allocated a derived context")
	}
}

func TestDoWithoutObserverDoesNotCount(t *testing.T) {
	t.Parallel()

	recorded := &clock{draw: halved}
	calls := 0
	err := Do(t.Context(), recorded.policy(), func(context.Context) error {
		calls++
		if calls == 1 {
			return Transient(errors.New("unwell"), 0)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("Do = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

// halved is the draw the matrix runs under: half of whatever ceiling it is
// offered. With the default base and cap it makes the schedule exactly
// 500ms, 1s, 2s.
func halved(n int64) int64 {
	return n / 2
}

// timeoutError renders the way the transport's own timeouts do: it matches
// [context.DeadlineExceeded] under [errors.Is] without any context having
// ended.
type timeoutError struct{}

// Error renders the message a dial timeout really produces.
func (timeoutError) Error() string {
	return "dial tcp 10.255.255.1:5000: i/o timeout"
}

// Is matches the deadline sentinel, exactly as net.errTimeout and
// net/http's timeout errors do.
func (timeoutError) Is(target error) bool {
	return target == context.DeadlineExceeded
}

// clock is the pair of seams a [Policy] takes as data, recording what the
// loop asked of them.
type clock struct {
	// waits are the durations Sleep was asked for, in order.
	waits []time.Duration
	// ceilings are the exclusive bounds Rand was asked to draw under, in
	// order.
	ceilings []int64
	// draw turns a ceiling into the value Rand answers with.
	draw func(n int64) int64
	// interrupt is what the Sleep at position interruptAt returns.
	interrupt error
	// interruptAt is the one-based Sleep call that returns interrupt. Zero
	// leaves every wait successful.
	interruptAt int
	// during runs in the middle of every wait. Nil does nothing.
	during func()
}

// sleep records the wait it was asked for and reports the interruption the
// fixture was built with, if this is the call that carries it.
func (c *clock) sleep(_ context.Context, d time.Duration) error {
	c.waits = append(c.waits, d)
	if c.during != nil {
		c.during()
	}
	if c.interruptAt > 0 && len(c.waits) == c.interruptAt {
		return c.interrupt
	}
	return nil
}

// rand records the ceiling it was offered and draws under it.
func (c *clock) rand(n int64) int64 {
	c.ceilings = append(c.ceilings, n)
	return c.draw(n)
}

// policy returns a four-attempt policy wired to this clock, with the default
// base and cap so the rows can talk in the numbers the design fixes.
func (c *clock) policy() Policy {
	return Policy{
		Attempts: DefaultAttempts,
		Base:     DefaultBase,
		Cap:      DefaultCap,
		Sleep:    c.sleep,
		Rand:     c.rand,
	}
}

// scripted returns an operation that answers with the next failure in script
// and counts the attempts made. An attempt past the end of the script fails
// the test.
func scripted(t *testing.T, script []error) (func(context.Context) error, *int) {
	t.Helper()
	calls := 0
	return func(context.Context) error {
		if calls >= len(script) {
			t.Fatalf("attempt %d past script of %d", calls+1, len(script))
		}
		err := script[calls]
		calls++
		return err
	}, &calls
}

// durationsEqual reports whether a and b hold the same durations in order.
func durationsEqual(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// int64sEqual reports whether a and b hold the same values in order.
func int64sEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
