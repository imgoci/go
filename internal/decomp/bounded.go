package decomp

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// probeSize is the one extra byte [BoundedReader] reads to detect a stored
// size that overruns the declared exact limit, and the size of the
// exact-limit probe.
const probeSize = 1

// BoundedReader counts raw bytes from an underlying reader against an exact
// stored-size limit (the layer descriptor size).
//
// Each [io.Reader.Read] is capped at remaining+1 so one extra byte is
// detected without draining an unbounded hostile body. The moment the
// cumulative count would exceed exact, the read fails wrapping
// [ErrSizeExceeded].
//
// When the count lands on exact, BoundedReader issues one further Read on
// the underlying reader and requires (0, [io.EOF]). That probe is how
// go-oci-blob's verified reader gets the EOF that triggers its digest
// check. BoundedReader never synthesizes EOF: a digest mismatch from the
// underlying reader is returned as itself, and an extra byte on the probe
// is [ErrSizeExceeded].
//
// An [io.EOF] arriving before the count reaches exact is the opposite
// failure — the stored file is shorter than declared — and fails wrapping
// [ErrSizeMismatch]. That makes the declared size an equality check, which
// is what spec §8 requires of a consumer verifying a file layer.
type BoundedReader struct {
	// r is the stored-file reader, typically go-oci-blob's verified body.
	r io.Reader
	// exact is the declared stored size in bytes.
	exact int64
	// n is the cumulative count of bytes accepted from r.
	n int64
	// err is the sticky read error.
	err error
}

// NewBoundedReader returns a reader that accepts at most exact bytes from r
// and probes for a clean EOF at that limit.
func NewBoundedReader(r io.Reader, exact int64) *BoundedReader {
	return &BoundedReader{r: r, exact: exact}
}

// Read copies raw stored bytes from the underlying reader, aborting the
// moment the declared size would be exceeded and probing for (0, [io.EOF])
// once the count lands on exact.
func (b *BoundedReader) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.n >= b.exact {
		return 0, b.probe()
	}
	n, err := b.readCapped(p)
	if b.err != nil {
		return n, b.err
	}
	if b.n < b.exact {
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = short(b.n, b.exact)
			}
			b.err = err
		}
		return n, err
	}
	if probeErr := b.probe(); probeErr != nil {
		return n, probeErr
	}
	if err != nil {
		b.err = err
	}
	return n, err
}

// readCapped reads at most remaining+1 bytes so an overrun is visible on
// this call without waiting for EOF.
func (b *BoundedReader) readCapped(p []byte) (int, error) {
	remaining := b.exact - b.n
	limit := remaining
	if remaining < math.MaxInt64 {
		limit = remaining + probeSize
	}
	if int64(len(p)) > limit {
		p = p[:int(limit)]
	}
	n, err := b.r.Read(p)
	if n <= 0 {
		return 0, err
	}
	b.n += int64(n)
	if b.n <= b.exact {
		return n, err
	}
	extra := b.n - b.exact
	n -= int(extra)
	b.n = b.exact
	b.err = exceeded("stored")
	return n, b.err
}

// probe issues the exact-limit Read. Success is (0, [io.EOF]); any byte is
// [ErrSizeExceeded]; any other error, including a digest mismatch, is
// returned as itself.
func (b *BoundedReader) probe() error {
	var extra [probeSize]byte
	n, err := b.r.Read(extra[:])
	if n > 0 {
		b.err = exceeded("stored")
		return b.err
	}
	if err == nil {
		return nil
	}
	b.err = err
	return err
}

// exceeded wraps [ErrSizeExceeded] with a which-limit detail.
func exceeded(kind string) error {
	return fmt.Errorf("decomp: %s size exceeded: %w", kind, ErrSizeExceeded)
}

// short wraps [ErrSizeMismatch] with the actual and declared byte counts.
func short(got, want int64) error {
	return fmt.Errorf("decomp: stored size mismatch: read %d bytes, declared %d: %w", got, want, ErrSizeMismatch)
}

// sizeSentinel reports whether err is a raw stored-size violation from a
// [BoundedReader]. Codec wrappers use it to pass such an error through
// instead of reclassifying an integrity failure as [ErrDecode].
func sizeSentinel(err error) bool {
	return errors.Is(err, ErrSizeExceeded) || errors.Is(err, ErrSizeMismatch)
}
