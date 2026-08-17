package index

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

// goldenIndexPath holds the independent byte oracle for [Build].
//
// testdata/canonical/pass/minimal.json is the spec-derived canonical corpus
// fixture at the module root, shared with the root-package parse tests. It is
// committed compact with no trailing newline. Comparing [Build] output against
// it byte for byte is the primary oracle for the producer: unlike the
// [Decode]/[Validate]/[VerifyCanonical] round trip, it cannot pass when the
// producer member set or the encoder drifts, because the fixture does not move
// with this package's code.
const goldenIndexPath = "../../testdata/canonical/pass/minimal.json"

// TestBuildGoldenBytes pins [Build] output to the checked-in canonical fixture.
func TestBuildGoldenBytes(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(goldenIndexPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(want); n == 0 || want[n-1] == '\n' {
		t.Fatalf("golden %s must be non-empty and have no trailing newline", goldenIndexPath)
	}

	// Built from the fixture's own annotation values, not from minimalModel,
	// so that editing the shared helper cannot silently retarget the golden.
	got, err := Build(&Model{
		Name:    "example",
		Version: "1",
		Entries: []ModelEntry{{
			Digest: digest.Digest(testManifestDigest1),
			Size:   1,
			Selector: Selector{
				Architecture:   "amd64",
				Target:         "x-test-target",
				Representation: "x-test-format",
				Role:           "x-test-file",
				Compression:    "none",
			},
			ContentDigest: digest.Digest(testContentDigestA),
			ContentSize:   0,
			Filename:      "a",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Build bytes differ from %s\ngot:  %s\nwant: %s", goldenIndexPath, got, want)
	}
}

// TestBuildMaxDescriptorSizeToken covers spec §9:784-786: the spec §5.2
// descriptor-size limit exists so every number stays exactly representable
// under RFC 8785. The largest permitted size must therefore serialize as the
// integer token itself, with no float rounding and no exponent form.
func TestBuildMaxDescriptorSizeToken(t *testing.T) {
	t.Parallel()
	m := minimalModel()
	m.Entries[0].Size = maxManifestSize
	raw, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	const want = `"size":9007199254740991`
	if !bytes.Contains(raw, []byte(want)) {
		t.Fatalf("encoded index does not contain %s\ngot: %s", want, raw)
	}
	tokens := sizeTokens(string(raw))
	if len(tokens) == 0 {
		t.Fatalf("found no size member to inspect in %s", raw)
	}
	for _, tok := range tokens {
		if strings.ContainsAny(tok, ".eE+-") {
			t.Fatalf("size token %q is not a plain integer literal", tok)
		}
	}

	v, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v.Manifests[0].Size != maxManifestSize {
		t.Fatalf("decoded size %d, want %d", v.Manifests[0].Size, maxManifestSize)
	}
}

// sizeTokens returns the JSON number literal following every `"size":` member
// in s.
func sizeTokens(s string) []string {
	const member = `"size":`
	var out []string
	for rest := s; ; {
		i := strings.Index(rest, member)
		if i < 0 {
			return out
		}
		rest = rest[i+len(member):]
		end := strings.IndexAny(rest, ",}]")
		if end < 0 {
			end = len(rest)
		}
		out = append(out, rest[:end])
		rest = rest[end:]
	}
}

// TestBuildRefusesInvalidModel covers spec §9:791-792: a producer must validate
// an index against §6 rules 1 through 8 before encoding it. Each case violates
// one rule in the 4-8 range, and [Build] must return nil bytes plus that rule's
// error rather than encoding anything.
func TestBuildRefusesInvalidModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// model returns producer input that violates exactly one §6 rule.
		model func() *Model
		rule  int
	}{
		{
			name: "rule 4 missing required role",
			model: func() *Model {
				m := minimalModel()
				e := m.Entries[0]
				e.Selector.Target = "metal"
				e.Selector.Representation = "linux-netboot"
				e.Selector.Role = "rootfs"
				m.Entries = []ModelEntry{e}
				return m
			},
			rule: specRuleRoles,
		},
		{
			name: "rule 5 duplicate selector tuple",
			model: func() *Model {
				m := minimalModel()
				m.Entries = []ModelEntry{
					modelEntry("none", testManifestDigest1),
					modelEntry("none", testManifestDigest2),
				}
				return m
			},
			rule: specRuleSelector,
		},
		{
			name: "rule 6 alternatives disagree on content digest",
			model: func() *Model {
				m := minimalModel()
				other := modelEntry("gzip", testManifestDigest2)
				other.ContentDigest = digest.Digest(testContentDigestB)
				m.Entries = []ModelEntry{modelEntry("none", testManifestDigest1), other}
				return m
			},
			rule: specRuleFileIdentity,
		},
		{
			name: "rule 7 roles share a filename",
			model: func() *Model {
				m := minimalModel()
				other := modelEntry("gzip", testManifestDigest2)
				other.Selector.Role = "x-test-other"
				m.Entries = []ModelEntry{modelEntry("none", testManifestDigest1), other}
				return m
			},
			rule: specRuleFilename,
		},
		{
			name: "rule 8 shared manifest digest disagrees on size",
			model: func() *Model {
				m := minimalModel()
				other := modelEntry("none", testManifestDigest1)
				other.Selector.Architecture = "arm64"
				other.Size = 2
				m.Entries = []ModelEntry{modelEntry("none", testManifestDigest1), other}
				return m
			},
			rule: specRuleSharedManifest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := Build(tc.model())
			if raw != nil {
				t.Fatalf("Build returned %d bytes for an invalid model", len(raw))
			}
			assertRule(t, err, tc.rule)
		})
	}
}

// TestBuildDeterministic covers spec §9:809-810: the same fields, descriptors,
// and annotations must produce the same index bytes and the same digest. The
// model carries extra annotation maps on purpose, because Go randomizes map
// iteration order and an encoder that leaked that order would fail here.
func TestBuildDeterministic(t *testing.T) {
	t.Parallel()
	model := func() *Model {
		gzip := modelEntry("gzip", testManifestDigest1)
		gzip.Annotations = map[string]string{
			"x-test-a": "1", "x-test-b": "2", "x-test-c": "3", "x-test-d": "4",
		}
		none := modelEntry("none", testManifestDigest2)
		zstd := modelEntry("zstd", testManifestDigest3)
		return &Model{
			Name:    "example",
			Version: "1",
			Annotations: map[string]string{
				"x-test-p": "1", "x-test-q": "2", "x-test-r": "3", "x-test-s": "4",
			},
			Entries: []ModelEntry{zstd, none, gzip},
		}
	}
	first, err := Build(model())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(model())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("unstable encoding\nfirst:  %s\nsecond: %s", first, second)
	}
	if got, want := digest.FromBytes(second), digest.FromBytes(first); got != want {
		t.Fatalf("digest %s, want %s", got, want)
	}
}

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
