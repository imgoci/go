package decomp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"testing"

	"github.com/ulikunitz/xz"
)

func xzStream(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func openXZDecoder(t *testing.T, r io.Reader) io.ReadCloser {
	t.Helper()
	rc, err := Decoder(nameXZ)(r)
	if err != nil {
		t.Fatalf("open xz: %v", err)
	}
	return rc
}

func TestXZSingleStream(t *testing.T) {
	t.Parallel()

	payload := []byte("hello imgoci xz")
	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(xzStream(t, payload))})
	t.Cleanup(func() { _ = rc.Close() })

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decoded %q, want %q", got, payload)
	}
}

func TestXZPaddingRejected(t *testing.T) {
	t.Parallel()

	stream := append(append([]byte{}, xzStream(t, []byte("payload"))...), 0, 0, 0, 0)
	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestXZConcatenatedStreamsRejected(t *testing.T) {
	t.Parallel()

	first := xzStream(t, []byte("first"))
	second := xzStream(t, []byte("second"))
	stream := append(append([]byte{}, first...), second...)

	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestXZTrailingByteHiddenInBufioRejected(t *testing.T) {
	t.Parallel()

	member := xzStream(t, []byte("payload"))
	stream := append(append([]byte{}, member...), 0xFF)
	src := &oneShotReader{data: stream}

	rc := openXZDecoder(t, src)
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestXZDecodeBombCeiling(t *testing.T) {
	t.Parallel()
	assertDecodeBomb(t, nameXZ, xzStream(t, bytes.Repeat([]byte{0}, decodeBombPayload)))
}

func TestXZCloseDoesNotProbe(t *testing.T) {
	t.Parallel()

	member := xzStream(t, []byte("still unread"))
	stream := append(append([]byte{}, member...), 0x01)
	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	if err := rc.Close(); err != nil {
		t.Fatalf("Close before EOF: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestXZCorruptRejected(t *testing.T) {
	t.Parallel()

	_, err := Decoder(nameXZ)(onlyReader{r: bytes.NewReader([]byte("not xz"))})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("header error %v is not ErrDecode", err)
	}
}

func TestXZTruncatedAtIndexRejected(t *testing.T) {
	t.Parallel()

	// A mid-stream prefix cut still sits inside the Block. ulikunitz reports a
	// clean EOF when Index+Footer are dropped after a complete Block, so the cut
	// is the Index indicator.
	stream := xzStream(t, []byte("hello imgoci xz index cut"))
	truncated := xzDropIndexAndFooter(t, stream)
	if len(truncated) == 0 || len(truncated) >= len(stream) {
		t.Fatalf("truncated len %d, full len %d", len(truncated), len(stream))
	}

	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(truncated)})
	t.Cleanup(func() { _ = rc.Close() })
	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("truncated-at-index error %v is not ErrDecode", err)
	}
}

func TestXZBoundedReaderExtraByte(t *testing.T) {
	t.Parallel()

	stream := xzStream(t, []byte("payload"))
	extra := append(append([]byte{}, stream...), 0xFF)
	br := NewBoundedReader(bytes.NewReader(extra), int64(len(stream)))
	rc := openXZDecoder(t, onlyReader{r: br})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("error %v is not ErrSizeExceeded", err)
	}
}

func TestXZUnderlyingSentinelPreserved(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("verified-reader sentinel")
	stream := xzStream(t, []byte("payload"))
	src := eofSentinelReader{r: bytes.NewReader(stream), err: sentinel}
	rc := openXZDecoder(t, onlyReader{r: src})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v is not the underlying sentinel", err)
	}
}

func TestXZDictionaryBombRejected(t *testing.T) {
	t.Parallel()

	stream := xzDictBomb(t)
	if len(stream) >= 100 {
		t.Fatalf("dict-bomb fixture is %d bytes, want <100", len(stream))
	}
	_, err := Decoder(nameXZ)(onlyReader{r: bytes.NewReader(stream)})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("dict bomb error %v is not ErrDecode", err)
	}
}

// eofSentinelReader replaces a clean [io.EOF] with err so a verified-reader
// digest mismatch at the stored-size boundary can be observed.
type eofSentinelReader struct {
	r   io.Reader
	err error
}

func (e eofSentinelReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, e.err
	}
	return n, err
}

// xzDropIndexAndFooter returns stream without the Index and Stream Footer.
// The last Block is kept; the cut is the Index indicator.
func xzDropIndexAndFooter(t *testing.T, stream []byte) []byte {
	t.Helper()
	if len(stream) < xzFooterLen {
		t.Fatal("stream shorter than a Stream Footer")
	}
	footer := stream[len(stream)-xzFooterLen:]
	if string(footer[xzFooterMagicOff:]) != xzFooterMagic {
		t.Fatal("fixture is missing Stream Footer magic YZ")
	}
	indexSize := (int(binary.LittleEndian.Uint32(footer[xzFooterBackSizeOff:xzFooterFlagsOff])) + 1) * xzIndexAlign
	cut := len(stream) - xzFooterLen - indexSize
	if cut <= xzStreamHeaderLen {
		t.Fatalf("index+footer %d leaves cut %d", xzFooterLen+indexSize, cut)
	}
	return stream[:cut]
}

// xzDictBomb is a <100-byte xz prefix whose first Block Header declares
// LZMA2 property byte 40 (4 GiB-1 dictionary).
func xzDictBomb(t *testing.T) []byte {
	t.Helper()
	hdr := make([]byte, xzStreamHeaderLen)
	copy(hdr, xzStreamMagic)
	const crc64Flag = 0x04
	hdr[7] = crc64Flag
	binary.LittleEndian.PutUint32(hdr[8:], crc32.ChecksumIEEE(hdr[6:8]))

	const encodedSize = 2
	block := make([]byte, (encodedSize+1)*xzBlockSizeUnit)
	block[0] = encodedSize
	block[2] = xzLZMA2ID
	block[3] = xzLZMA2PropLen
	block[4] = xzDictMaxCode
	binary.LittleEndian.PutUint32(block[len(block)-4:], crc32.ChecksumIEEE(block[:len(block)-4]))

	if decode, err := decodeXZDictCap(xzDictMaxCode); err != nil || decode != xzDictMaxCap {
		t.Fatalf("property %d decoded to %d, %v; want %d", xzDictMaxCode, decode, err, xzDictMaxCap)
	}
	out := make([]byte, len(hdr)+len(block))
	copy(out, hdr)
	copy(out[len(hdr):], block)
	return out
}
