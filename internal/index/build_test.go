package index

import (
	"testing"

	"github.com/opencontainers/go-digest"
)

func TestBuildSelfOracle(t *testing.T) {
	t.Parallel()
	raw, err := Build(minimalModel())
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := Validate(v); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := VerifyCanonical(raw); err != nil {
		t.Fatalf("VerifyCanonical: %v", err)
	}
}

func TestBuildSortsBeforeEncode(t *testing.T) {
	t.Parallel()
	m := minimalModel()
	m.Entries = []ModelEntry{
		modelEntry("zstd", testManifestDigest3),
		modelEntry("none", testManifestDigest2),
		modelEntry("gzip", testManifestDigest1),
	}
	raw, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{
		v.Manifests[0].Selector().Compression,
		v.Manifests[1].Selector().Compression,
		v.Manifests[2].Selector().Compression,
	}
	want := []string{"gzip", "none", "zstd"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("encoded order %v, want %v", got, want)
		}
	}
}

func TestBuildNilModel(t *testing.T) {
	t.Parallel()
	if _, err := Build(nil); err == nil {
		t.Fatal("Build(nil) succeeded")
	}
}

func TestBuildRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	m := minimalModel()
	m.Name = string([]byte{0xff})
	if _, err := Build(m); err == nil {
		t.Fatal("Build accepted invalid UTF-8")
	}
}

func minimalModel() *Model {
	return &Model{
		Name:    "example",
		Version: "1",
		Entries: []ModelEntry{modelEntry("none", testManifestDigest1)},
	}
}

func modelEntry(compression, manifestDigest string) ModelEntry {
	return ModelEntry{
		Digest: digest.Digest(manifestDigest),
		Size:   1,
		Selector: Selector{
			Architecture:   "amd64",
			Target:         "x-test-target",
			Representation: "x-test-format",
			Role:           "x-test-file",
			Compression:    compression,
		},
		ContentDigest: digest.Digest(testContentDigestA),
		ContentSize:   0,
		Filename:      "a",
	}
}
