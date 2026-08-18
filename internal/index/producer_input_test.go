package index

import "testing"

func TestPlaceholderIdentityKeyIncludesUsage(t *testing.T) {
	t.Parallel()
	base := Selector{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Role:           "x-test-file",
	}
	empty := placeholderIdentityKey(base)
	live := placeholderIdentityKey(Selector{
		Architecture:   base.Architecture,
		Target:         base.Target,
		Representation: base.Representation,
		Usage:          "live",
		Role:           base.Role,
	})
	if empty == live {
		t.Fatal("usage must distinguish placeholder content identity")
	}
	got := placeholderIdentityKey(Selector{
		Architecture:   base.Architecture,
		Target:         base.Target,
		Representation: base.Representation,
		Usage:          "install,live",
		Role:           base.Role,
	})
	const want = "amd64/x-test-target/x-test-format/install,live/x-test-file"
	if got != want {
		t.Fatalf("placeholderIdentityKey = %q, want %q", got, want)
	}
}
