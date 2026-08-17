package decomp

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

const boundedExact = 8

// neverEOFReader always fills p and never returns EOF. A correct
// BoundedReader must abort at exact+1 without waiting for the stream to end.
type neverEOFReader struct{}

func (neverEOFReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// phaseReader returns first on the first Read, then rest. The first Read
// delivers exactly `exact` bytes and the next Read is the probe, which
// separates the exact-limit case from the remaining+1 excess-during-read path.
type phaseReader struct {
	first   []byte
	rest    []byte
	phase   int
	restErr error
}

func (p *phaseReader) Read(b []byte) (int, error) {
	if p.phase == 0 {
		p.phase = 1
		n := copy(b, p.first)
		return n, nil
	}
	n := copy(b, p.rest)
	p.rest = p.rest[n:]
	if p.restErr != nil {
		return n, p.restErr
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func TestBoundedReaderExcessDuringReadNeverEOF(t *testing.T) {
	t.Parallel()

	br := NewBoundedReader(neverEOFReader{}, boundedExact)
	buf := make([]byte, boundedExact*4)
	n, err := br.Read(buf)
	if !errors.Is(err, ErrSizeExceeded) {
		t.Fatalf("error %v is not ErrSizeExceeded", err)
	}
	if n != boundedExact {
		t.Fatalf("accepted %d bytes, want %d (the extra byte is not delivered)", n, boundedExact)
	}
}

func TestBoundedReaderExactLimitProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rest    []byte
		restErr error
		wantErr error
	}{
		{
			name:    "success underlying returns 0 EOF",
			rest:    nil,
			restErr: io.EOF,
			wantErr: nil,
		},
		{
			name:    "failure more data",
			rest:    []byte{0xAA},
			wantErr: ErrSizeExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			first := bytes.Repeat([]byte{'a'}, boundedExact)
			src := &phaseReader{first: first, rest: append([]byte{}, tt.rest...), restErr: tt.restErr}
			br := NewBoundedReader(src, boundedExact)

			got, err := io.ReadAll(br)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error %v is not %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if !bytes.Equal(got, first) {
					t.Fatalf("got %q, want %q", got, first)
				}
			}
		})
	}
}

func TestBoundedReaderShortStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "one byte short", payload: bytes.Repeat([]byte{'a'}, boundedExact-1)},
		{name: "well short", payload: []byte("short")},
		{name: "empty", payload: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			br := NewBoundedReader(bytes.NewReader(tt.payload), boundedExact)
			got, err := io.ReadAll(br)
			if !errors.Is(err, ErrSizeMismatch) {
				t.Fatalf("error %v is not ErrSizeMismatch", err)
			}
			if errors.Is(err, ErrSizeExceeded) {
				t.Fatal("underrun was reported as ErrSizeExceeded")
			}
			if !bytes.Equal(got, tt.payload) && len(tt.payload) > 0 {
				t.Fatalf("got %q, want %q", got, tt.payload)
			}

			if _, err := br.Read(make([]byte, 1)); !errors.Is(err, ErrSizeMismatch) {
				t.Fatalf("sticky error %v is not ErrSizeMismatch", err)
			}
		})
	}
}

func TestBoundedReaderPropagatesUnderlyingError(t *testing.T) {
	t.Parallel()

	mismatch := errors.New("digest mismatch")
	first := bytes.Repeat([]byte{'b'}, boundedExact)
	src := &phaseReader{first: first, restErr: mismatch}
	br := NewBoundedReader(src, boundedExact)
	_, err := io.ReadAll(br)
	if !errors.Is(err, mismatch) {
		t.Fatalf("error %v is not the underlying digest mismatch", err)
	}
	if errors.Is(err, ErrSizeExceeded) {
		t.Fatal("digest mismatch was rewritten as ErrSizeExceeded")
	}
}
