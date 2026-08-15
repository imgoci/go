package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// scanFrame tracks one JSON array or object while walking [json.Decoder] tokens.
type scanFrame struct {
	// object is true when this frame is a JSON object.
	object bool
	// seen holds decoded object keys in this frame.
	seen map[string]struct{}
	// expectKey is true when the next object token must be a key.
	expectKey bool
}

// scanDuplicateKeys reports a duplicate object key in data.
//
// Keys are compared after JSON string decoding via [json.Decoder.Token], so
// `"\u0061"` is the same key as `"a"`. The walk applies at every nesting
// depth. Commas and colons are not tokens; after `{` the decoder yields
// key, value, key, value, then `}`.
func scanDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	stack := make([]scanFrame, 0)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			if len(stack) != 0 {
				return errors.New("jcs: duplicate-key scan: unterminated container")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("jcs: duplicate-key scan: %w", err)
		}
		if err := applyToken(&stack, tok); err != nil {
			return err
		}
	}
}

// applyToken updates stack for one [json.Decoder.Token] value.
func applyToken(stack *[]scanFrame, tok json.Token) error {
	if d, ok := tok.(json.Delim); ok {
		return applyDelim(stack, d)
	}
	return applyValueOrKey(stack, tok)
}

// applyDelim pushes or pops a container frame for `{`, `}`, `[`, or `]`.
func applyDelim(stack *[]scanFrame, d json.Delim) error {
	switch d {
	case '{':
		*stack = append(*stack, scanFrame{
			object:    true,
			seen:      make(map[string]struct{}),
			expectKey: true,
		})
		return nil
	case '[':
		*stack = append(*stack, scanFrame{})
		return nil
	case '}', ']':
		return popFrame(stack)
	default:
		return fmt.Errorf("jcs: duplicate-key scan: unexpected delimiter %q", d)
	}
}

// popFrame removes the current container and marks a completed object value.
func popFrame(stack *[]scanFrame) error {
	if len(*stack) == 0 {
		return errors.New("jcs: duplicate-key scan: unmatched closer")
	}
	*stack = (*stack)[:len(*stack)-1]
	if n := len(*stack); n > 0 && (*stack)[n-1].object {
		(*stack)[n-1].expectKey = true
	}
	return nil
}

// applyValueOrKey records an object key or consumes a primitive value.
func applyValueOrKey(stack *[]scanFrame, tok json.Token) error {
	if len(*stack) == 0 {
		return nil
	}
	top := &(*stack)[len(*stack)-1]
	if !top.object || !top.expectKey {
		if top.object {
			top.expectKey = true
		}
		return nil
	}
	key, ok := tok.(string)
	if !ok {
		return errors.New("jcs: duplicate-key scan: expected string key")
	}
	if _, exists := top.seen[key]; exists {
		return fmt.Errorf("jcs: duplicate key %q", key)
	}
	top.seen[key] = struct{}{}
	top.expectKey = false
	return nil
}
