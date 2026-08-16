package decomp

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecoderUnsupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		compression string
		wantDetail  string
	}{
		{
			name:        "unknown name",
			compression: "lz4",
			wantDetail:  `unknown compression "lz4"`,
		},
		{
			name:        "empty name",
			compression: "",
			wantDetail:  `unknown compression ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			open := Decoder(tt.compression)
			if open == nil {
				t.Fatal("Decoder returned a nil constructor")
			}
			rc, err := open(strings.NewReader(""))
			if rc != nil {
				t.Fatalf("constructor returned ReadCloser %T, want nil", rc)
			}
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("error %v is not ErrUnsupported", err)
			}
			if !strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("error %q does not contain %q", err, tt.wantDetail)
			}
			if strings.Contains(err.Error(), "not supported in this build") {
				t.Fatalf("error %q must not claim the decoder is missing from this build", err)
			}
		})
	}
}

func TestDecoderKnownNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		compression string
	}{
		{name: "none", compression: nameNone},
		{name: "gzip", compression: nameGzip},
		{name: "xz", compression: nameXZ},
		{name: "zstd", compression: nameZstd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			open := Decoder(tt.compression)
			if open == nil {
				t.Fatal("Decoder returned a nil constructor")
			}
		})
	}
}

func TestDecoderRejectsNilConstructorCallOnUnsupported(t *testing.T) {
	t.Parallel()
	_, err := Decoder("brotli")(io.NopCloser(strings.NewReader("")))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error %v is not ErrUnsupported", err)
	}
}
