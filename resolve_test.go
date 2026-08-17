package imgoci

import (
	"errors"
	"slices"
	"testing"
)

func TestResolveAtomicLastRoleUnsatisfiable(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("amd64", "incus", "incus-vm", "disk", "none", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "metadata", "zstd", "application/vnd.bigoci.file.v1"),
	)
	got, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "incus",
		Representation: "incus-vm",
		Roles:          []string{"disk", "metadata"},
		Compressions:   []string{"none"},
	})
	if err == nil {
		t.Fatal("expected error when the last role has no accepted compression")
	}
	if got != nil {
		t.Fatalf("atomic resolve must return a nil result, got %#v", got)
	}
}

func TestResolveDefaultRoles(t *testing.T) {
	t.Parallel()
	netboot := testIndex(
		testEntry("amd64", "metal", "linux-netboot", "initramfs", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "kernel", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "rootfs", "none", standardFileMediaType),
	)
	got, err := netboot.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "linux-netboot",
		Compressions:   []string{"none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries()) != 3 {
		t.Fatalf("linux-netboot default roles: got %d entries", len(got.Entries()))
	}

	incus := testIndex(
		testEntry("amd64", "incus", "incus-vm", "disk", "none", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "metadata", "none", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "rootfs", "none", standardFileMediaType),
	)
	got, err = incus.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "incus",
		Representation: "incus-vm",
		Compressions:   []string{"none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := got.Entries()
	if len(entries) != 2 || entries[0].Selector.Role != "disk" || entries[1].Selector.Role != "metadata" {
		t.Fatalf("incus-vm default roles: %#v", entries)
	}

	unknown := testIndex(
		testEntry("amd64", "qemu", "x-test-format", "x-test-file", "none", standardFileMediaType),
		testEntry("amd64", "qemu", "x-test-format", "x-other-file", "none", standardFileMediaType),
	)
	got, err = unknown.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "x-test-format",
		Compressions:   []string{"none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries()) != 2 {
		t.Fatalf("unknown representation default is every role, got %d", len(got.Entries()))
	}
}

func TestResolveCapabilityFilterBeforeCompression(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("amd64", "qemu", "x-test-format", "x-test-file", "gzip", standardFileMediaType),
		testEntry("amd64", "qemu", "x-test-format", "x-test-file", "none", standardFileMediaType),
		testEntry("amd64", "qemu", "x-test-format", "x-test-file", "zstd", "application/vnd.bigoci.file.v1"),
	)
	zeroCaps, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "x-test-format",
		Compressions:   []string{"zstd", "gzip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := zeroCaps.Entries()[0].Selector.Compression; got != "gzip" {
		t.Fatalf("zero Capabilities must drop zstd then pick gzip, got %q", got)
	}

	both, err := NewCapabilities(standardFileMediaType, "application/vnd.bigoci.file.v1")
	if err != nil {
		t.Fatal(err)
	}
	withBigoci, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "x-test-format",
		Compressions:   []string{"zstd", "gzip"},
		Capabilities:   both,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := withBigoci.Entries()[0].Selector.Compression; got != "zstd" {
		t.Fatalf("with BigOCI capability, prefer zstd, got %q", got)
	}
}

func TestResolveUnsupportedType(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("amd64", "qemu", "qcow2", "disk", "zstd", "application/vnd.bigoci.file.v1"),
	)
	got, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"zstd"},
	})
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("error = %v, want ErrUnsupportedType", err)
	}
	if got != nil {
		t.Fatalf("expected nil result, got %#v", got)
	}
}

func TestResolveMissingDeliverableAndQuery(t *testing.T) {
	t.Parallel()
	idx := testIndex(testEntry("amd64", "qemu", "qcow2", "disk", "none", standardFileMediaType))
	got, err := idx.Resolve(ResolveQuery{
		Architecture:   "arm64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"none"},
	})
	if err == nil || got != nil {
		t.Fatalf("missing deliverable: got (%#v, %v)", got, err)
	}
	if _, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
	}); err == nil {
		t.Fatal("empty compressions must be invalid")
	}
	if _, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"none", "none"},
	}); err == nil {
		t.Fatal("duplicate compressions must be invalid")
	}
	if _, err := idx.Resolve(ResolveQuery{
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"none"},
	}); err == nil {
		t.Fatal("missing architecture must be invalid")
	}
}

