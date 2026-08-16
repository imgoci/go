package retry

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestTransient(t *testing.T) {
	t.Parallel()

	errHungUp := errors.New("the registry hung up")

	t.Run("nil stays nil", func(t *testing.T) {
		t.Parallel()
		if got := Transient(nil, time.Second); got != nil {
			t.Fatalf("Transient(nil) = %v, want nil", got)
		}
	})

	t.Run("renders the underlying failure unchanged", func(t *testing.T) {
		t.Parallel()
		got := Transient(errHungUp, time.Second)
		if got.Error() != errHungUp.Error() {
			t.Fatalf("Error() = %q, want %q", got.Error(), errHungUp.Error())
		}
	})

	t.Run("leaves the failure reachable", func(t *testing.T) {
		t.Parallel()
		probe := &probeError{wraps: errHungUp}
		tagged := Transient(fmt.Errorf("outer: %w", probe), 2*time.Second)
		if !errors.Is(tagged, errHungUp) {
			t.Fatal("errors.Is did not find the sentinel under the tag")
		}
		var found *probeError
		if !errors.As(tagged, &found) {
			t.Fatal("errors.As did not find the probe under the tag")
		}
		if found != probe {
			t.Fatal("errors.As found a different probe")
		}
	})
}

func TestIsTransient(t *testing.T) {
	t.Parallel()

	errHungUp := errors.New("the registry hung up")
	after := 3 * time.Second

	tests := []struct {
		name      string
		err       error
		wantOK    bool
		wantAfter time.Duration
	}{
		{name: "untagged is terminal", err: errHungUp},
		{name: "nil is terminal"},
		{
			name:      "constructor tag is transient with its hint",
			err:       Transient(errHungUp, after),
			wantOK:    true,
			wantAfter: after,
		},
		{
			name:      "outermost tag wins",
			err:       Transient(Transient(errHungUp, time.Second), after),
			wantOK:    true,
			wantAfter: after,
		},
		{
			name:      "tag survives wrapping",
			err:       fmt.Errorf("upload: %w", Transient(errHungUp, after)),
			wantOK:    true,
			wantAfter: after,
		},
		{
			name:      "Retryable true is transient",
			err:       &blobRetryableError{err: errHungUp, after: after, retry: true},
			wantOK:    true,
			wantAfter: after,
		},
		{
			name:   "Retryable true without RetryAfter has zero hint",
			err:    retryableFlagError{err: errHungUp, retry: true},
			wantOK: true,
		},
		{
			name: "Retryable false is terminal",
			err:  &blobRetryableError{err: errHungUp, after: after, retry: false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertIsTransient(t, tc.err, tc.wantAfter, tc.wantOK)
		})
	}
}

func assertIsTransient(t *testing.T, err error, wantAfter time.Duration, wantOK bool) {
	t.Helper()
	gotAfter, ok := IsTransient(err)
	if ok != wantOK {
		t.Fatalf("IsTransient ok = %v, want %v", ok, wantOK)
	}
	if gotAfter != wantAfter {
		t.Fatalf("after = %v, want %v", gotAfter, wantAfter)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value string
		now   time.Time
		want  time.Duration
	}{
		{name: "empty", value: "", now: now, want: 0},
		{name: "delta seconds", value: "5", now: now, want: 5 * time.Second},
		{name: "delta seconds zero", value: "0", now: now, want: 0},
		{name: "delta seconds negative", value: "-1", now: now, want: 0},
		{name: "delta seconds overflow", value: "9223372036854775807", now: now, want: 0},
		{name: "garbage", value: "soon", now: now, want: 0},
		{
			name:  "http date imf-fixdate",
			value: now.Add(10 * time.Second).Format(http.TimeFormat),
			now:   now,
			want:  10 * time.Second,
		},
		{
			name:  "http date rfc850",
			value: now.Add(7 * time.Second).UTC().Format(time.RFC850),
			now:   now,
			want:  7 * time.Second,
		},
		{
			name:  "http date already past",
			value: now.Add(-time.Second).Format(http.TimeFormat),
			now:   now,
			want:  0,
		},
		{
			name:  "http date ansic",
			value: now.Add(3 * time.Second).UTC().Format(time.ANSIC),
			now:   now,
			want:  3 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseRetryAfter(tc.value, tc.now)
			if got != tc.want {
				t.Fatalf("ParseRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// probeError is a concrete error type the transparency tests find with
// [errors.As].
type probeError struct {
	// wraps is the sentinel the probe carries.
	wraps error
}

func (p *probeError) Error() string {
	return "probe: " + p.wraps.Error()
}

func (p *probeError) Unwrap() error {
	return p.wraps
}

// blobRetryableError stands in for a go-oci-blob error that exposes Retryable()
// bool and RetryAfter() [time.Duration].
type blobRetryableError struct {
	// err is the underlying failure.
	err error
	// after is the peer-requested wait.
	after time.Duration
	// retry is the Retryable() verdict.
	retry bool
}

func (e *blobRetryableError) Error() string {
	return e.err.Error()
}

func (e *blobRetryableError) Unwrap() error {
	return e.err
}

func (e *blobRetryableError) Retryable() bool {
	return e.retry
}

func (e *blobRetryableError) RetryAfter() time.Duration {
	return e.after
}

// retryableFlagError implements only Retryable() bool.
type retryableFlagError struct {
	// err is the underlying failure.
	err error
	// retry is the Retryable() verdict.
	retry bool
}

func (e retryableFlagError) Error() string {
	return e.err.Error()
}

func (e retryableFlagError) Retryable() bool {
	return e.retry
}
