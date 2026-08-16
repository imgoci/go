package transfer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imgoci/go/internal/retry"
)

// errRetryProbe is the transient failure [TestReporterBindContextCountsRetries] retries.
var errRetryProbe = errors.New("transient probe")

func TestReporterMonotonicAndTerminalOnce(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 10}, {ContentSize: 5}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	r.addWire(4)
	r.entryVerified(10)
	r.addWire(3)
	r.entryVerified(5)
	r.finish()
	r.finish()

	if len(snaps) != 4 { // initial + 2 verified + 1 terminal
		t.Fatalf("got %d snapshots, want 4: %+v", len(snaps), snaps)
	}
	if snaps[0].Phase != PhaseStaging || snaps[0].CompletedFiles != 0 || snaps[0].TotalFiles != 2 ||
		snaps[0].TotalBytes != 15 {
		t.Fatalf("initial %+v", snaps[0])
	}
	assertFetchMonotone(t, snaps)
	last := snaps[len(snaps)-1]
	if last.Phase != PhaseCommit || last.CompletedFiles != 2 || last.CompletedBytes != 15 {
		t.Fatalf("terminal %+v", last)
	}
	if commitCount(snaps) != 1 {
		t.Fatalf("commit-phase snapshots %d, want 1", commitCount(snaps))
	}
	if last.WireBytes != 7 {
		t.Fatalf("wire bytes %d", last.WireBytes)
	}
}

func TestReporterRetryCountsAttemptsAfterFirst(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 1}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	r.addRetry()
	r.addRetry()
	r.entryVerified(1)
	r.finish()

	if len(snaps) < 3 {
		t.Fatalf("got %d snapshots, want at least initial plus two retries", len(snaps))
	}
	if snaps[1].Retries != 1 {
		t.Fatalf("first retry snapshot Retries = %d, want 1", snaps[1].Retries)
	}
	if snaps[2].Retries != 2 {
		t.Fatalf("second retry snapshot Retries = %d, want 2", snaps[2].Retries)
	}
	last := snaps[len(snaps)-1]
	if last.Retries != 2 {
		t.Fatalf("Retries = %d, want 2", last.Retries)
	}
	assertFetchMonotone(t, snaps)
}

func TestReporterMultipartLatestAbsoluteMerge(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 8}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	a := r.multipartReport()
	a(10, 1)
	a(25, 2)
	r.entryVerified(8)
	r.finish()

	last := snaps[len(snaps)-1]
	if last.WireBytes != 25 {
		t.Fatalf("WireBytes = %d, want latest 25 not 35", last.WireBytes)
	}
	if last.Retries != 2 {
		t.Fatalf("Retries = %d, want latest 2 not 3", last.Retries)
	}
	assertFetchMonotone(t, snaps)
}

func TestReporterStandardPlusMultipartAggregation(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 4}, {ContentSize: 6}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	r.addWire(4)
	r.addRetry()
	a := r.multipartReport()
	b := r.multipartReport()
	a(10, 1)
	b(20, 3)
	a(12, 2)
	r.entryVerified(4)
	r.entryVerified(6)
	r.finish()

	var sawStandardRetry bool
	for _, snap := range snaps {
		if snap.Retries == 1 && snap.WireBytes == 4 && snap.CompletedFiles == 0 {
			sawStandardRetry = true
			break
		}
	}
	if !sawStandardRetry {
		t.Fatalf("standard retry was not observable immediately: %+v", snaps)
	}
	last := snaps[len(snaps)-1]
	if last.WireBytes != 4+12+20 {
		t.Fatalf("WireBytes = %d, want 36", last.WireBytes)
	}
	if last.Retries != 1+2+3 {
		t.Fatalf("Retries = %d, want 6", last.Retries)
	}
	if last.Phase != PhaseCommit || commitCount(snaps) != 1 {
		t.Fatalf("terminal %+v commit count %d", last, commitCount(snaps))
	}
	assertFetchMonotone(t, snaps)
}

func TestReporterConcurrentSerialization(t *testing.T) {
	t.Parallel()
	const workers = 8
	var (
		mu       sync.Mutex
		snaps    []Progress
		inFlight atomic.Bool
	)
	r := newReporter([]Entry{{ContentSize: 1}, {ContentSize: 1}, {ContentSize: 1}, {ContentSize: 1}}, func(p Progress) {
		if !inFlight.CompareAndSwap(false, true) {
			t.Error("callback overlapped")
		}
		mu.Lock()
		snaps = append(snaps, p)
		mu.Unlock()
		inFlight.Store(false)
	})

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			r.addWire(1)
			r.addRetry()
			report := r.multipartReport()
			report(3, 1)
			report(5, 2)
		})
	}
	wg.Wait()
	r.entryVerified(1)
	r.entryVerified(1)
	r.entryVerified(1)
	r.entryVerified(1)
	r.finish()
	r.finish()

	mu.Lock()
	defer mu.Unlock()
	assertFetchMonotone(t, snaps)
	last := snaps[len(snaps)-1]
	if last.WireBytes != int64(workers)+int64(workers*5) {
		t.Fatalf("WireBytes = %d, want %d", last.WireBytes, workers+workers*5)
	}
	if last.Retries != workers+workers*2 {
		t.Fatalf("Retries = %d, want %d", last.Retries, workers+workers*2)
	}
	if commitCount(snaps) != 1 {
		t.Fatalf("commit-phase snapshots %d, want 1", commitCount(snaps))
	}
}

