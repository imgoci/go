package decomp

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/ulikunitz/xz"
)

const (
	// xzStreamHeaderLen is the xz Stream Header length (magic, flags, CRC).
	xzStreamHeaderLen = 12
	// xzFooterLen is the xz Stream Footer length.
	xzFooterLen = 12
	// xzFooterMagicOff is the offset of the 'Y','Z' magic in a Stream Footer.
	xzFooterMagicOff = 10
	// xzFooterBackSizeOff is the offset of the little-endian Backward Size.
	xzFooterBackSizeOff = 4
	// xzFooterCRCLen is the CRC-32 covering Backward Size and Stream Flags.
	xzFooterCRCLen = 4
	// xzFooterFlagsOff is the start of the 2-byte Stream Flags in the footer.
	xzFooterFlagsOff = 8
	// xzFooterFlagsLen is the Stream Flags field length.
	xzFooterFlagsLen = 2
	// xzMinIndexSize is the smallest legal Index (indicator + padding to 4).
	xzMinIndexSize = 4
	// xzIndexAlign is the Index/footer Backward Size alignment.
	xzIndexAlign = 4
	// xzMagicLen is the Stream Header magic length.
	xzMagicLen = 6
	// xzBlockSizeUnit converts a Block Header Size byte into a byte count.
	xzBlockSizeUnit = 4
	// xzFilterCountMask is the 2-bit Number of Filters field minus one.
	xzFilterCountMask = 0x03
	// xzCompressedSizeBit is Block Flags bit 6 (Compressed Size present).
	xzCompressedSizeBit = 0x40
	// xzUncompressedSizeBit is Block Flags bit 7 (Uncompressed Size present).
	xzUncompressedSizeBit = 0x80
	// xzLZMA2ID is the LZMA2 Filter ID.
	xzLZMA2ID = 0x21
	// xzLZMA2PropLen is the LZMA2 property-byte length.
	xzLZMA2PropLen = 1
	// xzDictMaxCode is the LZMA2 dictionary property byte for 4 GiB-1.
	xzDictMaxCode = 40
	// xzDictMaxCap is the dictionary capacity encoded by [xzDictMaxCode].
	xzDictMaxCap = 1<<32 - 1
	// xzDictExpBias is the exponent bias in the LZMA2 property-byte encoding.
	xzDictExpBias = 11
	// xzDictExpMask is the 5-bit exponent field after shifting out the mantissa.
	xzDictExpMask = 0x1f
	// xzDictMantissaBit is the low bit of the 2-bit mantissa (2 | bit).
	xzDictMantissaBit = 1
	// xzDictMantissaBase is the mantissa base in the LZMA2 property-byte encoding.
	xzDictMantissaBase = 2
	// xzBlockHeaderMin is the size-byte plus Block Flags.
	xzBlockHeaderMin = 2
)

// xzStreamMagic is the six-byte xz Stream Header magic.
const xzStreamMagic = "\xfd7zXZ\x00"

// xzFooterMagic is the two-byte Stream Footer magic.
const xzFooterMagic = "YZ"

// xzReader is a single-stream xz decoder. br is shared with zr so the
// trailing-byte probe can see bytes the decoder buffered.
type xzReader struct {
	// br is the single [bufio.Reader] shared by the xz decoder and the
	// trailing-byte probe.
	br *bufio.Reader
	// src tees compressed bytes for Stream Footer verification and retains
	// the last non-EOF underlying error the library would otherwise drop.
	src *xzSource
	// zr is the ulikunitz xz decoder with SingleStream set.
	zr *xz.Reader
	// err is the sticky read error: [io.EOF] on a clean single stream, or a
	// wrapped [ErrDecode] / propagated [ErrSizeExceeded].
	err error
	// closed reports whether Close has run.
	closed bool
	// closeErr is the error from the first Close call.
	closeErr error
}

// xzSource sits under the shared [bufio.Reader]. It keeps the last
// [xzFooterLen] compressed bytes and the last non-EOF Read error.
type xzSource struct {
	// r is the raw stored-file reader, typically a [BoundedReader].
	r io.Reader
	// buf holds the most recent compressed bytes, up to [xzFooterLen].
	buf [xzFooterLen]byte
	// n is how many bytes of buf are valid.
	n int
	// total is the number of compressed bytes observed.
	total int64
	// err is the last non-EOF error from r.
	err error
}

