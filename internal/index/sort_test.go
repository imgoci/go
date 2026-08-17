package index

import "testing"

// TestDescriptorOrderUTF8ByteOrder covers spec §9: manifests sort by
// (architecture, target, representation, usage, role, compression), each field
// compared by ascending UTF-8 byte order.
//
// Cases come in two shapes. A "decides" case holds every earlier component
// equal, so the named component alone fixes the order. A "dominates" case gives
// the earlier component the smaller value while the later component would order
// the pair the other way.
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
			name: "target decides when architecture is equal",
			a: Selector{
				Architecture:   "amd64",
				Target:         "aws",
				Representation: "r",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "azure",
				Representation: "r",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "architecture dominates target",
			a: Selector{
				Architecture:   "amd64",
				Target:         "zzz",
				Representation: "r",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "arm64",
				Target:         "aws",
				Representation: "r",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "representation decides when architecture and target are equal",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "iso",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "qcow2",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "target dominates representation",
			a: Selector{
				Architecture:   "amd64",
				Target:         "aws",
				Representation: "qcow2",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "azure",
				Representation: "iso",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "role decides when architecture, target, and representation are equal",
			a:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "disk", Compression: "none"},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Role:           "kernel",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "representation dominates role",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "iso",
				Role:           "kernel",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "qcow2",
				Role:           "disk",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "usage decides when architecture, target, and representation are equal",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "install",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "live",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "empty usage sorts before any present usage",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "live",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "usage decides before role",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "",
				Role:           "kernel",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Usage:          "live",
				Role:           "disk",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "representation dominates usage",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "iso",
				Usage:          "live",
				Role:           "role",
				Compression:    "none",
			},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "qcow2",
				Usage:          "install",
				Role:           "role",
				Compression:    "none",
			},
			want: -1,
		},
		{
			name: "role dominates compression",
			a:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "disk", Compression: "zstd"},
			b: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Role:           "kernel",
				Compression:    "gzip",
			},
			want: -1,
		},
		{
			name: "role compares by byte order not case-insensitively",
			a: Selector{
				Architecture:   "amd64",
				Target:         "t",
				Representation: "r",
				Role:           "Kernel",
				Compression:    "none",
			},
			b:    Selector{Architecture: "amd64", Target: "t", Representation: "r", Role: "disk", Compression: "none"},
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
	d := validDescriptor(
		s.Architecture, s.Target, s.Representation, s.Role, s.Compression,
		"a", testContentDigestA, "0", testManifestDigest1, 1,
	)
	if s.Usage != "" {
		d.Annotations[AnnotationUsage] = s.Usage
	}
	return d
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