func TestReporterBindContextCountsRetries(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 1}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	ctx := r.bindContext(t.Context())
	attempts := 0
	err := retry.Do(ctx, retry.Policy{
		Attempts: 3,
		Sleep:    func(context.Context, time.Duration) error { return nil },
		Rand:     func(int64) int64 { return 0 },
	}, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return retry.Transient(errRetryProbe, 0)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	var sawFirst, sawSecond bool
	for _, snap := range snaps {
		switch snap.Retries {
		case 1:
			sawFirst = true
		case 2:
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Fatalf("retry snapshots not emitted immediately: %+v", snaps)
	}
	r.entryVerified(1)
	r.finish()
	if snaps[len(snaps)-1].Retries != 2 {
		t.Fatalf("Retries = %d, want 2", snaps[len(snaps)-1].Retries)
	}
}

func TestReporterBindContextCancellationDuringBackoffDoesNotCount(t *testing.T) {
	t.Parallel()
	var snaps []Progress
	r := newReporter([]Entry{{ContentSize: 1}}, func(p Progress) {
		snaps = append(snaps, p)
	})
	ctx, cancel := context.WithCancel(t.Context())
	ctx = r.bindContext(ctx)
	err := retry.Do(ctx, retry.Policy{
		Attempts: 3,
		Sleep: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
		Rand: func(int64) int64 { return 0 },
	}, func(context.Context) error {
		return retry.Transient(errRetryProbe, 0)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want Canceled", err)
	}
	if !errors.Is(err, errRetryProbe) {
		t.Fatalf("Do = %v, want probe in hand", err)
	}
	r.entryVerified(1)
	r.finish()
	if last := snaps[len(snaps)-1]; last.Retries != 0 {
		t.Fatalf("Retries = %d, want 0 after cancel during backoff", last.Retries)
	}
}

func TestReporterNilCallbackSkipsOptionalWork(t *testing.T) {
	t.Parallel()
	r := newReporter([]Entry{{ContentSize: 3}}, nil)
	if r.watching() {
		t.Fatal("nil callback is watching")
	}
	if r.multipartReport() != nil {
		t.Fatal("nil callback allocated a multipart report")
	}
	r.addWire(3)
	r.addRetry()
	r.entryVerified(3)
	r.addFallbacks(1)
	if r.standardWire != 0 || r.standardRetries != 0 || r.current.Fallbacks != 0 {
		t.Fatalf("nil callback recorded optional counters: %+v", r.current)
	}
}

func TestWorkerCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		requested, entries, want int
	}{
		{requested: 0, entries: 10, want: defaultWorkers},
		{requested: -1, entries: 10, want: defaultWorkers},
		{requested: 8, entries: 3, want: 3},
		{requested: 2, entries: 10, want: 2},
		{requested: 0, entries: 0, want: 1},
	}
	for _, tc := range tests {
		if got := workerCount(tc.requested, tc.entries); got != tc.want {
			t.Fatalf("workerCount(%d,%d)=%d want %d", tc.requested, tc.entries, got, tc.want)
		}
	}
}

// assertFetchMonotone requires absolute fetch snapshots stay monotone.
func assertFetchMonotone(t *testing.T, snaps []Progress) {
	t.Helper()
	var (
		files     int
		completed int64
		wire      int64
		retries   int
		fallbacks int
	)
	for i, s := range snaps {
		if s.Direction != DirectionFetch {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.CompletedFiles < files || s.CompletedBytes < completed ||
			s.WireBytes < wire || s.Retries < retries || s.Fallbacks < fallbacks {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		if s.TotalFiles != snaps[0].TotalFiles || s.TotalBytes != snaps[0].TotalBytes {
			t.Fatalf("snap %d totals changed: %+v", i, s)
		}
		files = s.CompletedFiles
		completed = s.CompletedBytes
		wire = s.WireBytes
		retries = s.Retries
		fallbacks = s.Fallbacks
	}
}

// commitCount returns how many snapshots used [PhaseCommit].
func commitCount(snaps []Progress) int {
	n := 0
	for _, s := range snaps {
		if s.Phase == PhaseCommit {
			n++
		}
	}
	return n
}
