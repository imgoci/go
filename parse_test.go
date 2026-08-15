package imgoci

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseIndexCanonicalPass(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "canonical", "pass", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no canonical pass fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			assertParseIndexCanonicalPass(t, path)
		})
	}
}

func assertParseIndexCanonicalPass(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatalf("ParseIndex(%s): %v", path, err)
	}
	if idx == nil {
		t.Fatal("expected index")
	}
	if idx.Digest() == "" {
		t.Fatal("expected digest of input bytes")
	}
	if idx.Name() == "" {
		t.Fatal("expected io.imgoci.name")
	}
	if got := idx.Entries(); len(got) == 0 {
		t.Fatal("expected at least one entry")
	}
}

func TestParseIndexCanonicalFail(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "canonical", "fail", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no canonical fail fixtures")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			idx, err := ParseIndex(b)
			if err == nil {
				t.Fatalf("ParseIndex(%s): accepted invalid index", path)
			}
			if !errors.Is(err, ErrInvalidIndex) {
				t.Fatalf("ParseIndex(%s): error %v is not ErrInvalidIndex", path, err)
			}
			if idx != nil {
				t.Fatalf("ParseIndex(%s): expected nil index on failure", path)
			}
		})
	}
}

func TestIndexAccessorsCopy(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("testdata", "canonical", "pass", "minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	ann := idx.Annotations()
	ann["io.imgoci.name"] = "mutated"
	if idx.Name() != "example" {
		t.Fatalf("mutating Annotations copy changed Name: %q", idx.Name())
	}
	entries := idx.Entries()
	entries[0].Annotations["io.imgoci.filename"] = "mutated"
	if idx.Entries()[0].Filename != "a" {
		t.Fatal("mutating Entries copy changed stored filename")
	}
	if idx.Entries()[0].Annotations["io.imgoci.filename"] != "a" {
		t.Fatal("mutating Entries copy changed stored annotations")
	}
}