func TestResolvedIndexDigest(t *testing.T) {
	t.Parallel()
	idx := testIndex(testEntry("amd64", "qemu", "qcow2", "disk", "none", standardFileMediaType))
	got, err := idx.Resolve(ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IndexDigest() != idx.Digest() {
		t.Fatalf("IndexDigest = %q, want %q", got.IndexDigest(), idx.Digest())
	}
}

func TestValidateResolveQueryCompressions(t *testing.T) {
	t.Parallel()
	base := ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
	}
	tests := []struct {
		name         string
		compressions []string
		wantErr      bool
	}{
		{name: "none", compressions: []string{"none"}},
		{name: "gzip", compressions: []string{"gzip"}},
		{name: "xz", compressions: []string{"xz"}},
		{name: "zstd", compressions: []string{"zstd"}},
		{name: "all_legal", compressions: []string{"none", "gzip", "xz", "zstd"}},
		{name: "x-brotli", compressions: []string{"x-brotli"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := base
			q.Compressions = tc.compressions
			err := validateResolveQuery(q)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateResolveQuery(%q) succeeded", tc.compressions)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateResolveQuery(%q): %v", tc.compressions, err)
			}
		})
	}
}

func TestResolveRoleListAndPerRoleCompression(t *testing.T) {
	t.Parallel()

	netboot := testIndex(
		testEntry("amd64", "metal", "linux-netboot", "initramfs", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "kernel", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "rootfs", "none", standardFileMediaType),
	)
	mixed := testIndex(
		testEntry("amd64", "incus", "incus-vm", "disk", "gzip", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "metadata", "gzip", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "metadata", "zstd", standardFileMediaType),
	)

	tests := []struct {
		// name describes the spec section 7.3 behavior under test.
		name string
		// idx is the index the query resolves against.
		idx *Index
		// query is the resolve query under test.
		query ResolveQuery
		// want is the expected "role/compression" pair per selected entry.
		want []string
		// wantErr requires a nil result and a non-nil error.
		wantErr bool
	}{
		{
			name: "a present role list limits the result to those roles",
			idx:  netboot,
			query: ResolveQuery{
				Architecture:   "amd64",
				Target:         "metal",
				Representation: "linux-netboot",
				Roles:          []string{"initramfs"},
				Compressions:   []string{"none"},
			},
			want: []string{"initramfs/none"},
		},
		{
			name: "an absent selected role fails without a partial result",
			idx:  netboot,
			query: ResolveQuery{
				Architecture:   "amd64",
				Target:         "metal",
				Representation: "linux-netboot",
				Roles:          []string{"kernel", "x-absent"},
				Compressions:   []string{"none"},
			},
			wantErr: true,
		},
		{
			name: "one preference list may choose a different compression per role",
			idx:  mixed,
			query: ResolveQuery{
				Architecture:   "amd64",
				Target:         "incus",
				Representation: "incus-vm",
				Roles:          []string{"disk", "metadata"},
				Compressions:   []string{"zstd", "gzip"},
			},
			want: []string{"disk/gzip", "metadata/zstd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.idx.Resolve(tt.query)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected a selection error")
				}
				if got != nil {
					t.Fatalf("failed selection must return no roles, got %#v", got.Entries())
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			entries := got.Entries()
			chosen := make([]string, 0, len(entries))
			for _, entry := range entries {
				chosen = append(chosen, entry.Selector.Role+"/"+entry.Selector.Compression)
			}
			if !slices.Equal(chosen, tt.want) {
				t.Fatalf("selection = %v, want %v", chosen, tt.want)
			}
		})
	}
}
