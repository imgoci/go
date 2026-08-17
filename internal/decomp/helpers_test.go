package decomp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

const (
	// decodeBombPayload is a high-ratio zeros buffer used to reach the
	// content-size ceiling mid-stream.
	decodeBombPayload = 1 << 20
	// decodeBombCeiling is well below decodeBombPayload so Copy aborts
	// before the decoder finishes.
	decodeBombCeiling = 4096
)

func assertDecodeBomb(t *testing.T, name string, stored []byte) {
	t.Helper()
	rc, err := Decoder(name, DefaultDecoderMaxWindow)(onlyReader{r: bytes.NewReader(stored)})
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	var buf bytes.Buffer
	w := NewCountingHashWriter(&buf, decodeBombCeiling)
	_, err = io.Copy(w, rc)
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("error %v is not ErrSizeExceeded", err)
	}
	if w.Size() != decodeBombCeiling {
		t.Fatalf("Size() = %d, want ceiling %d", w.Size(), decodeBombCeiling)
	}
	if buf.Len() != decodeBombCeiling {
		t.Fatalf("buf len %d, want ceiling %d", buf.Len(), decodeBombCeiling)
	}
}
