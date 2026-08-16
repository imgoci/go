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
	// percentScale turns a completed/total byte fraction into the integer a
	// progress line prints.
	percentScale = 100
)

// lineWriter serializes whole-line writes from the progress renderer, the
// command, and the signal handler. [os.Stderr] serializes on its own; the
// [bytes.Buffer] tests inject does not.
type lineWriter struct {
	// mu serializes writes so concurrent emitters cannot interleave a line.
	mu sync.Mutex
	// out is the stream the lines are written to.
	out io.Writer
}

// newLineWriter wraps out in a lineWriter.
func newLineWriter(out io.Writer) *lineWriter {
	return &lineWriter{out: out}
}

// Write writes p under the lock. Callers pass one whole line, so the lock is
// enough.
func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.out.Write(p)
}

// watcher stores the latest progress snapshot and prints it from a goroutine on
// its own clock so the library callback never blocks a transfer.
//
// A nil *watcher is a run that asked for no progress; its methods no-op.
type watcher struct {
	// mu guards latest and seen.
	mu sync.Mutex
	// latest is the last snapshot the library delivered.
	latest imgoci.Progress
	// seen is true after the first snapshot. Until then writeLine prints nothing.
	seen bool

	// out is the stream the progress lines are written to.
	out io.Writer
	// now is the clock the elapsed column is measured on.
	now func() time.Time
	// started is the elapsed-column origin.
	started time.Time
	// ticks fires once per progress line.
	ticks <-chan time.Time
	// stopTicks releases the ticker, or is a no-op when tests inject ticks.
	stopTicks func()
	// quit stops the renderer.
	quit chan struct{}
	// finished closes after the renderer returns.
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

// record stores the snapshot and returns; it is the library progress callback.
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

// writeLine prints one line for the latest snapshot, or nothing until one has
// arrived.
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

// renderProgress formats one progress line. Every field is present on every
// line.
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

// progressPercent is the transfer fraction floored to a whole number. Unknown
// totals read zero.
func progressPercent(p imgoci.Progress) int {
	if p.TotalBytes <= 0 {
		return 0
	}

	return int(percentScale * p.CompletedBytes / p.TotalBytes)
}

// guardStderr returns the writer every diagnostic, progress line, and signal
// message shares. Wrapping is idempotent so main and run can both ask without
// stacking locks.
func guardStderr(stderr io.Writer) io.Writer {
	if _, ok := stderr.(*lineWriter); ok {
		return stderr
	}

	return newLineWriter(stderr)
}

// startProgress starts the renderer, or returns nil when -progress is unset.
// Lines go to e.stderr, which run has already guarded.
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
