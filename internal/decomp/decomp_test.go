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
		notDetail   string
	}{
		{
			name:        "xz reserved for later slice",
			compression: nameXZ,
			wantDetail:  "not supported in this build",
			notDetail:   "unknown compression",
		},
		{
			name:        "zstd reserved for later slice",
			compression: nameZstd,
			wantDetail:  "not supported in this build",
			notDetail:   "unknown compression",
		},
		{
			name:        "unknown name",
			compression: "lz4",
			wantDetail:  `unknown compression "lz4"`,
			notDetail:   "not supported in this build",
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
			if tt.notDetail != "" && strings.Contains(err.Error(), tt.notDetail) {
				t.Fatalf("error %q must not contain %q", err, tt.notDetail)
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
