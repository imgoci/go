package decomp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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
	rc, err := Decoder(nameZstd)(r)
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
	_, err := Decoder(nameZstd)(onlyReader{r: bytes.NewReader(stream)})
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

	_, err := Decoder(nameZstd)(onlyReader{r: bytes.NewReader(zstdDictFrame(t, []byte("payload")))})
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

	_, err := Decoder(nameZstd)(onlyReader{r: bytes.NewReader([]byte("not zstd"))})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("header error %v is not ErrDecode", err)
	}
}

func TestZstdWindowBombRejected(t *testing.T) {
	t.Parallel()

	// 10-byte 512 MiB-window frame: magic 28B52FFD, FHD 00, WD 98, RLE last block
	// 0B 00 00, one byte. Rejected at header inspect before a decoder (or 512 MiB
	// window) is allocated.
	frame := []byte("\x28\xb5\x2f\xfd\x00\x98\x0b\x00\x00\x00")
	_, err := Decoder(nameZstd)(bytes.NewReader(frame))
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("window bomb error %v is not ErrDecode", err)
	}
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
