package decomp

import (
	"errors"
	"fmt"
	"io"
)

// Compression names from spec section 5.4.
const (
	nameNone = "none"
	nameGzip = "gzip"
	nameXZ   = "xz"
	nameZstd = "zstd"
)

// Sentinel errors for the decomp contract. Callers match with [errors.Is].
var (
	// ErrDecode reports a strict decompression violation: a corrupt stream,
	// concatenated members, or any trailing byte after a single unit.
	ErrDecode = errors.New("decode")

	// ErrUnsupported reports a compression name this build cannot decode.
	ErrUnsupported = errors.New("unsupported compression")

	// ErrSizeExceeded reports that a [BoundedReader] saw more raw bytes than
	// the declared stored size, or that a [CountingHashWriter] would write
	// past io.imgoci.content.size.
	ErrSizeExceeded = errors.New("size exceeded")
)

// Decoder returns a constructor for the named compression.
//
// The constructor reads a single compression unit from r and returns an
// [io.ReadCloser] over the decoded bytes. Recognized names are "none",
// "gzip", "xz", and "zstd". Any unrecognized name fails with
// [ErrUnsupported] and a detail that names it unknown.
//
// The returned constructor is safe to call concurrently with other
// constructors; each call produces an independent decoder.
func Decoder(name string) func(r io.Reader) (io.ReadCloser, error) {
	switch name {
	case nameNone:
		return openNone
	case nameGzip:
		return openGzip
	case nameXZ:
		return openXZ
	case nameZstd:
		return openZstd
	default:
		return rejectUnknown(name)
	}
}

// rejectUnknown returns an opener that fails with [ErrUnsupported] for a
// compression name the spec does not define.
func rejectUnknown(name string) func(io.Reader) (io.ReadCloser, error) {
	return func(_ io.Reader) (io.ReadCloser, error) {
		return nil, fmt.Errorf("decomp: unknown compression %q: %w", name, ErrUnsupported)
	}
}
