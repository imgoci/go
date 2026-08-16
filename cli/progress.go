package main

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	imgoci "github.com/imgoci/go"
)

const (
	// progressPrecision is how finely a progress line reports elapsed time.
	progressPrecision = 100 * time.Millisecond
	// percentScale turns a fraction of the transfer into the integer a
	// progress line prints.
	percentScale = 100
)

// lineWriter serializes whole lines onto a writer several goroutines share.
//
// It exists because -progress adds the only concurrency this CLI's output has
// ever had: the renderer writes from its own goroutine while the command and
// the signal handler may still write diagnostics. [os.Stderr] would serialize
// them itself, but the tests drive the whole program with a [bytes.Buffer] in
// its place, and a buffer serializes nothing.
type lineWriter struct {
	// mu serializes the writes.
	mu sync.Mutex
	// out is the stream the lines go to.
	out io.Writer
}

// newLineWriter returns a writer that hands out whole lines one at a time.
func newLineWriter(out io.Writer) *lineWriter {
	return &lineWriter{out: out}
}

// Write writes p under the lock. Every caller passes one whole line, which is
// what makes the lock enough.
func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.out.Write(p)
}

// watcher is the CLI's whole progress apparatus: the snapshot the library
// hands it, and the goroutine that prints one.
//
// The library's callback only stores a snapshot and returns, so nothing this
// CLI does can slow a transfer down. A goroutine on a clock of its own renders
// whatever the last stored snapshot was.
//
// A nil *watcher is a run that asked for no progress, and its methods do
// nothing, so no caller has to check.
type watcher struct {
	// mu guards latest and seen.
	mu sync.Mutex
	// latest is the last snapshot the library delivered.
	latest imgoci.Progress
	// seen says a snapshot has arrived, before which there is nothing to
	// print and nothing worth printing a placeholder for.
	seen bool

	// out is where the lines go.
	out io.Writer
	// now reads the clock the elapsed column is measured on.
	now func() time.Time
	// started is when the watcher began, the zero of that column.
	started time.Time
	// ticks asks for a line.
	ticks <-chan time.Time
	// stopTicks releases whatever produces ticks.
	stopTicks func()
	// quit tells the renderer to leave.
	quit chan struct{}
	// finished closes when the renderer has left.
	finished chan struct{}
	// once keeps stop to a single run however many paths reach it.
	once sync.Once
}

// newWatcher starts the renderer for one transfer.
func newWatcher(out io.Writer, ticks <-chan time.Time, stopTicks func(), now func() time.Time) *watcher {
	w := &watcher{
		out:       out,
		now:       now,
		started:   now(),
		ticks:     ticks,
		stopTicks: stopTicks,
		quit:      make(chan struct{}),
		finished:  make(chan struct{}),
	}

	go w.render()

	return w
}

// record is the callback the library is given. It stores the snapshot and
// returns, which is the whole of what a progress callback should do.
func (w *watcher) record(p imgoci.Progress) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.latest = p
	w.seen = true
}

// stop ends the renderer and prints the final line.
func (w *watcher) stop() {
	if w == nil {
		return
	}

	w.once.Do(func() {
		close(w.quit)
		<-w.finished
		w.stopTicks()
		w.writeLine()
	})
}

// render prints a line on every tick until the transfer ends.
func (w *watcher) render() {
	defer close(w.finished)

	for {
		select {
		case <-w.quit:
			return
		case <-w.ticks:
			w.writeLine()
		}
	}
}

// writeLine prints one line for the latest snapshot, or nothing at all while
// none has arrived.
func (w *watcher) writeLine() {
	w.mu.Lock()
	line, ready := "", w.seen
	if ready {
		line = renderProgress(w.latest, w.now().Sub(w.started))
	}
	w.mu.Unlock()

	if ready {
		_, _ = io.WriteString(w.out, line)
	}
}

// renderProgress is the progress line: one shape, every field present every
// time, whatever the transfer is doing.
func renderProgress(p imgoci.Progress, elapsed time.Duration) string {
	return fmt.Sprintf(
		"imgoci: progress %s %s pct=%d files=%d/%d bytes=%s/%s wire=%s retries=%d fallbacks=%d elapsed=%s\n",
		p.Direction, p.Phase, progressPercent(p),
		p.CompletedFiles, p.TotalFiles,
		strconv.FormatInt(p.CompletedBytes, 10),
		strconv.FormatInt(p.TotalBytes, 10),
		strconv.FormatInt(p.WireBytes, 10),
		p.Retries, p.Fallbacks,
		elapsed.Round(progressPrecision),
	)
}

// progressPercent is how much of the transfer is in place, floored to a whole
// number. Totals the library has not learned yet read zero.
func progressPercent(p imgoci.Progress) int {
	if p.TotalBytes <= 0 {
		return 0
	}

	return int(percentScale * p.CompletedBytes / p.TotalBytes)
}

// guardStderr returns the writer every diagnostic, progress line, and signal
// message shares. Wrapping is idempotent so main and run can both ask for a
// guard without stacking locks.
func guardStderr(stderr io.Writer) io.Writer {
	if _, ok := stderr.(*lineWriter); ok {
		return stderr
	}

	return newLineWriter(stderr)
}

// startProgress starts the renderer for a transfer, or returns nil when
// -progress asked for none. Lines go to e.stderr, which run has already
// guarded, so the renderer and the command serialize on one writer.
func startProgress(e env, every time.Duration) *watcher {
	if every <= 0 {
		return nil
	}

	if e.ticks != nil {
		return newWatcher(e.stderr, e.ticks, func() {}, e.clock())
	}

	ticker := time.NewTicker(every)

	return newWatcher(e.stderr, ticker.C, ticker.Stop, e.clock())
}
