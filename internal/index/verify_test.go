package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCanonicalRejectsPrettyJSON(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, "pass", "minimal.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = VerifyCanonical(raw)
	if err == nil {
		t.Fatal("pretty-printed fixture passed rule 10")
	}
	if !strings.Contains(err.Error(), "spec §6 rule 10") {
		t.Fatalf("error %v does not name rule 10", err)
	}
}

func TestVerifyCanonicalRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	err := VerifyCanonical([]byte{0xff, '{', '}'})
	if err == nil {
		t.Fatal("VerifyCanonical accepted invalid UTF-8")
	}
	if !strings.Contains(err.Error(), "spec §6 rule 10") {
		t.Fatalf("error %v does not name rule 10", err)
	}
}

func TestVerifyCanonicalAcceptsBuildOutput(t *testing.T) {
	t.Parallel()
	raw, err := Build(minimalModel())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCanonical(raw); err != nil {
		t.Fatal(err)
	}
}
