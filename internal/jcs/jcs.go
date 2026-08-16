package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	webpki "github.com/gowebpki/jcs"
)

// Verify reports whether original is already RFC 8785 canonical JSON.
//
// parsed is the value produced by decoding original. It is not re-encoded; the
// parameter records that grammar decoding must run before verification. The
// transform runs on original so that [utf8.Valid] remains a pre-gate:
// re-encoding parsed would hide that the pinned transform copies invalid UTF-8
// unvalidated.
func Verify(original []byte, parsed any) error {
	_ = parsed
	if !utf8.Valid(original) {
		return errors.New("jcs: input is not valid UTF-8")
	}
	if err := scanDuplicateKeys(original); err != nil {
		return err
	}
	canonical, err := transform(original)
	if err != nil {
		return fmt.Errorf("jcs: transform: %w", err)
	}
	if !bytes.Equal(canonical, original) {
		return errors.New("jcs: input is not RFC 8785 canonical")
	}
	return nil
}

// Encode returns the RFC 8785 canonical JSON encoding of v.
//
// Caller strings must already be valid UTF-8 ([utf8.ValidString]). Encode is
// [json.Marshal] followed by the same transform [Verify] uses.
func Encode(v any) ([]byte, error) {
	marshaled, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jcs: marshal: %w", err)
	}
	canonical, err := transform(marshaled)
	if err != nil {
		return nil, fmt.Errorf("jcs: transform: %w", err)
	}
	return canonical, nil
}

// transform calls the pinned RFC 8785 implementation.
func transform(original []byte) ([]byte, error) {
	return webpki.Transform(original)
}
