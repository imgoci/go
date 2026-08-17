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

// DefaultDecoderMaxWindow is the default working-set ceiling one decoder may
// allocate: 128 MiB, covering both the zstd window and the xz LZMA2
// dictionary.
//
// 128 MiB is the zstd CLI's own default decode limit
// (ZSTD_WINDOWLOG_LIMIT_DEFAULT, windowLog 27), so a frame produced by
// `zstd --long=27` decodes here exactly where the reference tool would take
// it. It also covers `xz -9`, whose LZMA2 dictionary is 64 MiB.
//
// The bound is per active decoder, not per transfer: concurrent role
// transfers each hold their own working set, so peak decoder memory is this
// value times the number of entries decoding at once.
const DefaultDecoderMaxWindow uint64 = 128 << 20

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

	// ErrSizeMismatch reports that a [BoundedReader] reached a clean end of
	// stream with fewer raw bytes than the declared stored size. It is the
	// underrun counterpart of [ErrSizeExceeded], which reports an overrun:
	// together they make the layer descriptor size an equality check rather
	// than a ceiling. Both are integrity failures, not decode failures.
	ErrSizeMismatch = errors.New("size mismatch")
)

// Decoder returns a constructor for the named compression.
//
// The constructor reads a single compression unit from r and returns an
// [io.ReadCloser] over the decoded bytes. Recognized names are "none",
// "gzip", "xz", and "zstd". Any unrecognized name fails with
// [ErrUnsupported] and a detail that names it unknown.
//
// maxWindow caps the working set a single decoder may allocate: the zstd
// Window_Size an ordinary frame declares (or the Frame_Content_Size a
// single-segment frame declares) and the LZMA2 dictionary capacity an xz
// Block Header declares. A stored file that needs more fails with
// [ErrDecode] at open, before the buffer is allocated.
// [DefaultDecoderMaxWindow] is the value callers use unless they have a
// reason not to. "none" and "gzip" have no such working set and ignore it.
//
// The returned constructor is safe to call concurrently with other
// constructors; each call produces an independent decoder.
func Decoder(name string, maxWindow uint64) func(r io.Reader) (io.ReadCloser, error) {
	switch name {
	case nameNone:
		return openNone
	case nameGzip:
		return openGzip
	case nameXZ:
		return func(r io.Reader) (io.ReadCloser, error) { return openXZ(r, maxWindow) }
	case nameZstd:
		return func(r io.Reader) (io.ReadCloser, error) { return openZstd(r, maxWindow) }
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
