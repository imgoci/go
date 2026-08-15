package jcs

import (
	"strings"
	"testing"
)

func TestScanDuplicateKeysRejectsDecodedEqualKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "unicode_escape_vs_literal", input: `{"\u0061":1,"a":2}`},
		{name: "identical_keys", input: `{"a":1,"a":2}`},
		{name: "nested_object", input: `{"outer":{"x":1,"x":2}}`},
		{name: "array_of_objects", input: `[{"ok":1},{"k":1,"k":2}]`},
		{name: "deep_nesting", input: `{"a":{"b":[{"c":1,"c":2}]}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := scanDuplicateKeys([]byte(tc.input))
			if err == nil {
				t.Fatal("scanDuplicateKeys: expected duplicate-key error")
			}
			if !strings.Contains(err.Error(), "duplicate key") {
				t.Fatalf("scanDuplicateKeys: got %v, want duplicate key", err)
			}
		})
	}
}

func TestScanDuplicateKeysAcceptsUniqueKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty_object", input: `{}`},
		{name: "distinct_keys", input: `{"a":1,"b":2}`},
		{name: "same_key_different_objects", input: `[{"a":1},{"a":2}]`},
		{name: "nested_unique", input: `{"a":{"a":1},"b":[2]}`},
		{name: "top_level_array", input: `[1,2,3]`},
		{name: "top_level_primitive", input: `true`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := scanDuplicateKeys([]byte(tc.input)); err != nil {
				t.Fatalf("scanDuplicateKeys(%s): %v", tc.input, err)
			}
		})
	}
}

func TestScanDuplicateKeysReportsDecoderErrors(t *testing.T) {
	t.Parallel()

	if err := scanDuplicateKeys([]byte(`{`)); err == nil {
		t.Fatal("expected error for unterminated object")
	}
}
