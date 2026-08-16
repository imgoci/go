package transfer

import (
	"testing"
)

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
	var files int
	var completedBytes int64
	commitCount := 0
	for i, s := range snaps {
		if s.Direction != DirectionFetch {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.Retries != 0 {
			t.Fatalf("snap %d retries %d", i, s.Retries)
		}
		if s.CompletedFiles < files || s.CompletedBytes < completedBytes {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		files = s.CompletedFiles
		completedBytes = s.CompletedBytes
		if s.Phase == PhaseCommit {
			commitCount++
		}
	}
	last := snaps[len(snaps)-1]
	if last.Phase != PhaseCommit || last.CompletedFiles != 2 || last.CompletedBytes != 15 {
		t.Fatalf("terminal %+v", last)
	}
	if commitCount != 1 {
		t.Fatalf("commit-phase snapshots %d, want 1", commitCount)
	}
	if last.WireBytes != 7 {
		t.Fatalf("wire bytes %d", last.WireBytes)
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