// openXZ constructs a strict single-stream xz decoder over r.
func openXZ(r io.Reader) (io.ReadCloser, error) {
	src := &xzSource{r: r}
	br := bufio.NewReader(src)
	if err := inspectXZDictCap(br); err != nil {
		return nil, err
	}
	zr, err := xz.ReaderConfig{SingleStream: true}.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("xz: header: %w: %w", ErrDecode, err)
	}
	return &xzReader{br: br, src: src, zr: zr}, nil
}

// Read decompressed bytes from the single xz stream. On library [io.EOF], the
// compressed tail must be a Stream Footer ([xzSource.checkFooter]) and
// [xzReader.br] must have no further byte. Concatenated streams, padding, a
// missing Index+Footer, or a trailing byte fail wrapping [ErrDecode].
// Subsequent Reads return the sticky error.
func (x *xzReader) Read(p []byte) (int, error) {
	if x.err != nil {
		return 0, x.err
	}
	n, err := x.zr.Read(p)
	if err == nil {
		return n, nil
	}
	if !errors.Is(err, io.EOF) {
		x.err = x.wrapRead(err)
		return n, x.err
	}
	if ferr := x.src.checkFooter(); ferr != nil {
		x.err = ferr
		return n, ferr
	}
	if probeErr := x.probeTrailing(); probeErr != nil {
		x.err = probeErr
		return n, probeErr
	}
	x.err = io.EOF
	return n, io.EOF
}

// Close releases the decompressor. It does not drain unread decoded bytes
// and does not probe for trailing input; that check runs only when Read
// reaches the stream boundary. Close is idempotent.
func (x *xzReader) Close() error {
	if x.closed {
		return x.closeErr
	}
	x.closed = true
	return x.closeErr
}

// probeTrailing reports whether any byte remains after the xz stream.
func (x *xzReader) probeTrailing() error {
	b, err := x.br.Peek(probeSize)
	if len(b) > 0 {
		return fmt.Errorf("xz: concatenated stream or trailing bytes after single stream: %w", ErrDecode)
	}
	if errors.Is(err, io.EOF) || err == nil {
		return nil
	}
	return fmt.Errorf("xz: trailing-byte probe: %w", err)
}

// wrapRead maps a non-EOF xz read error onto [ErrDecode], preserving
// [ErrSizeExceeded] and any other underlying sentinel the library's
// SingleStream one-byte probe would replace with "unexpected data after
// stream".
func (x *xzReader) wrapRead(err error) error {
	if errors.Is(err, ErrSizeExceeded) {
		return err
	}
	if srcErr := x.src.err; srcErr != nil {
		if errors.Is(srcErr, ErrSizeExceeded) {
			return srcErr
		}
		return fmt.Errorf("xz: trailing-byte probe: %w", srcErr)
	}
	return fmt.Errorf("xz: %w: %w", ErrDecode, err)
}

// Read copies compressed bytes from the underlying reader, retaining a
// Stream Footer-sized tail and any non-EOF error.
func (s *xzSource) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.push(p[:n])
	}
	if err != nil && !errors.Is(err, io.EOF) {
		s.err = err
	}
	return n, err
}

// push records p into the Stream Footer tail ring.
func (s *xzSource) push(p []byte) {
	s.total += int64(len(p))
	if len(p) >= xzFooterLen {
		copy(s.buf[:], p[len(p)-xzFooterLen:])
		s.n = xzFooterLen
		return
	}
	overflow := s.n + len(p) - xzFooterLen
	if overflow > 0 {
		copy(s.buf[:], s.buf[overflow:s.n])
		s.n -= overflow
	}
	copy(s.buf[s.n:], p)
	s.n += len(p)
}

// checkFooter reports whether the retained tail is a plausible xz Stream
// Footer: magic 'Y','Z', CRC-32, and a Backward Size that fits the stream.
func (s *xzSource) checkFooter() error {
	if s.n < xzFooterLen {
		return fmt.Errorf("xz: truncated: missing stream footer: %w", ErrDecode)
	}
	if string(s.buf[xzFooterMagicOff:xzFooterLen]) != xzFooterMagic {
		return fmt.Errorf("xz: truncated: missing stream footer: %w", ErrDecode)
	}
	want := crc32.ChecksumIEEE(s.buf[xzFooterCRCLen : xzFooterFlagsOff+xzFooterFlagsLen])
	got := binary.LittleEndian.Uint32(s.buf[:xzFooterCRCLen])
	if want != got {
		return fmt.Errorf("xz: stream footer CRC: %w", ErrDecode)
	}
	indexSize := (int64(binary.LittleEndian.Uint32(s.buf[xzFooterBackSizeOff:xzFooterFlagsOff])) + 1) * xzIndexAlign
	if indexSize < xzMinIndexSize {
		return fmt.Errorf("xz: stream footer backward size: %w", ErrDecode)
	}
	need := int64(xzStreamHeaderLen) + indexSize + int64(xzFooterLen)
	if s.total < need {
		return fmt.Errorf("xz: stream footer backward size: %w", ErrDecode)
	}
	return nil
}

