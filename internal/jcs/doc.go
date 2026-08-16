// Package jcs is the RFC 8785 canonicalization gate used by imgoci rule 10.
//
// [Verify] and [Encode] are the only entry points. Both wrap
// github.com/gowebpki/jcs v1.0.1; no other package in this module imports it.
//
// # Verify order
//
// [Verify] implements a fixed three-step check of original bytes:
//
//  1. [utf8.Valid] on the raw input. gowebpki/jcs copies bytes at or above 0x20
//     unvalidated (parseQuotedString / decorateString), so invalid UTF-8
//     round-trips byte-identically through the transform. [encoding/json]
//     Unmarshal, Valid, and [json.Decoder.Token] accept invalid UTF-8 and
//     substitute U+FFFD; Marshal emits \ufffd.
//  2. A token-level duplicate-key scan that compares keys after JSON string
//     decoding, so `"\u0061"` duplicates `"a"`. The scan walks every nesting
//     depth. gowebpki/jcs already rejects duplicates; the scan is a redundant
//     check that survives a dependency swap.
//  3. jcs.Transform of original, then a byte-compare with the input.
//     Non-canonical spellings (whitespace, 1e2, non-minimal escapes, unsorted
//     keys) diverge and are rejected. The parsed argument records that grammar
//     decoding must already have succeeded: the transform is not a JSON grammar
//     validator ([1 2] becomes [12]).
//
// For every non-canonical or non-I-JSON input that survives the [utf8.Valid]
// pre-gate and Decode, the transform errors or produces output that is not
// byte-equal to the input. Invalid surrogate pairs and precision loss are
// rejected by the byte-compare, not by a transform error.
//
// # Encode
//
// [Encode] is [json.Marshal] of v followed by the same transform. Caller
// strings must already satisfy [utf8.ValidString]; Marshal otherwise
// substitutes U+FFFD.
package jcs
