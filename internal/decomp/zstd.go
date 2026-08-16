package decomp

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

const (
	// zstdMagicSize is the 4-byte frame or skippable-frame magic.
	zstdMagicSize = 4
	// zstdBlockHeaderSize is the 3-byte zstd block header.
	zstdBlockHeaderSize = 3
	// zstdChecksumSize is the optional 4-byte content checksum.
	zstdChecksumSize = 4
	// zstdBlockLastMask is bit 0 of the block header (last-block flag).
	zstdBlockLastMask = 1
	// zstdBlockTypeShift is the shift for the 2-bit block type.
	zstdBlockTypeShift = 1
	// zstdBlockTypeMask is the 2-bit block-type field.
	zstdBlockTypeMask = 3
	// zstdBlockSizeShift is the shift for the 21-bit block size.
	zstdBlockSizeShift = 3
	// zstdBlockHeaderHiShift is the bit shift for block-header byte 2.
	zstdBlockHeaderHiShift = 16
	// zstdBlockTypeRaw is an uncompressed block.
	zstdBlockTypeRaw = 0
	// zstdBlockTypeRLE is a 1-byte run-length block.
	zstdBlockTypeRLE = 1
	// zstdBlockTypeCompressed is a compressed block.
	zstdBlockTypeCompressed = 2
	// zstdSkippableNibbleMask selects the high nibble of skippable magic byte 0.
	zstdSkippableNibbleMask = 0xf0
	// zstdSkippableNibble is the high nibble of skippable magic byte 0 (0x50–0x5F).
	zstdSkippableNibble = 0x50
	// maxDecodeWindow is the largest zstd window or xz LZMA2 dictionary this
	// package will allocate. 8 MiB is the zstd CLI default window for
	// compression levels ≤19 and ulikunitz/xz's default DictCap.
	maxDecodeWindow = 8 << 20
)

// zstdSkippableMagicTail is bytes 1–3 of a skippable-frame magic (LE 0x184D2A5?).
const zstdSkippableMagicTail = "\x2a\x4d\x18"

// zstdReader is a single-frame zstd decoder. br is shared with the
// frame-limiter so the trailing-byte probe can see bytes the decoder
// buffered.
type zstdReader struct {
	// br is the single [bufio.Reader] shared by the limiter and the
	// trailing-byte probe.
	br *bufio.Reader
	// zr is the klauspost zstd decoder, fed one frame by [zstdLimitReader].
	zr *zstd.Decoder
	// err is the sticky read error: [io.EOF] on a clean single frame, or a
	// wrapped [ErrDecode] / propagated [ErrSizeExceeded].
	err error
	// closed reports whether Close has run.
	closed bool
	// closeErr is the error from the first Close call.
	closeErr error
}

// zstdLimitReader feeds the decoder exactly one non-skippable zstd frame
// from br. Concatenated frames and trailing skippable frames stay in br
// for the trailing-byte probe.
type zstdLimitReader struct {
	// br is the shared [bufio.Reader].
	br *bufio.Reader
	// remain is how many compressed bytes of the current region the decoder
	// may still take.
	remain int
	// hasChecksum reports that a 4-byte content checksum follows the last
	// block.
	hasChecksum bool
	// afterLast reports that the last block has been handed over.
	afterLast bool
	// needChecksum reports that the checksum region has not yet been issued.
	needChecksum bool
	// done reports that the first frame is fully handed over.
	done bool
}

// openZstd constructs a strict single-frame zstd decoder over r.
func openZstd(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	h, err := inspectZstdHeader(br)
	if err != nil {
		return nil, err
	}
	limited := &zstdLimitReader{br: br, remain: h.HeaderSize, hasChecksum: h.HasCheckSum}
	zr, err := zstd.NewReader(
		limited,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(uint64(maxDecodeWindow)),
	)
	if err != nil {
		return nil, fmt.Errorf("zstd: header: %w: %w", ErrDecode, err)
	}
	return &zstdReader{br: br, zr: zr}, nil
}

// Read decompressed bytes from the single zstd frame. When the frame ends,
// concatenated frames, skippable frames, or any trailing byte in
// [zstdReader.br] fail wrapping [ErrDecode]. Subsequent Reads return the sticky
// error.
func (z *zstdReader) Read(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}
	n, err := z.zr.Read(p)
	if err == nil {
		return n, nil
	}
	if !errors.Is(err, io.EOF) {
		z.err = wrapZstdRead(err)
		return n, z.err
	}
	if probeErr := z.probeTrailing(); probeErr != nil {
		z.err = probeErr
		return n, probeErr
	}
	z.err = io.EOF
	return n, io.EOF
}

// Close releases the decompressor. It does not drain unread decoded bytes
// and does not probe for trailing input; that check runs only when Read
// reaches the frame boundary. Close is idempotent.
func (z *zstdReader) Close() error {
	if z.closed {
		return z.closeErr
	}
	z.closed = true
	z.zr.Close()
	return z.closeErr
}

// probeTrailing reports whether any byte remains after the zstd frame.
func (z *zstdReader) probeTrailing() error {
	b, err := z.br.Peek(probeSize)
	if len(b) > 0 {
		return fmt.Errorf("zstd: concatenated frame or trailing bytes after single frame: %w", ErrDecode)
	}
	if errors.Is(err, io.EOF) || err == nil {
		return nil
	}
	return fmt.Errorf("zstd: trailing-byte probe: %w", err)
}

