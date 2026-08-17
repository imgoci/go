package imgoci

import (
	"errors"
	"slices"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			_, err := validateResolveQuery(q)
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
				assertResolveFailedWholesale(t, got, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertResolvedRoleCompressions(t, got, tt.want)
		})
	}
}

func TestValidateResolveQueryUsage(t *testing.T) {
	t.Parallel()
	base := ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "iso",
		Compressions:   []string{"none"},
	}
	tests := []struct {
		name    string
		usage   []string
		want    string
		wantErr string
	}{
		{name: "nil is the empty set", usage: nil, want: ""},
		{name: "empty slice is the empty set", usage: []string{}, want: ""},
		{
			name:  "install-offline without install is accepted",
			usage: []string{"install-offline"},
			want:  "install-offline",
		},
		{
			name:    "duplicates are rejected",
			usage:   []string{"install", "install"},
			wantErr: "resolve query usage",
		},
		{
			name:    "a non-basic token is rejected",
			usage:   []string{"INSTALL"},
			wantErr: "resolve query usage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := base
			q.Usage = tt.usage
			got, err := validateResolveQuery(q)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveUsageExactEquality(t *testing.T) {
	t.Parallel()

	empty := Usage{}
	install := mustUsage(t, "install")
	compound := mustUsage(t, "install", "install-offline")
	live := mustUsage(t, "live")

	// live is first so a usage-blind match cannot accidentally return the empty
	// set, and compound precedes install so containment would pick compound
	// for a query of {"install"}.
	idx := testIndex(
		metalISODisk(live, "live.iso", liveContentDigest),
		metalISODisk(compound, "compound.iso", compoundContentDigest),
		metalISODisk(install, "install.iso", installContentDigest),
		metalISODisk(empty, "empty.iso", emptyContentDigest),
	)
	base := ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "iso",
		Compressions:   []string{"none"},
	}

	tests := []struct {
		name         string
		idx          *Index
		usage        []string
		wantFilename string
		wantDigest   string
		wantUsage    string
		wantErr      string
	}{
		{
			name:         "nil usage selects the empty-usage deliverable",
			idx:          idx,
			usage:        nil,
			wantFilename: "empty.iso",
			wantDigest:   emptyContentDigest,
		},
		{
			name:         "empty slice selects the empty-usage deliverable",
			idx:          idx,
			usage:        []string{},
			wantFilename: "empty.iso",
			wantDigest:   emptyContentDigest,
		},
		{
			name: "empty-usage first wins over later same-compression non-empty usage",
			idx: testIndex(
				metalISODisk(empty, "empty.iso", emptyContentDigest),
				metalISODisk(live, "live.iso", liveContentDigest),
			),
			usage:        nil,
			wantFilename: "empty.iso",
			wantDigest:   emptyContentDigest,
		},
		{
			name: "nil usage against only non-empty usage names the empty set",
			idx: testIndex(
				metalISODisk(live, "live.iso", liveContentDigest),
				metalISODisk(install, "install.iso", installContentDigest),
			),
			usage:   nil,
			wantErr: "usage=<empty>",
		},
		{
			name:         "unsorted tokens select the compound usage set",
			idx:          idx,
			usage:        []string{"install-offline", "install"},
			wantFilename: "compound.iso",
			wantDigest:   compoundContentDigest,
			wantUsage:    "install,install-offline",
		},
		{
			name:    "a subset does not match a larger usage set",
			idx:     testIndex(metalISODisk(compound, "compound.iso", compoundContentDigest)),
			usage:   []string{"install"},
			wantErr: `usage="install"`,
		},
		{
			name:    "a superset does not match",
			idx:     idx,
			usage:   []string{"install", "install-offline", "live"},
			wantErr: `usage="install,install-offline,live"`,
		},
		{
			name:    "a usage set present on no deliverable names the request",
			idx:     idx,
			usage:   []string{"rescue"},
			wantErr: `usage="rescue"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := base
			q.Usage = tt.usage
			got, err := tt.idx.Resolve(q)
			if tt.wantErr != "" {
				assertResolveFailedWholesale(t, got, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				require.NotErrorIs(t, err, ErrUnsupportedType)
				require.NotErrorIs(t, err, ErrNotFound)
				return
			}
			require.NoError(t, err)
			entries := got.Entries()
			require.Len(t, entries, 1)
			assert.Equal(t, tt.wantFilename, entries[0].Filename)
			assert.Equal(t, digest.Digest(tt.wantDigest), entries[0].ContentDigest)
			assert.Equal(t, tt.wantUsage, entries[0].Selector.Usage.String())
		})
	}
}

func TestResolveUnknownAndPrivateUsage(t *testing.T) {
	t.Parallel()
	unknown := mustUsage(t, "x-recovery")
	private := mustUsage(t, "x-vendor.private")
	combined := mustUsage(t, "x-owner-name", "custom-usage")
	idx := testIndex(
		metalISODisk(unknown, "unknown.iso", unknownContentDigest),
		metalISODisk(private, "private.iso", privateContentDigest),
		metalISODisk(combined, "combined.iso", combinedContentDigest),
	)

	tests := []struct {
		name         string
		usage        []string
		wantFilename string
		wantDigest   string
	}{
		{
			name:         "unknown usage resolves",
			usage:        []string{"x-recovery"},
			wantFilename: "unknown.iso",
			wantDigest:   unknownContentDigest,
		},
		{
			name:         "private usage resolves",
			usage:        []string{"x-vendor.private"},
			wantFilename: "private.iso",
			wantDigest:   privateContentDigest,
		},
		{
			name:         "unsorted unknown and private set resolves",
			usage:        []string{"x-owner-name", "custom-usage"},
			wantFilename: "combined.iso",
			wantDigest:   combinedContentDigest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := idx.Resolve(ResolveQuery{
				Architecture:   "amd64",
				Target:         "metal",
				Representation: "iso",
				Usage:          tt.usage,
				Compressions:   []string{"none"},
			})
			require.NoError(t, err)
			entries := got.Entries()
			require.Len(t, entries, 1)
			assert.Equal(t, tt.wantFilename, entries[0].Filename)
			assert.Equal(t, digest.Digest(tt.wantDigest), entries[0].ContentDigest)
		})
	}
}

func TestResolveUsageOfflineFailureContract(t *testing.T) {
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
	require.Error(t, err)
	assert.Nil(t, got)
	require.ErrorIs(t, err, ErrUnsupportedType)

	missing, err := idx.Resolve(ResolveQuery{
		Architecture:   "arm64",
		Target:         "qemu",
		Representation: "qcow2",
		Usage:          []string{"live"},
		Compressions:   []string{"none"},
	})
	require.Error(t, err)
	assert.Nil(t, missing)
	require.NotErrorIs(t, err, ErrUnsupportedType)
	require.NotErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), `usage="live"`)
}

const (
	emptyContentDigest    = "sha256:e000000000000000000000000000000000000000000000000000000000000000"
	installContentDigest  = "sha256:e111111111111111111111111111111111111111111111111111111111111111"
	compoundContentDigest = "sha256:e222222222222222222222222222222222222222222222222222222222222222"
	liveContentDigest     = "sha256:e333333333333333333333333333333333333333333333333333333333333333"
	unknownContentDigest  = "sha256:e444444444444444444444444444444444444444444444444444444444444444"
	privateContentDigest  = "sha256:e555555555555555555555555555555555555555555555555555555555555555"
	combinedContentDigest = "sha256:e666666666666666666666666666666666666666666666666666666666666666"
)

// metalISODisk returns the (amd64, metal, iso, disk) fixture with usage identity.
func metalISODisk(usage Usage, filename, contentDigest string) FileEntry {
	entry := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	entry.Selector.Usage = usage
	entry.Filename = filename
	entry.ContentDigest = digest.Digest(contentDigest)
	return entry
}

// mustUsage returns a validated Usage or fails the test.
func mustUsage(t testing.TB, values ...string) Usage {
	t.Helper()
	usage, err := NewUsage(values...)
	require.NoError(t, err)
	return usage
}

// assertResolveFailedWholesale requires a non-nil error and a nil result: spec
// section 7.3 admits no partial selection.
func assertResolveFailedWholesale(t *testing.T, got *Resolved, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a selection error")
	}
	if got != nil {
		t.Fatalf("failed selection must return no roles, got %#v", got.Entries())
	}
}

// assertResolvedRoleCompressions requires the resolution's entries to be
// exactly want, each formatted as "role/compression" in selection order.
func assertResolvedRoleCompressions(t *testing.T, got *Resolved, want []string) {
	t.Helper()
	entries := got.Entries()
	chosen := make([]string, 0, len(entries))
	for _, entry := range entries {
		chosen = append(chosen, entry.Selector.Role+"/"+entry.Selector.Compression)
	}
	if !slices.Equal(chosen, want) {
		t.Fatalf("selection = %v, want %v", chosen, want)
	}
}
