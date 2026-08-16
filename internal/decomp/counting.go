package decomp

import (
	"io"

	"github.com/opencontainers/go-digest"
)

// CountingHashWriter writes decoded content while hashing it and aborting
// the moment a write would pass io.imgoci.content.size. A hostile decode
// bomb cannot push the output past that ceiling.
type CountingHashWriter struct {
	// w is the staged-file writer.
	w io.Writer
	// d hashes the bytes actually accepted.
	d digest.Digester
	// n is the cumulative count of bytes accepted.
	n int64
	// ceiling is io.imgoci.content.size: the most bytes a decode may emit.
	ceiling int64
	// err is the sticky write error.
	err error
}

// NewCountingHashWriter returns a writer that copies into w, hashes with
// SHA-256, and fails wrapping [ErrSizeExceeded] rather than accept more
// than ceiling bytes.
func NewCountingHashWriter(w io.Writer, ceiling int64) *CountingHashWriter {
	return &CountingHashWriter{
		w:       w,
		d:       digest.Canonical.Digester(),
		ceiling: ceiling,
	}
}

// Write copies p into the underlying writer, hashing what is accepted.
// A write that would pass the ceiling is truncated to the remaining budget
// and then fails wrapping [ErrSizeExceeded].
func (c *CountingHashWriter) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	remaining := c.ceiling - c.n
	if remaining <= 0 {
		if len(p) == 0 {
			return 0, nil
		}
		c.err = exceeded("content")
		return 0, c.err
	}
	chunk := p
	over := false
	if int64(len(p)) > remaining {
		chunk = p[:int(remaining)]
		over = true
	}
	n, err := c.writeHashed(chunk)
	if err != nil {
		c.err = err
		return n, err
	}
	if over {
		c.err = exceeded("content")
		return n, c.err
	}
	return n, nil
}

// writeHashed copies p to the underlying writer and hashes the accepted
// prefix.
func (c *CountingHashWriter) writeHashed(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		_, _ = c.d.Hash().Write(p[:n])
		c.n += int64(n)
	}
	return n, err
}

// Digest is the SHA-256 of the bytes accepted so far.
func (c *CountingHashWriter) Digest() digest.Digest {
	return c.d.Digest()
}

// Size is the number of bytes accepted so far.
func (c *CountingHashWriter) Size() int64 {
	return c.n
}