// inspectXZDictCap peeks the first Block Header after the Stream Header and
// rejects an LZMA2 dictionary above [maxDecodeWindow] before the library can
// allocate it. Incomplete or non-xz input is left for [xz.Reader] to diagnose.
func inspectXZDictCap(br *bufio.Reader) error {
	peeked, err := br.Peek(xzStreamHeaderLen + probeSize)
	if len(peeked) < xzStreamHeaderLen+probeSize {
		return nil
	}
	if err != nil && len(peeked) == 0 {
		return nil
	}
	if string(peeked[:xzMagicLen]) != xzStreamMagic {
		return nil
	}
	sizeByte := peeked[xzStreamHeaderLen]
	if sizeByte == 0 {
		return nil
	}
	blockLen := (int(sizeByte) + 1) * xzBlockSizeUnit
	peeked, err = br.Peek(xzStreamHeaderLen + blockLen)
	if len(peeked) < xzStreamHeaderLen+blockLen {
		return nil
	}
	if err != nil && len(peeked) == 0 {
		return nil
	}
	dictCap, ok := xzBlockLZMA2Dict(peeked[xzStreamHeaderLen : xzStreamHeaderLen+blockLen])
	if !ok {
		return nil
	}
	if dictCap > int64(maxDecodeWindow) {
		return fmt.Errorf("xz: LZMA2 dictionary %d exceeds %d: %w", dictCap, maxDecodeWindow, ErrDecode)
	}
	return nil
}

// xzBlockLZMA2Dict returns the LZMA2 dictionary capacity declared in a Block
// Header, or false if the header cannot be parsed far enough to find it.
func xzBlockLZMA2Dict(block []byte) (int64, bool) {
	if len(block) < xzBlockHeaderMin {
		return 0, false
	}
	flags := block[1]
	nfilters := int(flags&xzFilterCountMask) + 1
	rest := block[2:]
	if flags&xzCompressedSizeBit != 0 {
		var ok bool
		_, rest, ok = xzUvarint(rest)
		if !ok {
			return 0, false
		}
	}
	if flags&xzUncompressedSizeBit != 0 {
		var ok bool
		_, rest, ok = xzUvarint(rest)
		if !ok {
			return 0, false
		}
	}
	for range nfilters {
		id, next, ok := xzUvarint(rest)
		if !ok {
			return 0, false
		}
		size, next, ok := xzUvarint(next)
		if !ok || size > uint64(len(next)) {
			return 0, false
		}
		propLen := int(size) //nolint:gosec // G115: size is capped by len(next).
		props := next[:propLen]
		rest = next[propLen:]
		if id != xzLZMA2ID {
			continue
		}
		if size != xzLZMA2PropLen {
			return 0, false
		}
		dictCap, err := decodeXZDictCap(props[0])
		if err != nil {
			return 0, false
		}
		return dictCap, true
	}
	return 0, false
}

// xzUvarint decodes one xz/LEB128 unsigned integer from p.
func xzUvarint(p []byte) (uint64, []byte, bool) {
	v, n := binary.Uvarint(p)
	if n <= 0 {
		return 0, p, false
	}
	return v, p[n:], true
}

// decodeXZDictCap mirrors ulikunitz lzma.DecodeDictCap: the property byte is
// a 2-bit mantissa / exponent encoding, and code 40 is 4 GiB-1.
func decodeXZDictCap(c byte) (int64, error) {
	if c > xzDictMaxCode {
		return 0, fmt.Errorf("xz: invalid LZMA2 dictionary code %d", c)
	}
	if c == xzDictMaxCode {
		return xzDictMaxCap, nil
	}
	mantissa := xzDictMantissaBase | int64(c)&xzDictMantissaBit
	exp := xzDictExpBias + int(c>>1)&xzDictExpMask
	return mantissa << exp, nil
}
