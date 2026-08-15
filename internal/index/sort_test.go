package index

import "testing"

func TestDescriptorOrderUTF8ByteOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b Selector
		want int
	}{
		{
			name: "uppercase before lowercase",
			a:    Selector{Architecture: "Amd64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			b:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			want: -1,
		},
		{
			name: "shorter prefix first",
			a:    Selector{Architecture: "arm", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			b:    Selector{Architecture: "arm64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			want: -1,
		},
		{
			name: "compression gzip before none",
			a:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "role", Compression: "gzip"},
			b:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			want: -1,
		},
		{
			name: "equal tuples",
			a:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			b:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "role", Compression: "none"},
			want: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := descriptorFromSelector(tc.a)
			b := descriptorFromSelector(tc.b)
			got := descriptorOrder(a, b)
			if sign(got) != sign(tc.want) {
				t.Fatalf("descriptorOrder = %d, want sign of %d", got, tc.want)
			}
		})
	}
}

func TestSortManifests(t *testing.T) {
	t.Parallel()
	zstd := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "zstd",
		"a", testContentDigestA, "1", testManifestDigest3, 1,
	)
	none := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest2, 1,
	)
	gzip := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	manifests := []Descriptor{zstd, none, gzip}
	sortManifests(manifests)
	got := []string{
		manifests[0].Selector().Compression,
		manifests[1].Selector().Compression,
		manifests[2].Selector().Compression,
	}
	want := []string{"gzip", "none", "zstd"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
	if !manifestsInCanonicalOrder(manifests) {
		t.Fatal("sorted manifests reported out of order")
	}
}

func descriptorFromSelector(s Selector) Descriptor {
	return validDescriptor(
		s.Architecture, s.Target, s.Representation, s.Role, s.Compression,
		"a", testContentDigestA, "0", testManifestDigest1, 1,
	)
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}
