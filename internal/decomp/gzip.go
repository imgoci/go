package decomp

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
)

// gzipReader is a single-member gzip decoder. br is shared with zr so the
// trailing-byte probe can see bytes the decoder buffered.
type gzipReader struct {
	// br is the single [bufio.Reader] shared by the gzip decoder and the
	// trailing-byte probe.
	br *bufio.Reader
	// zr is the stdlib gzip decoder with [gzip.Reader.Multistream] false.
	zr *gzip.Reader
	// err is the sticky read error: [io.EOF] on a clean single member, or a
	// wrapped [ErrDecode] / propagated [ErrSizeExceeded].
	err error
	// closed reports whether Close has run.
	closed bool
	// closeErr is the error from the first Close call.
	closeErr error
}

// openGzip constructs a strict single-member gzip decoder over r.
func openGzip(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	zr, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("gzip: header: %w: %w", ErrDecode, err)
	}
	zr.Multistream(false)
	return &gzipReader{br: br, zr: zr}, nil
}

// Read decompressed bytes from the single gzip member. When the member ends, a
// second member or any trailing byte in [gzipReader.br] fails wrapping
// [ErrDecode]. Subsequent Reads return the sticky error.
func (g *gzipReader) Read(p []byte) (int, error) {
	if g.err != nil {
		return 0, g.err
	}
	n, err := g.zr.Read(p)
	if err == nil {
		return n, nil
	}
	if !errors.Is(err, io.EOF) {
		g.err = wrapGzipRead(err)
		return n, g.err
	}
	if probeErr := g.probeTrailing(); probeErr != nil {
		g.err = probeErr
		return n, probeErr
	}
	g.err = io.EOF
	return n, io.EOF
}

// Close releases the decompressor. It does not drain unread decoded bytes
// and does not probe for trailing input; that check runs only when Read
// reaches the member boundary. Close is idempotent.
func (g *gzipReader) Close() error {
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	g.closeErr = g.zr.Close()
	return g.closeErr
}

// probeTrailing reports whether any byte remains after the gzip member.
func (g *gzipReader) probeTrailing() error {
	b, err := g.br.Peek(probeSize)
	if len(b) > 0 {
		return fmt.Errorf("gzip: concatenated member or trailing bytes after single member: %w", ErrDecode)
	}
	if errors.Is(err, io.EOF) || err == nil {
		return nil
	}
	return fmt.Errorf("gzip: trailing-byte probe: %w", err)
}

// wrapGzipRead maps a non-EOF gzip read error onto [ErrDecode], preserving
// [ErrSizeExceeded] and [ErrSizeMismatch] from a [BoundedReader] beneath the
// decoder: a raw stored-size violation is an integrity failure and must not
// be reclassified as a codec failure.
func wrapGzipRead(err error) error {
	if sizeSentinel(err) {
		return err
	}
	return fmt.Errorf("gzip: %w: %w", ErrDecode, err)
}