// wrapZstdRead maps a non-EOF zstd read error onto [ErrDecode], preserving
// [ErrSizeExceeded] from a [BoundedReader] beneath the decoder.
func wrapZstdRead(err error) error {
	if errors.Is(err, ErrSizeExceeded) {
		return err
	}
	return fmt.Errorf("zstd: %w: %w", ErrDecode, err)
}

// inspectZstdHeader peeks the frame header. Skippable frames,
// dictionary-required frames, and windows above [maxDecodeWindow] fail wrapping
// [ErrDecode] before a decoder is constructed.
//
// The peek is [zstd.HeaderMaxSize] plus the 4-byte magic so a Frame_Header
// with Dictionary_ID_Flag=3 (4-byte DID) and Frame_Content_Size_Flag=3
// (8-byte FCS) is not short of the bytes [zstd.Header.Decode] needs.
func inspectZstdHeader(br *bufio.Reader) (zstd.Header, error) {
	peeked, err := br.Peek(zstd.HeaderMaxSize + zstdMagicSize)
	if len(peeked) < zstdMagicSize {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return zstd.Header{}, fmt.Errorf("zstd: header: %w: %w", ErrDecode, err)
	}
	if zstdSkippableMagic(peeked[:zstdMagicSize]) {
		return zstd.Header{}, fmt.Errorf("zstd: skippable frame: %w", ErrDecode)
	}
	var h zstd.Header
	if decErr := h.Decode(peeked); decErr != nil {
		return zstd.Header{}, fmt.Errorf("zstd: header: %w: %w", ErrDecode, decErr)
	}
	if h.DictionaryID != 0 {
		return zstd.Header{}, fmt.Errorf("zstd: dictionary-required frame: %w", ErrDecode)
	}
	if !h.SingleSegment && h.WindowSize > uint64(maxDecodeWindow) {
		return zstd.Header{}, fmt.Errorf(
			"zstd: window size %d exceeds %d: %w",
			h.WindowSize, maxDecodeWindow, ErrDecode,
		)
	}
	return h, nil
}

// zstdSkippableMagic reports whether magic is a skippable-frame magic
// (0x184D2A50–0x184D2A5F little-endian).
func zstdSkippableMagic(magic []byte) bool {
	return len(magic) >= zstdMagicSize &&
		magic[0]&zstdSkippableNibbleMask == zstdSkippableNibble &&
		string(magic[1:zstdMagicSize]) == zstdSkippableMagicTail
}

// Read copies compressed bytes of the first zstd frame from the shared
// buffer. Once that frame ends, further reads return [io.EOF] so the
// decoder cannot consume a concatenated or skippable follow-on frame.
func (z *zstdLimitReader) Read(p []byte) (int, error) {
	if err := z.ensureRemain(); err != nil {
		return 0, err
	}
	if z.done {
		return 0, io.EOF
	}
	if len(p) > z.remain {
		p = p[:z.remain]
	}
	n, err := z.br.Read(p)
	z.remain -= n
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	}
	if z.remain == 0 {
		if advErr := z.ensureRemain(); advErr != nil {
			return n, advErr
		}
	}
	if n == 0 && z.done {
		return 0, io.EOF
	}
	if n == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	return n, nil
}

// ensureRemain parses the next frame region when the current one is spent.
func (z *zstdLimitReader) ensureRemain() error {
	for z.remain == 0 && !z.done {
		if err := z.advance(); err != nil {
			return err
		}
	}
	return nil
}

// advance issues the next header, block, or checksum region, or marks the
// frame done.
func (z *zstdLimitReader) advance() error {
	switch {
	case z.needChecksum:
		z.remain = zstdChecksumSize
		z.needChecksum = false
		return nil
	case z.afterLast:
		z.done = true
		return nil
	default:
		return z.nextBlock()
	}
}

// nextBlock peeks one zstd block header and allows that block through.
func (z *zstdLimitReader) nextBlock() error {
	hdr, err := z.br.Peek(zstdBlockHeaderSize)
	if len(hdr) < zstdBlockHeaderSize {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return err
	}
	last, body, err := parseZstdBlockSize(hdr)
	if err != nil {
		return err
	}
	z.remain = zstdBlockHeaderSize + body
	if last {
		z.afterLast = true
		z.needChecksum = z.hasChecksum
	}
	return nil
}

// parseZstdBlockSize returns the last-block flag and compressed body size
// (not including the 3-byte header) for a zstd block header.
func parseZstdBlockSize(hdr []byte) (bool, int, error) {
	bh := uint32(hdr[0]) | uint32(hdr[1])<<8 | uint32(hdr[2])<<zstdBlockHeaderHiShift
	last := bh&zstdBlockLastMask != 0
	typ := (bh >> zstdBlockTypeShift) & zstdBlockTypeMask
	size := int(bh >> zstdBlockSizeShift)
	switch typ {
	case zstdBlockTypeRaw, zstdBlockTypeCompressed:
		return last, size, nil
	case zstdBlockTypeRLE:
		return last, 1, nil
	default:
		return false, 0, fmt.Errorf("zstd: reserved block type: %w", ErrDecode)
	}
}
