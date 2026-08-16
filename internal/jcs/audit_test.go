package jcs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Each case records the mechanism that must reject the input: utf8.Valid
// pre-gate, duplicate-key scan, transform error, or byte-compare. The transform
// is not required to error on every violation.

func TestAuditInvalidUTF8RejectedByPreGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
	}{
		{name: "invalid_byte_in_value", input: []byte(`{"a":"` + "\xff" + `"}`)},
		{name: "invalid_byte_in_key", input: []byte("{\"\xff\":1}")},
		{name: "overlong_c0_af", input: []byte(`{"a":"` + "\xc0\xaf" + `"}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if utf8.Valid(tc.input) {
				t.Fatal("fixture must fail utf8.Valid")
			}
			// The transform alone round-trips these bytes.
			got, err := transform(tc.input)
			if err != nil {
				t.Fatalf("transform alone must succeed; gowebpki copies unvalidated bytes: %v", err)
			}
			if !bytes.Equal(got, tc.input) {
				t.Fatalf("transform alone round-trips invalid UTF-8; got %q want %q", got, tc.input)
			}
			err = Verify(tc.input, nil)
			if err == nil {
				t.Fatal("Verify must reject invalid UTF-8")
			}
			if !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Fatalf("Verify must fail at the utf8.Valid pre-gate, got %v", err)
			}
		})
	}
}

func TestAuditDecodedEqualDuplicateKeys(t *testing.T) {
	t.Parallel()

	input := []byte(`{"\u0061":1,"a":2}`)
	if err := scanDuplicateKeys(input); err == nil {
		t.Fatal("dup-scan must reject decoded-equal keys")
	}
	if _, err := transform(input); err == nil {
		t.Fatal("transform must also reject decoded-equal keys")
	}
	if err := Verify(input, nil); err == nil {
		t.Fatal("Verify must reject decoded-equal duplicate keys")
	}
}

func TestAuditLoneSurrogateTransformErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
	}{
		{name: "ud800x", input: []byte(`"\ud800x"`)},
		{name: "udead", input: []byte(`"\udead"`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := transform(tc.input); err == nil {
				t.Fatal("transform must error on a lone surrogate")
			}
			if err := Verify(tc.input, nil); err == nil {
				t.Fatal("Verify must reject a lone surrogate")
			}
		})
	}
}

func TestAuditInvalidSurrogatePairCaughtByByteCompare(t *testing.T) {
	t.Parallel()

	input := []byte(`"\ud800\ud800"`)
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform accepts invalid surrogate pairs silently; got error %v", err)
	}
	if bytes.Equal(got, input) {
		t.Fatal("U+FFFD output can never byte-equal the escape spelling")
	}
	err = Verify(input, nil)
	if err == nil {
		t.Fatal("Verify must reject via byte-compare")
	}
	if !strings.Contains(err.Error(), "not RFC 8785 canonical") {
		t.Fatalf("Verify must fail at byte-compare, got %v", err)
	}
}

func TestAuditOverflowExponentErrors(t *testing.T) {
	t.Parallel()

	input := []byte("1e400")
	if _, err := transform(input); err == nil {
		t.Fatal("transform must error on 1e400")
	}
	if err := Verify(input, nil); err == nil {
		t.Fatal("Verify must reject 1e400")
	}
}

func TestAuditNaNBareTokenErrors(t *testing.T) {
	t.Parallel()

	input := []byte("NaN")
	if _, err := transform(input); err == nil {
		t.Fatal("transform must error on bare NaN")
	}
	if err := Verify(input, nil); err == nil {
		t.Fatal("Verify must reject bare NaN")
	}
}

func TestAuditPastIntegerPrecisionCaughtByByteCompare(t *testing.T) {
	t.Parallel()

	input := []byte("9007199254740993")
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform must not error; precision loss is silent: %v", err)
	}
	if bytes.Equal(got, input) {
		t.Fatalf("transform must round 2^53+1, got %s", got)
	}
	err = Verify(input, nil)
	if err == nil {
		t.Fatal("Verify must reject via byte-compare")
	}
	if !strings.Contains(err.Error(), "not RFC 8785 canonical") {
		t.Fatalf("Verify must fail at byte-compare, got %v", err)
	}
}

func TestAuditMinusZeroCaughtByByteCompare(t *testing.T) {
	t.Parallel()

	input := []byte("-0")
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform must not error on -0: %v", err)
	}
	if !bytes.Equal(got, []byte("0")) {
		t.Fatalf("transform serializes -0 as 0, got %s", got)
	}
	err = Verify(input, nil)
	if err == nil {
		t.Fatal("Verify must reject -0 via byte-compare")
	}
	if !strings.Contains(err.Error(), "not RFC 8785 canonical") {
		t.Fatalf("Verify must fail at byte-compare, got %v", err)
	}
}

func TestAuditExponentSpellingCanonicalizesToInteger(t *testing.T) {
	t.Parallel()

	input := []byte("1e2")
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if !bytes.Equal(got, []byte("100")) {
		t.Fatalf("1e2 must canonicalize to 100, got %s", got)
	}
	err = Verify(input, nil)
	if err == nil {
		t.Fatal("non-canonical spelling 1e2 must be rejected by byte-compare")
	}
	if !strings.Contains(err.Error(), "not RFC 8785 canonical") {
		t.Fatalf("Verify must fail at byte-compare, got %v", err)
	}
}

func TestAuditGrammarHoleWhitespaceInsideLiterals(t *testing.T) {
	t.Parallel()

	input := []byte("[1 2]")
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform accepts [1 2] as a grammar hole: %v", err)
	}
	if !bytes.Equal(got, []byte("[12]")) {
		t.Fatalf("transform absorbs the space and emits [12], got %s", got)
	}
	if json.Valid(input) {
		t.Fatal("encoding/json must reject [1 2]; that is why Decode precedes Verify")
	}
	// [1 2] does not survive Decode. If it reached the transform, output != input
	// would still reject it.
	if bytes.Equal(got, input) {
		t.Fatal("output must differ from input")
	}
	if utf8.Valid(input) && !bytes.Equal(got, input) {
		return
	}
	t.Fatal("audit framing: transform errors or output != input")
}
