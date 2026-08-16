package decomp

import (
	"errors"
	"fmt"
	"io"
)

// Compression names from spec section 5.4. "xz" and "zstd" are recognized
// but not implemented in this build.
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
	// The error detail distinguishes a name reserved for a later slice
	// ("xz", "zstd") from an unknown name.
	ErrUnsupported = errors.New("unsupported compression")

	// ErrSizeExceeded reports that a [BoundedReader] saw more raw bytes than
	// the declared stored size, or that a [CountingHashWriter] would write
	// past io.imgoci.content.size.
	ErrSizeExceeded = errors.New("size exceeded")
)

// Decoder returns a constructor for the named compression.
//
// The constructor reads a single compression unit from r and returns an
// [io.ReadCloser] over the decoded bytes. Names "xz" and "zstd" are known
// but not implemented in this build; they fail with [ErrUnsupported] and a
// detail that names the later-slice reservation. Any other unrecognized
// name fails with [ErrUnsupported] and a detail that names it unknown.
//
// The returned constructor is safe to call concurrently with other
// constructors; each call produces an independent decoder.
func Decoder(name string) func(r io.Reader) (io.ReadCloser, error) {
	switch name {
	case nameNone:
		return openNone
	case nameGzip:
		return openGzip
	case nameXZ, nameZstd:
		return reject(name, true)
	default:
		return reject(name, false)
	}
}

// reject returns an opener that fails with [ErrUnsupported].
//
// known is true for compression names the spec defines but this build does
// not yet implement.
func reject(name string, known bool) func(io.Reader) (io.ReadCloser, error) {
	return func(_ io.Reader) (io.ReadCloser, error) {
		if known {
			return nil, fmt.Errorf(
				"decomp: compression %q is not supported in this build: %w",
				name, ErrUnsupported,
			)
		}
		return nil, fmt.Errorf("decomp: unknown compression %q: %w", name, ErrUnsupported)
	}
}
