package decomp

import (
	"bytes"
	"io"
	"testing"
)

func TestNonePassthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "empty", payload: nil},
		{name: "bytes", payload: []byte("stored file, no transform")},
		{name: "binary", payload: []byte{0x00, 0xFF, 0x80, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rc, err := Decoder(nameNone, DefaultDecoderMaxWindow)(bytes.NewReader(tt.payload))
			if err != nil {
				t.Fatalf("open none: %v", err)
			}
			t.Cleanup(func() { _ = rc.Close() })

			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			want := tt.payload
			if want == nil {
				want = []byte{}
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("got %q, want %q", got, want)
			}
			if err := rc.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
		})
	}
}
