package decomp

import (
	"bytes"
	"errors"
	"testing"

	"github.com/opencontainers/go-digest"
)

const contentCeiling = 4

func TestCountingHashWriterPassthrough(t *testing.T) {
	t.Parallel()

	payload := []byte("abcd")
	var buf bytes.Buffer
	w := NewCountingHashWriter(&buf, contentCeiling)
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("wrote %d, want %d", n, len(payload))
	}
	if w.Size() != int64(len(payload)) {
		t.Fatalf("Size() = %d, want %d", w.Size(), len(payload))
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("buf = %q, want %q", buf.Bytes(), payload)
	}
	want := digest.Canonical.FromBytes(payload)
	if w.Digest() != want {
		t.Fatalf("Digest() = %s, want %s", w.Digest(), want)
	}
}

func TestCountingHashWriterCeilingAbortMidStream(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewCountingHashWriter(&buf, contentCeiling)

	n, err := w.Write([]byte("ab"))
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if n != 2 {
		t.Fatalf("first Write n = %d, want 2", n)
	}

	n, err = w.Write([]byte("cdef"))
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("crossing Write error %v is not ErrSizeExceeded", err)
	}
	if n != 2 {
		t.Fatalf("crossing Write accepted %d, want 2 (up to the ceiling)", n)
	}
	if w.Size() != contentCeiling {
		t.Fatalf("Size() = %d, want ceiling %d", w.Size(), contentCeiling)
	}
	if got := buf.String(); got != "abcd" {
		t.Fatalf("buf = %q, want %q", got, "abcd")
	}

	n, err = w.Write([]byte("x"))
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("sticky error %v is not ErrSizeExceeded", err)
	}
	if n != 0 {
		t.Fatalf("sticky Write n = %d, want 0", n)
	}
}
