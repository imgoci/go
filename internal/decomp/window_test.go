package decomp

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/ulikunitz/xz"
)

// realToolFixture is a stored file produced by the reference command-line
// compressor. Decoded length and digest are recorded in testdata/README.md by
// an independent oracle (xz -dc / zstd -dc piped to shasum), never by this
// package's decoders.
type realToolFixture struct {
	// name is the subtest name.
	name string
	// path is the committed fixture, relative to the package directory.
	path string
	// compression is the spec compression name that decodes it.
	compression string
	// declaredWindow is the working set the stream declares: the LZMA2
	// dictionary capacity for xz, Window_Size for zstd.
	declaredWindow uint64
	// decodedSize is the decoded byte length from testdata/README.md.
	decodedSize int
	// decodedSHA256 is the hex decoded digest from testdata/README.md.
	decodedSHA256 string
}

// realToolFixtures returns the committed reference-compressor stored files.
// Both declare a working set above 8 MiB and no larger than
// [DefaultDecoderMaxWindow]. The decoded lengths and digests are the literals
// recorded in testdata/README.md, never recomputed here.
func realToolFixtures() []realToolFixture {
	return []realToolFixture{
		{
			name:           "xz -9",
			path:           "testdata/xz-9.xz",
			compression:    nameXZ,
			declaredWindow: 64 << 20,
			decodedSize:    21,
			decodedSHA256:  "626f31a02a7566ac80c1b2752775ab4e84382385fc11bbbee85312e628218aca",
		},
		{
			name:           "zstd --long=27",
			path:           "testdata/zstd-long-27.zst",
			compression:    nameZstd,
			declaredWindow: 128 << 20,
			decodedSize:    33554432,
			decodedSHA256:  "83ee47245398adee79bd9c0a8bc57b821e92aba10f5f9ade8a5d1fae4d8c4302",
		},
	}
}

// TestRealToolFixturesDecodeAtDefaultWindow decodes each reference-compressor
// fixture at [DefaultDecoderMaxWindow] and compares the output length and
// digest against the values recorded in testdata/README.md.
func TestRealToolFixturesDecodeAtDefaultWindow(t *testing.T) {
	t.Parallel()

	for _, fx := range realToolFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			if fx.declaredWindow > DefaultDecoderMaxWindow {
				t.Fatalf("fixture declares %d, above the default %d", fx.declaredWindow, DefaultDecoderMaxWindow)
			}
			stored := readFixture(t, fx.path)

			rc, err := Decoder(fx.compression, DefaultDecoderMaxWindow)(onlyReader{r: bytes.NewReader(stored)})
			if err != nil {
				t.Fatalf("open %s at the default window: %v", fx.compression, err)
			}
			t.Cleanup(func() { _ = rc.Close() })

			sum := sha256.New()
			n, err := io.Copy(sum, rc)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if n != int64(fx.decodedSize) {
				t.Fatalf("decoded %d bytes, want the recorded %d", n, fx.decodedSize)
			}
			if got := hex.EncodeToString(sum.Sum(nil)); got != fx.decodedSHA256 {
				t.Fatalf("decoded digest %s, want the recorded %s", got, fx.decodedSHA256)
			}
		})
	}
}

// TestRealToolFixturesRejectedBelowDeclaredWindow configures a ceiling under
// each fixture's declared working set and requires [ErrDecode].
func TestRealToolFixturesRejectedBelowDeclaredWindow(t *testing.T) {
	t.Parallel()

	// A ceiling below both fixtures' declared working sets.
	const lowered = 8 << 20

	for _, fx := range realToolFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			t.Parallel()
			stored := readFixture(t, fx.path)

			rc, err := Decoder(fx.compression, lowered)(onlyReader{r: bytes.NewReader(stored)})
			if err == nil {
				t.Cleanup(func() { _ = rc.Close() })
				_, err = io.Copy(io.Discard, rc)
			}
			if !errors.Is(err, ErrDecode) {
				t.Fatalf("error %v is not ErrDecode", err)
			}
		})
	}
}

// TestXZDeclaredDictionaryIsConfigured covers the dictionary plumbing on a
// stream whose farthest match sits beyond 8 MiB: [inspectXZDictCap] returns
// the declared capacity, the decoder built from it reaches the exact payload
// at the default ceiling, and an 8 MiB ceiling refuses the stream.
func TestXZDeclaredDictionaryIsConfigured(t *testing.T) {
	t.Parallel()

	const (
		dictCap    = 32 << 20
		markerLen  = 1 << 10
		zerosLen   = 9 << 20
		matchDelta = zerosLen + markerLen
	)
	if matchDelta <= 8<<20 {
		t.Fatalf("match distance %d is inside the 8 MiB default dictionary", matchDelta)
	}

	payload := make([]byte, 0, 2*markerLen+zerosLen)
	marker := make([]byte, markerLen)
	for i := range marker {
		marker[i] = byte(i*7 + 1)
	}
	payload = append(payload, marker...)
	payload = append(payload, make([]byte, zerosLen)...)
	payload = append(payload, marker...)

	var buf bytes.Buffer
	w, err := xz.WriterConfig{DictCap: dictCap}.NewWriter(&buf)
	if err != nil {
		t.Fatalf("xz writer: %v", err)
	}
	if _, err = w.Write(payload); err != nil {
		t.Fatalf("xz write: %v", err)
	}
	if err = w.Close(); err != nil {
		t.Fatalf("xz close: %v", err)
	}
	stored := buf.Bytes()

	declared, err := inspectXZDictCap(bufio.NewReader(bytes.NewReader(stored)), DefaultDecoderMaxWindow)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if declared != dictCap {
		t.Fatalf("declared dictionary %d, want %d", declared, dictCap)
	}

	rc := openXZDecoder(t, onlyReader{r: bytes.NewReader(stored)})
	t.Cleanup(func() { _ = rc.Close() })
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decoded %d bytes that differ from the %d-byte payload", len(got), len(payload))
	}

	if _, err := Decoder(nameXZ, 8<<20)(onlyReader{r: bytes.NewReader(stored)}); !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v at an 8 MiB limit is not ErrDecode", err)
	}
}

// readFixture reads a committed testdata stored file.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return stored
}
