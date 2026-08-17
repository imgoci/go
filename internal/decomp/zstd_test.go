package decomp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const (
	// zstdSkippableMagicLE is skippable-frame magic 0x184D2A50 little-endian.
	zstdSkippableMagicLE = 0x184D2A50
	// zstdTestDictID is a non-zero dictionary ID for dictionary-required frames.
	zstdTestDictID = 42
)

func zstdFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	return enc.EncodeAll(payload, nil)
}

func zstdSkippable(payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], zstdSkippableMagicLE)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[8:], payload)
	return out
}

func zstdDictFrame(t *testing.T, payload []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderDictRaw(zstdTestDictID, []byte("imgoci-zstd-test-dictionary")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	frame := enc.EncodeAll(payload, nil)
	var h zstd.Header
	if err := h.Decode(frame); err != nil {
		t.Fatalf("dict frame header: %v", err)
	}
	if h.DictionaryID == 0 {
		t.Fatal("encoder omitted dictionary ID; cannot test dictionary-required frames")
	}
	return frame
}

func openZstdDecoder(t *testing.T, r io.Reader) io.ReadCloser {
	t.Helper()
	rc, err := Decoder(nameZstd, DefaultDecoderMaxWindow)(r)
	if err != nil {
		t.Fatalf("open zstd: %v", err)
	}
	return rc
}

func TestZstdSingleFrame(t *testing.T) {
	t.Parallel()

	payload := []byte("hello imgoci zstd")
	rc := openZstdDecoder(t, onlyReader{r: bytes.NewReader(zstdFrame(t, payload))})
	t.Cleanup(func() { _ = rc.Close() })

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decoded %q, want %q", got, payload)
	}
}

