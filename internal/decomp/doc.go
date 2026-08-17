// Package decomp implements strict single-unit decompression for imgoci
// stored files, plus the size-bounded readers and writers the consumer uses
// to enforce descriptor and content-size limits.
//
// # Compression contract
//
// Spec compression names identify a transform of the stored file. A decoder
// must consume exactly one gzip member, xz stream, or zstd frame (or the
// stored bytes unchanged for "none") and reject concatenated units, padding,
// skippable frames, and any trailing byte. [Decoder] dispatches by name and
// takes the working-set ceiling one decoder may allocate.
// This build implements "none", "gzip", "xz", and "zstd". Unknown names
// fail with [ErrUnsupported].
//
// # Shared bufio invariant (gzip, xz, zstd)
//
// The stdlib gzip decoder, when given a reader that is not a
// [bufio.Reader], wraps one internally. After [compress/gzip.Reader]
// [compress/gzip.Reader.Multistream] is set false, the first member's EOF
// can leave a second member or a trailing byte inside that private buffer.
// Probing the original reader then sees [io.EOF] and the extra bytes vanish.
//
// Each opener therefore constructs one [bufio.Reader], passes it to the
// codec (gzip reuses it because [bufio.Reader] implements the flate.Reader
// byte interface; xz reuses it as [io.ByteReader]; zstd is fed through a
// frame limiter on the same buffer), and peeks that same buffer when the
// unit ends. A consumer that reads the decoder to [io.EOF] has the
// single-unit-no-trailing guarantee. [io.Closer.Close] does not drain and
// does not probe; it is idempotent and does not substitute for reading to
// EOF.
//
// # Upstream decoder behavior
//
// ulikunitz/xz v0.5.16 default NewReader concatenates streams and consumes
// 4-byte stream padding. ReaderConfig.SingleStream rejects padding,
// concatenated streams, and a trailing byte with "unexpected data after
// stream", but that one-byte probe discards the underlying Read error, and
// a stream truncated after the last Block (Index and Stream Footer missing)
// surfaces as a clean [io.EOF]. ReaderConfig.DictCap is not a cap:
// lzma.Reader2Config.fill substitutes 8 MiB for a zero DictCap and
// lzmaFilter.reader then raises whatever is configured to the capacity the
// Block Header declares, so the library allocates the declared dictionary
// however small a DictCap it is handed. Refusing the stream before
// construction is the only bound. [Decoder] reads the declared capacity out
// of the first Block Header, rejects one above its ceiling, and configures
// DictCap with the declared value so the dictionary this decoder runs on is
// stated rather than inferred from that internal maximum.
//
// xz decoding therefore tees compressed bytes through a 12-byte ring. On
// library [io.EOF] the tail must be a Stream Footer (magic "YZ", CRC-32 of
// Backward Size and Stream Flags, Backward Size that fits the stream). A
// cut at the Index indicator fails wrapping [ErrDecode]. SingleStream still
// rejects padding and concatenated streams. When SingleStream reports
// unexpected data, the tee's last non-EOF underlying error is returned with
// %w so [ErrSizeExceeded] from a [BoundedReader] and a verified-reader
// digest sentinel survive [errors.Is]; otherwise the error is wrapped with
// [ErrDecode]. The gzip-style Peek after a clean EOF is only a backstop for
// a byte SingleStream did not consume. The library itself eats trailing
// bytes on the unexpected-data path, so that Peek is not the no-trailing
// guarantee.
//
// klauspost/compress v1.18.6 zstd silently skips skippable frames (leading
// and trailing) and concatenates frames. Trailing non-magic garbage surfaces
// as unexpected EOF. A dictionary ID other than 0 surfaces as
// "unknown dictionary". The default decoder window ceiling is 512 MiB, and a
// frame turned away by that ceiling reports "decompressed size exceeds
// configured limit", which names the wrong thing. Strict decoding therefore
// inspects the 4-byte frame magic (0x28B52FFD vs skippable
// 0x184D2A50–0x184D2A5F) and the window/dictionary descriptor via zstd.Header
// before constructing a decoder, peeking HeaderMaxSize plus the 4-byte magic
// so a 4-byte Dictionary_ID field is not treated as truncated. Skippable
// frames, dictionary-required frames, and a required decode window above the
// configured ceiling fail at open, the last naming both the requirement and
// the limit. WithDecoderMaxWindow is set to the same ceiling as a backstop,
// and the decoder is limited to the first non-skippable frame by walking
// block headers so concatenated and trailing skippable frames remain in the
// shared buffer for the trailing probe.
//
// # Decode-bomb bounds
//
// Decoded output is capped by [CountingHashWriter] at io.imgoci.content.size.
// In addition, this package refuses to construct a decompressor whose
// declared working set exceeds the ceiling [Decoder] is given: zstd
// Window_Size (Frame_Content_Size for a Single_Segment frame, which is
// decoded into one buffer that size) and xz LZMA2 dictionary capacity. One
// ceiling covers both codecs; [DefaultDecoderMaxWindow] is 128 MiB, the zstd
// CLI's own default decode limit and enough for the 64 MiB dictionary of
// `xz -9`. A frame that declares a 512 MiB zstd window or a 4 GiB LZMA2
// dictionary fails with [ErrDecode] at open, before those buffers are
// allocated. Residual decoder-state cost is bounded by the declared window
// or dictionary that passed the ceiling plus the codec's own block-sized
// scratch; it does not follow a capacity the ceiling refused. The bound is
// per decoder, so concurrent decodes multiply it.
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
//
// The limit is an equality check, not a ceiling: an [io.EOF] that arrives
// before the count reaches exact means the stored file is shorter than the
// layer descriptor declares and fails wrapping [ErrSizeMismatch]. Spec §8
// requires the consumer to verify the layer digest and size, and a digest
// alone cannot catch a declared size that overstates a blob whose bytes do
// hash correctly. Both size sentinels are integrity failures, so the codec
// wrappers pass them through instead of restating them as [ErrDecode].
package decomp
