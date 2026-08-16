// Package decomp implements strict single-unit decompression for imgoci
// stored files, plus the size-bounded readers and writers the consumer uses
// to enforce descriptor and content-size limits.
//
// # Compression contract
//
// Spec compression names identify a transform of the stored file. A decoder
// must consume exactly one gzip member, xz stream, or zstd frame (or the
// stored bytes unchanged for "none") and reject concatenated units, padding,
// skippable frames, and any trailing byte. [Decoder] dispatches by name.
// This build implements "none" and "gzip". The names "xz" and "zstd" are
// reserved for a later slice and fail with [ErrUnsupported]; unknown names
// fail with the same sentinel and a different detail string.
//
// # Shared bufio invariant (gzip)
//
// The stdlib gzip decoder, when given a reader that is not a
// [bufio.Reader], wraps one internally. After [compress/gzip.Reader]
// [compress/gzip.Reader.Multistream] is set false, the first member's EOF
// can leave a second member or a trailing byte inside that private buffer.
// Probing the original reader then sees [io.EOF] and the extra bytes vanish.
//
// The gzip opener therefore constructs one [bufio.Reader], passes it to
// [compress/gzip.NewReader] (which reuses it because [bufio.Reader]
// implements the flate.Reader byte interface), and peeks that same buffer
// when the member ends. A consumer that reads the decoder to [io.EOF] has
// the single-member-no-trailing guarantee. [io.Closer.Close] does not drain
// and does not probe; it is idempotent and does not substitute for reading
// to EOF.
//
// # Exact-limit probe (BoundedReader)
//
// [BoundedReader] counts raw bytes from the underlying reader against an
// exact stored-size limit (the layer descriptor size). Each [io.Reader.Read]
// is capped at remaining+1 so one extra byte is detected without draining
// an unbounded hostile body. The moment the cumulative count would exceed
// exact, the read fails wrapping [ErrSizeExceeded].
//
// When the count lands on exact, BoundedReader issues one further Read on
// the underlying reader and requires (0, [io.EOF]). That probe is how
// go-oci-blob's verified reader gets the EOF that triggers its digest
// check. BoundedReader never synthesizes EOF: a digest mismatch from the
// underlying reader is returned as itself, and an extra byte on the probe
// is [ErrSizeExceeded].
package decomp
