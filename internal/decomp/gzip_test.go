package decomp

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"
)

// onlyReader implements only [io.Reader], not [io.ByteReader], so the stdlib
// gzip decoder would wrap a private [bufio.Reader] if given this stream
// directly. The opener must share its own [bufio.Reader] with the probe.
type onlyReader struct {
	r io.Reader
}

func (o onlyReader) Read(p []byte) (int, error) {
	return o.r.Read(p)
}

// oneShotReader returns data once, then panics on any further Read. A
// trailing byte that lives only in the shared [bufio.Reader] buffer is visible to
// Peek without a second underlying Read; a probe of the original reader
// panics.
type oneShotReader struct {
	data   []byte
	called bool
}

func (o *oneShotReader) Read(p []byte) (int, error) {
	if o.called {
		panic("probe read the underlying stream; trailing byte should still be buffered")
	}
	o.called = true
	n := copy(p, o.data)
	return n, io.EOF
}

func gzipMember(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func openGzipDecoder(t *testing.T, r io.Reader) io.ReadCloser {
	t.Helper()
	rc, err := Decoder(nameGzip)(r)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	return rc
}

func TestGzipSingleMember(t *testing.T) {
	t.Parallel()

	payload := []byte("hello imgoci")
	rc := openGzipDecoder(t, onlyReader{r: bytes.NewReader(gzipMember(t, payload))})
	t.Cleanup(func() { _ = rc.Close() })

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("decoded %q, want %q", got, payload)
	}
}

func TestGzipConcatenatedMembersRejected(t *testing.T) {
	t.Parallel()

	first := gzipMember(t, []byte("first"))
	second := gzipMember(t, []byte("second"))
	stream := append(append([]byte{}, first...), second...)

	rc := openGzipDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestGzipTrailingByteHiddenInBufioRejected(t *testing.T) {
	t.Parallel()

	// The member plus one trailing byte is far smaller than bufio's default
	// buffer, so one underlying Read fills both into the shared [bufio.Reader].
	// After the member ends the original reader is exhausted; the probe must
	// consult the buffered byte. oneShotReader panics if the probe misses it
	// and reads the underlying stream again.
	member := gzipMember(t, []byte("payload"))
	stream := append(append([]byte{}, member...), 0xFF)
	src := &oneShotReader{data: stream}

	rc := openGzipDecoder(t, src)
	t.Cleanup(func() { _ = rc.Close() })

	_, err := io.ReadAll(rc)
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("error %v is not ErrDecode", err)
	}
}

func TestGzipCorruptRejected(t *testing.T) {
	t.Parallel()

	_, err := Decoder(nameGzip)(onlyReader{r: bytes.NewReader([]byte("not gzip"))})
	if !errors.Is(err, ErrDecode) {
		t.Fatalf("header error %v is not ErrDecode", err)
	}
}

func TestGzipCloseDoesNotProbe(t *testing.T) {
	t.Parallel()

	payload := []byte("still unread")
	member := gzipMember(t, payload)
	stream := append(append([]byte{}, member...), 0x01)
	rc := openGzipDecoder(t, onlyReader{r: bytes.NewReader(stream)})
	if err := rc.Close(); err != nil {
		t.Fatalf("Close before EOF: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