func TestZstdLeadingSkippableRejected(t *testing.T) {
	t.Parallel()

	stream := append(zstdSkippable([]byte("skip")), zstdFrame(t, []byte("payload"))...)
	_, err := Decoder(nameZstd, DefaultDecoderMaxWindow)(onlyReader{r: bytes.NewReader(stream)})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestZstdTrailingSkippableRejected(t *testing.T) {
	t.Parallel()

	stream := append(append([]byte{}, zstdFrame(t, []byte("payload"))...), zstdSkippable([]byte("skip"))...)
	rc := openZstdDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestZstdConcatenatedFramesRejected(t *testing.T) {
	t.Parallel()

	first := zstdFrame(t, []byte("first"))
	second := zstdFrame(t, []byte("second"))
	stream := append(append([]byte{}, first...), second...)

	rc := openZstdDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestZstdDictionaryRequiredRejected(t *testing.T) {
	t.Parallel()

	_, err := Decoder(nameZstd, DefaultDecoderMaxWindow)(onlyReader{r: bytes.NewReader(zstdDictFrame(t, []byte("payload")))})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestZstdTrailingGarbageRejected(t *testing.T) {
	t.Parallel()

	stream := append(append([]byte{}, zstdFrame(t, []byte("payload"))...), 0xFF)
	src := &oneShotReader{data: stream}

	rc := openZstdDecoder(t, src)
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestZstdDecodeBombCeiling(t *testing.T) {
	t.Parallel()
	assertDecodeBomb(t, nameZstd, zstdFrame(t, bytes.Repeat([]byte{0}, decodeBombPayload)))
}

func TestZstdCloseDoesNotProbe(t *testing.T) {
	t.Parallel()

	frame := zstdFrame(t, []byte("still unread"))
	stream := append(append([]byte{}, frame...), 0x01)
	rc := openZstdDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	if err := rc.Close(); err != nil {
		t.Fatalf("Close before EOF: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestZstdCorruptRejected(t *testing.T) {
	t.Parallel()

	_, err := Decoder(nameZstd, DefaultDecoderMaxWindow)(onlyReader{r: bytes.NewReader([]byte("not zstd"))})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("header error %v is not ErrDecode", err)
	}
}

func TestZstdWindowBombRejectedAtConfiguredLimit(t *testing.T) {
	t.Parallel()

	// 10-byte 512 MiB-window frame: magic 28B52FFD, FHD 00, WD 98, RLE last block
	// 0B 00 00, one byte. Rejected at header inspect before a decoder (or 512 MiB
	// window) is allocated.
	frame := []byte("\x28\xb5\x2f\xfd\x00\x98\x0b\x00\x00\x00")

	tests := []struct {
		name      string
		maxWindow uint64
	}{
		{name: "package default", maxWindow: DefaultDecoderMaxWindow},
		{name: "lowered limit", maxWindow: 8 << 20},
		{name: "raised limit still under 512 MiB", maxWindow: 256 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Decoder(nameZstd, tt.maxWindow)(bytes.NewReader(frame))
			if !errors.Is(err, ErrDecode) {
				t.Fatalf("window bomb error %v is not ErrDecode", err)
			}
			limit := strconv.FormatUint(tt.maxWindow, 10)
			if !strings.Contains(err.Error(), limit) {
				t.Fatalf("error %q does not name the configured limit %s", err, limit)
			}
		})
	}
}

// TestZstdSingleSegmentRequirementNamesWindow covers the diagnostic a
// Single_Segment frame used to get. Such a frame declares no window and is
// decoded into one buffer the size of its Frame_Content_Size, so klauspost
// turned it away with "decompressed size exceeds configured limit" — a
// content-size complaint about a window-policy decision.
func TestZstdSingleSegmentRequirementNamesWindow(t *testing.T) {
	t.Parallel()

	const declared = 64 << 20
	frame := zstdSingleSegmentFrame(declared)

	if _, err := Decoder(nameZstd, DefaultDecoderMaxWindow)(bytes.NewReader(frame)); err != nil {
		t.Fatalf("header inspect at the default rejected a %d-byte single-segment frame: %v", declared, err)
	}

	_, err := Decoder(nameZstd, 8<<20)(bytes.NewReader(frame))
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("single-segment error %v is not ErrDecode", err)
	}
	if !strings.Contains(err.Error(), "decode window") {
		t.Fatalf("error %q does not name the required decode window", err)
	}
	for _, want := range []string{strconv.Itoa(declared), strconv.Itoa(8 << 20)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %s", err, want)
		}
	}
	if strings.Contains(err.Error(), "decompressed size exceeds configured limit") {
		t.Fatalf("error %q reports a content-size limit for a window-policy rejection", err)
	}
}

// zstdSingleSegmentFrame is a frame header with Single_Segment_Flag set,
// Frame_Content_Size_Flag=3 (8-byte FCS), no Dictionary_ID, and no window
// descriptor, followed by a last RLE block. fcs is the declared content size,
// which is the buffer such a frame requires.
func zstdSingleSegmentFrame(fcs uint64) []byte {
	const (
		// singleSegmentFHD sets Frame_Content_Size_Flag=3 (0xc0) and
		// Single_Segment_Flag (0x20).
		singleSegmentFHD = 0xe0
		fcsSize          = 8
		rleLast          = 0x0b
	)
	out := append([]byte{0x28, 0xb5, 0x2f, 0xfd, singleSegmentFHD}, make([]byte, fcsSize)...)
	binary.LittleEndian.PutUint64(out[len(out)-fcsSize:], fcs)
	return append(out, rleLast, 0, 0, 'A')
}

func TestZstdZeroDictionaryIDAccepted(t *testing.T) {
	t.Parallel()

	// DID_Flag=3 (4-byte Dictionary_ID), DID=0, FCS_Flag=3 (8-byte FCS).
	// Peek(HeaderMaxSize) is one byte short of this header; the extra
	// 4-byte magic allowance must keep it from looking truncated.
	frame := zstdZeroDIDFourByteFrame()
	rc := openZstdDecoder(t, onlyReader{r: bytes.NewReader(frame)})
	t.Cleanup(func() { _ = rc.Close() })

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, []byte{'A'}) {
		t.Fatalf("decoded %q, want %q", got, "A")
	}
}

// zstdZeroDIDFourByteFrame is a 1-byte RLE frame with Dictionary_ID_Flag=3,
// Dictionary_ID=0, Frame_Content_Size_Flag=3, and a 1 KiB window.
func zstdZeroDIDFourByteFrame() []byte {
	const (
		fhd     = 0xc3
		window  = 0x00
		fcsSize = 8
		rleLast = 0x0b
	)
	out := []byte{0x28, 0xb5, 0x2f, 0xfd, fhd, window, 0, 0, 0, 0}
	fcs := make([]byte, fcsSize)
	fcs[0] = 1
	out = append(out, fcs...)
	return append(out, rleLast, 0, 0, 'A')
}

// TestZstdBoundedReaderShortStreamPropagatesSizeMismatch covers a complete
// zstd frame whose enclosing layer descriptor declares one byte more than
// the blob holds. The raw underrun is an integrity failure and must reach
// the caller as [ErrSizeMismatch], not as a codec [ErrDecode].
func TestZstdBoundedReaderShortStreamPropagatesSizeMismatch(t *testing.T) {
	t.Parallel()

	frame := zstdFrame(t, []byte("payload"))
	br := NewBoundedReader(bytes.NewReader(frame), int64(len(frame))+1)
	rc := openZstdDecoder(t, onlyReader{r: br})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("error %v is not ErrSizeMismatch", err)
	}
	if errors.Is(err, ErrDecode) {
		t.Fatalf("stored-size underrun was reclassified as ErrDecode: %v", err)
	}
}
