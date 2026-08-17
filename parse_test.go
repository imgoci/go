package imgoci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imgoci/go/internal/index"
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

// canonicalPhase names the [ParseIndex] stage that must reject a fixture in
// testdata/canonical/fail. Every stage earlier than that one must accept the
// fixture: a fixture caught before the stage it is named for no longer
// exercises that rule, and the earlier gate hides the regression.
type canonicalPhase int

const (
	// phaseDecode expects [index.Decode] to reject the fixture.
	phaseDecode canonicalPhase = iota
	// phaseValidate expects [index.Validate] to reject the fixture with
	// [index.ErrRule].
	phaseValidate
	// phaseVerifyCanonical expects [index.VerifyCanonical] to reject the
	// fixture.
	phaseVerifyCanonical
)

// String names the phase for failure messages.
func (p canonicalPhase) String() string {
	switch p {
	case phaseDecode:
		return "index.Decode"
	case phaseValidate:
		return "index.Validate"
	case phaseVerifyCanonical:
		return "index.VerifyCanonical"
	default:
		return fmt.Sprintf("canonicalPhase(%d)", int(p))
	}
}

// canonicalFailPhases maps every fixture in testdata/canonical/fail to the
// stage that must reject it. The map and the directory must match exactly;
// [TestParseIndexCanonicalFail] fails on a fixture missing from the map and on
// a map entry with no fixture on disk.
func canonicalFailPhases() map[string]canonicalPhase {
	return map[string]canonicalPhase{
		// Grammar gates: invalid UTF-8 and duplicate object keys, compared
		// after JSON string decoding.
		"duplicate-keys-raw.json":     phaseDecode,
		"duplicate-keys-decoded.json": phaseDecode,
		"invalid-utf8-key.json":       phaseDecode,
		"invalid-utf8-value.json":     phaseDecode,

		// Consumer rules 3–5 and 9: value-layer defects whose bytes are
		// already RFC 8785 canonical, so rule 10 cannot mask them.
		"canonical-wrong-descriptor-order.json":       phaseValidate,
		"canonical-wrong-usage-descriptor-order.json": phaseValidate,
		"duplicate-usage-value.json":                  phaseValidate,
		"noncanonical-usage-order.json":               phaseValidate,
		"present-empty-usage-value.json":              phaseValidate,
		"install-offline-without-install.json":        phaseValidate,
		"duplicate-six-field-selector.json":           phaseValidate,
		// Consumer rule 10: valid, well-formed indexes whose only defect is
		// that RFC 8785 would spell the bytes differently.
		"pretty-printed.json":     phaseVerifyCanonical,
		"unsorted-keys.json":      phaseVerifyCanonical,
		"exponent-1e0.json":       phaseVerifyCanonical,
		"exponent-1e2.json":       phaseVerifyCanonical,
		"nonminimal-escapes.json": phaseVerifyCanonical,
		"lone-surrogate.json":     phaseVerifyCanonical,
	}
}

func TestParseIndexCanonicalFail(t *testing.T) {
	t.Parallel()
	want := canonicalFailPhases()
	files, err := filepath.Glob(filepath.Join("testdata", "canonical", "fail", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no canonical fail fixtures")
	}
	found := make(map[string]struct{}, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		found[name] = struct{}{}
		phase, ok := want[name]
		if !ok {
			t.Errorf("fixture %s is not named by canonicalFailPhases: add it with the phase it proves", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertParseIndexCanonicalFail(t, path, phase)
		})
	}
	for name := range want {
		if _, ok := found[name]; !ok {
			t.Errorf("canonicalFailPhases names %s but no such fixture exists", name)
		}
	}
}

// assertParseIndexCanonicalFail requires path to survive every phase before
// phase, to be rejected by phase, and to be rejected by [ParseIndex] with
// [ErrInvalidIndex] and no index.
func assertParseIndexCanonicalFail(t *testing.T, path string, phase canonicalPhase) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value := assertCanonicalDecodePhase(t, path, b, phase)
	if phase != phaseDecode {
		assertCanonicalValidatePhase(t, path, value, phase)
	}
	assertCanonicalVerifyPhase(t, path, b, phase)
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
}

// assertCanonicalDecodePhase runs [index.Decode] and requires it to reject the
// fixture only when phase is [phaseDecode]. It returns the decoded value, which
// is nil exactly when Decode failed as expected.
func assertCanonicalDecodePhase(t *testing.T, path string, b []byte, phase canonicalPhase) *index.Value {
	t.Helper()
	value, err := index.Decode(b)
	if phase == phaseDecode {
		if err == nil {
			t.Fatalf("index.Decode(%s): accepted bytes the fixture exists to reject", path)
		}
		return nil
	}
	if err != nil {
		t.Fatalf(
			"index.Decode(%s): rejected the fixture (%v); it is caught before the %s phase it claims to prove",
			path,
			err,
			phase,
		)
	}
	return value
}

// assertCanonicalValidatePhase runs [index.Validate] and requires it to reject
// the fixture with [index.ErrRule] only when phase is [phaseValidate].
func assertCanonicalValidatePhase(t *testing.T, path string, value *index.Value, phase canonicalPhase) {
	t.Helper()
	err := index.Validate(value)
	if phase == phaseValidate {
		assertCanonicalRuleRejection(t, path, err)

		return
	}
	if err != nil {
		t.Fatalf(
			"index.Validate(%s): rejected the fixture (%v); it is caught before the %s phase it claims to prove",
			path,
			err,
			phase,
		)
	}
}

// assertCanonicalRuleRejection requires err to be the spec §6 rule failure the
// fixture is named for, so a fixture cannot pass by failing the wrong rule.
func assertCanonicalRuleRejection(t *testing.T, path string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("index.Validate(%s): accepted an index the fixture exists to reject", path)
	}
	if !errors.Is(err, index.ErrRule) {
		t.Fatalf("index.Validate(%s): error %v is not index.ErrRule", path, err)
	}
	rule := canonicalFailRule(filepath.Base(path))
	if rule == 0 {
		return
	}
	want := fmt.Sprintf("spec §6 rule %d", rule)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("index.Validate(%s): error %v does not name %s", path, err, want)
	}
}

// canonicalFailRule returns the spec §6 rule a Validate-phase fixture must
// name. Zero means the fixture is not pinned to a numbered rule.
func canonicalFailRule(name string) int {
	switch name {
	case "duplicate-usage-value.json", "noncanonical-usage-order.json", "present-empty-usage-value.json":
		return 3
	case "install-offline-without-install.json":
		return 4
	case "duplicate-six-field-selector.json":
		return 5
	case "canonical-wrong-descriptor-order.json", "canonical-wrong-usage-descriptor-order.json":
		return 9
	default:
		return 0
	}
}

// assertCanonicalVerifyPhase runs [index.VerifyCanonical] and requires it to
// reject the fixture when phase is [phaseVerifyCanonical]. A fixture named for
// an earlier stage may still be RFC 8785 canonical, so it is not asserted on
// here.
func assertCanonicalVerifyPhase(t *testing.T, path string, b []byte, phase canonicalPhase) {
	t.Helper()
	if phase != phaseVerifyCanonical {
		return
	}
	if err := index.VerifyCanonical(b); err == nil {
		t.Fatalf("index.VerifyCanonical(%s): accepted non-canonical bytes", path)
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
