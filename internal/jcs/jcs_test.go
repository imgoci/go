package jcs

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestVerifyAcceptsCanonicalInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "true", input: "true"},
		{name: "false", input: "false"},
		{name: "null", input: "null"},
		{name: "zero", input: "0"},
		{name: "negative", input: "-1"},
		{name: "fractional", input: "0.5"},
		{name: "canonical_exponent", input: "1e+30"},
		{name: "empty_object", input: "{}"},
		{name: "empty_array", input: "[]"},
		{name: "nested", input: `{"a":[1,{"b":null}]}`},
		{name: "minimal_escapes", input: `"€$\u000f\nA'B\"\\\\\"/"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			original := []byte(tc.input)
			if err := Verify(original, mustDecode(t, original)); err != nil {
				t.Fatalf("Verify(%s): %v", tc.input, err)
			}
		})
	}
}

func TestVerifyAcceptsUTF16KeySort(t *testing.T) {
	t.Parallel()

	canonical, err := os.ReadFile(filepath.Join(corpusDir(t), "output", "rfc8785-sorting.json"))
	if err != nil {
		t.Fatalf("read sorting output: %v", err)
	}
	if err := Verify(canonical, mustDecode(t, canonical)); err != nil {
		t.Fatalf("Verify(RFC 8785 sorting): %v", err)
	}
}

func TestEncodeOutputPassesVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
	}{
		{name: "true", value: true},
		{name: "false", value: false},
		{name: "null", value: nil},
		{name: "negative", value: -2},
		{name: "fractional", value: 0.25},
		{name: "nested", value: map[string]any{"z": []any{true, nil}, "a": 1}},
		{name: "escapes", value: "line\nquote\""},
		{name: "unicode_keys", value: map[string]any{"ö": 1, "€": 2, "a": 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := Encode(tc.value)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if err := Verify(encoded, mustDecode(t, encoded)); err != nil {
				t.Fatalf("Verify(Encode(%s))=%q: %v", tc.name, encoded, err)
			}
		})
	}
}

func TestCorpusFiles(t *testing.T) {
	t.Parallel()

	inputDir := filepath.Join(corpusDir(t), "input")
	outputDir := filepath.Join(corpusDir(t), "output")
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatalf("read corpus input: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("corpus input directory is empty")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkCorpusPair(t, filepath.Join(inputDir, name), filepath.Join(outputDir, name))
		})
	}
}

func checkCorpusPair(t *testing.T, inputPath, outputPath string) {
	t.Helper()
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	want, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got, err := transform(input)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("transform mismatch\n got %s\nwant %s", got, want)
	}
	if verifyErr := Verify(want, mustDecode(t, want)); verifyErr != nil {
		t.Fatalf("Verify(canonical output): %v", verifyErr)
	}
	encoded, err := Encode(mustDecode(t, want))
	if err != nil {
		t.Fatalf("Encode(parsed output): %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("Encode(parsed) mismatch\n got %s\nwant %s", encoded, want)
	}
	if verifyErr := Verify(encoded, mustDecode(t, encoded)); verifyErr != nil {
		t.Fatalf("Verify(Encode(parsed)): %v", verifyErr)
	}
}

func TestRFC8785AppendixBNumbers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(corpusDir(t), "numbers", "rfc8785-appendix-b.txt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read appendix B numbers: %v", err)
	}
	for i, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		hexBits, expected, ok := bytes.Cut(line, []byte(","))
		if !ok {
			t.Fatalf("line %d: missing comma: %s", i+1, line)
		}
		t.Run(string(hexBits), func(t *testing.T) {
			t.Parallel()
			bits, err := strconv.ParseUint(string(hexBits), 16, 64)
			if err != nil {
				t.Fatalf("parse hex: %v", err)
			}
			got, err := Encode(math.Float64frombits(bits))
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("Encode bits %s: got %s want %s", hexBits, got, expected)
			}
			if err := Verify(got, mustDecode(t, got)); err != nil {
				t.Fatalf("Verify(Encode): %v", err)
			}
		})
	}
}

func mustDecode(t *testing.T, original []byte) any {
	t.Helper()
	var parsed any
	if err := json.Unmarshal(original, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return parsed
}

func corpusDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "jcs")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("corpus %s: %v", dir, err)
	}
	return dir
}
