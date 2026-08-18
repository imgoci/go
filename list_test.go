package imgoci

import (
	"slices"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/imgoci/go/internal/index"
)

func testEntry(arch, target, repr, role, compression, artifactType string) FileEntry {
	return FileEntry{
		MediaType:    "application/vnd.oci.image.manifest.v1+json",
		ArtifactType: artifactType,
		Digest:       digest.Digest("sha256:1111111111111111111111111111111111111111111111111111111111111111"),
		Size:         1,
		Selector: Selector{
			Architecture:   arch,
			Target:         target,
			Representation: repr,
			Role:           role,
			Compression:    compression,
		},
		ContentDigest: digest.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ContentSize:   0,
		Filename:      role + ".bin",
		Annotations:   map[string]string{},
	}
}

func testIndex(entries ...FileEntry) *Index {
	out := make([]index.Entry, len(entries))
	for i, entry := range entries {
		out[i] = index.Entry{
			MediaType:    entry.MediaType,
			ArtifactType: entry.ArtifactType,
			Digest:       entry.Digest,
			Size:         entry.Size,
			Selector: index.Selector{
				Architecture:   entry.Selector.Architecture,
				Target:         entry.Selector.Target,
				Representation: entry.Selector.Representation,
				Usage:          entry.Selector.Usage.String(),
				Role:           entry.Selector.Role,
				Compression:    entry.Selector.Compression,
			},
			ContentDigest: entry.ContentDigest,
			ContentSize:   entry.ContentSize,
			Filename:      entry.Filename,
			Annotations:   entry.Annotations,
		}
	}
	return &Index{
		digest:  digest.Digest("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		name:    "example",
		version: "1",
		entries: out,
	}
}

func TestListFiltersAndSort(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("arm64", "metal", "qcow2", "disk", "zstd", standardFileMediaType),
		testEntry("amd64", "qemu", "qcow2", "disk", "gzip", standardFileMediaType),
		testEntry("amd64", "qemu", "qcow2", "disk", "none", standardFileMediaType),
		testEntry("amd64", "qemu", "raw", "disk", "none", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "disk", "none", standardFileMediaType),
		testEntry("amd64", "incus", "incus-vm", "metadata", "none", standardFileMediaType),
	)

	got, err := idx.List(ListQuery{Representation: "qcow2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("qcow2 list len = %d, want 2", len(got))
	}
	if got[0].Architecture != "amd64" || got[1].Architecture != "arm64" {
		t.Fatalf("deliverables not sorted by architecture: %#v", got)
	}
	if len(got[0].Roles) != 1 || got[0].Roles[0].Role != "disk" {
		t.Fatalf("unexpected roles: %#v", got[0].Roles)
	}
	alts := got[0].Roles[0].Alternatives
	if len(alts) != 2 || alts[0].Compression != "gzip" || alts[1].Compression != "none" {
		t.Fatalf("alternatives not sorted by compression: %#v", alts)
	}

	got, err = idx.List(ListQuery{Roles: []string{"disk", "metadata"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Representation != "incus-vm" {
		t.Fatalf("role filter: %#v", got)
	}

	full, err := idx.List(ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([][3]string, 0, len(full))
	for _, deliverable := range full {
		keys = append(keys, [3]string{
			deliverable.Architecture,
			deliverable.Target,
			deliverable.Representation,
		})
	}
	wantKeys := [][3]string{
		{"amd64", "incus", "incus-vm"},
		{"amd64", "qemu", "qcow2"},
		{"amd64", "qemu", "raw"},
		{"arm64", "metal", "qcow2"},
	}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf(
			"deliverable keys = %v, want %v: sort is architecture, then target, then representation",
			keys,
			wantKeys,
		)
	}
}

func TestListEmptyAndInvalidQuery(t *testing.T) {
	t.Parallel()
	idx := testIndex(testEntry("amd64", "qemu", "qcow2", "disk", "none", standardFileMediaType))

	got, err := idx.List(ListQuery{Architecture: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty list should be valid, got %#v", got)
	}

	if _, err := idx.List(ListQuery{Roles: []string{}}); err == nil {
		t.Fatal("non-nil empty Roles must be invalid")
	}
	if _, err := idx.List(ListQuery{Architecture: "AMD64"}); err == nil {
		t.Fatal("uppercase architecture is not a basic token")
	}
	if _, err := idx.List(ListQuery{Architecture: "arm/v7/extra"}); err == nil {
		t.Fatal("three-part architecture must be invalid")
	}
	if _, err := idx.List(ListQuery{Roles: []string{"disk", "disk"}}); err == nil {
		t.Fatal("duplicate roles must be invalid")
	}
	if _, err := idx.List(ListQuery{Architecture: "arm/v7"}); err != nil {
		t.Fatalf("arm/v7 must be a valid architecture: %v", err)
	}
}

func TestListDoesNotFilterCapabilities(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("amd64", "qemu", "qcow2", "disk", "zstd", "application/vnd.bigoci.file.v1"),
		testEntry("amd64", "qemu", "qcow2", "disk", "none", standardFileMediaType),
	)
	got, err := idx.List(ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Roles) != 1 || len(got[0].Roles[0].Alternatives) != 2 {
		t.Fatalf("listing must keep unsupported types: %#v", got)
	}
	alts := got[0].Roles[0].Alternatives
	if alts[0].Compression != "none" || alts[0].ArtifactType != standardFileMediaType {
		t.Fatalf("first alternative = %#v, want the standard type under none", alts[0])
	}
	if alts[1].Compression != "zstd" || alts[1].ArtifactType != "application/vnd.bigoci.file.v1" {
		t.Fatalf("second alternative = %#v, want the BigOCI type exposed, not filtered", alts[1])
	}
}

func TestListUTF8RoleSort(t *testing.T) {
	t.Parallel()
	idx := testIndex(
		testEntry("amd64", "metal", "linux-netboot", "rootfs", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "kernel", "none", standardFileMediaType),
		testEntry("amd64", "metal", "linux-netboot", "initramfs", "none", standardFileMediaType),
	)
	got, err := idx.List(ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(got[0].Roles))
	for _, role := range got[0].Roles {
		roles = append(roles, role.Role)
	}
	want := []string{"initramfs", "kernel", "rootfs"}
	if !slices.Equal(roles, want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
}

func TestListQueryUsageValidation(t *testing.T) {
	t.Parallel()
	idx := testIndex(testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType))

	got, err := idx.List(ListQuery{Usage: nil})
	require.NoError(t, err, "nil usage applies no filter")
	assert.NotEmpty(t, got, "nil usage must match every usage set")

	_, err = idx.List(ListQuery{Usage: []string{}})
	require.Error(t, err, "non-nil empty usage must be rejected")
	assert.Contains(t, err.Error(), "usage", "error must name the usage field")

	_, err = idx.List(ListQuery{Usage: []string{"install", "install"}})
	require.Error(t, err, "duplicate usage tokens must be rejected")
	assert.Contains(t, err.Error(), "usage", "error must name the usage field")

	_, err = idx.List(ListQuery{Usage: []string{"Live"}})
	require.Error(t, err, "non-basic usage token must be rejected")
	assert.Contains(t, err.Error(), "usage", "error must name the usage field")
}

func TestListUsageFilterAndExactSet(t *testing.T) {
	t.Parallel()
	idx := usageSplitIndex()

	got, err := idx.List(ListQuery{})
	require.NoError(t, err)
	require.Len(t, got, 5, "nil usage returns every deliverable")
	assertDeliverableKeys(t, got, [][4]string{
		{"amd64", "metal", "iso", ""},
		{"amd64", "metal", "iso", "install"},
		{"amd64", "metal", "iso", "install,install-offline"},
		{"amd64", "metal", "iso", "live"},
		{"amd64", "metal", "qcow2", ""},
	})
	assert.Empty(t, got[0].Usage.String(), "empty usage set sorts first and is reported exactly")
	assert.Equal(t, "install", got[1].Usage.String())
	assert.Equal(t, "install,install-offline", got[2].Usage.String())
	assert.Equal(t, "live", got[3].Usage.String())
	assert.Empty(t, got[4].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"install"}})
	require.NoError(t, err)
	require.Len(t, got, 2, "install filter matches every set that contains install")
	assertDeliverableKeys(t, got, [][4]string{
		{"amd64", "metal", "iso", "install"},
		{"amd64", "metal", "iso", "install,install-offline"},
	})
	assert.Equal(t, "install", got[0].Usage.String(), "result reports the exact set, not the filter")
	assert.Equal(t, "install,install-offline", got[1].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"install-offline"}})
	require.NoError(t, err)
	require.Len(t, got, 1, "install-offline-only filter is containment, not equality")
	assert.Equal(t, "install,install-offline", got[0].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"live", "install"}})
	require.NoError(t, err)
	assert.Empty(t, got, "unsorted live+install matches only a set containing both")

	got, err = idx.List(ListQuery{Usage: []string{"install", "install-offline", "live"}})
	require.NoError(t, err)
	assert.Empty(t, got, "a usage filter that is a superset of every deliverable matches nothing")

	got, err = idx.List(ListQuery{Usage: []string{"custom-usage"}})
	require.NoError(t, err)
	assert.Empty(t, got, "unmatched usage returns an empty result, not an error")
}

func TestListUnknownAndPrivateUsage(t *testing.T) {
	t.Parallel()
	custom := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	custom.Selector.Usage = usageFromCanonical("custom-usage")
	private := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	private.Selector.Usage = usageFromCanonical("x-owner-name")
	combined := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	combined.Selector.Usage = usageFromCanonical("custom-usage,x-owner-name")
	idx := testIndex(custom, private, combined)

	got, err := idx.List(ListQuery{})
	require.NoError(t, err)
	require.Len(t, got, 3, "unknown and private usage tokens must list normally")
	assert.Equal(t, "custom-usage", got[0].Usage.String())
	assert.Equal(t, "custom-usage,x-owner-name", got[1].Usage.String())
	assert.Equal(t, "x-owner-name", got[2].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"custom-usage"}})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "custom-usage", got[0].Usage.String())
	assert.Equal(t, "custom-usage,x-owner-name", got[1].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"x-owner-name"}})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "custom-usage,x-owner-name", got[0].Usage.String())
	assert.Equal(t, "x-owner-name", got[1].Usage.String())

	got, err = idx.List(ListQuery{Usage: []string{"x-owner-name", "custom-usage"}})
	require.NoError(t, err)
	require.Len(t, got, 1, "list reports the exact combined unknown and private set")
	assert.Equal(t, "custom-usage,x-owner-name", got[0].Usage.String())
}

func TestListRoleFilterIsPerUsageSet(t *testing.T) {
	t.Parallel()
	empty := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	liveKernel := testEntry("amd64", "metal", "iso", "kernel", "none", standardFileMediaType)
	liveKernel.Selector.Usage = usageFromCanonical("live")
	idx := testIndex(empty, liveKernel)

	got, err := idx.List(ListQuery{Roles: []string{"kernel"}})
	require.NoError(t, err)
	require.Len(t, got, 1, "a role present only under live must not satisfy the empty-usage deliverable")
	assert.Equal(t, "live", got[0].Usage.String())
	require.Len(t, got[0].Roles, 1)
	assert.Equal(t, "kernel", got[0].Roles[0].Role)

	got, err = idx.List(ListQuery{Roles: []string{"disk"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Usage.String())
}

func TestListSameRoleUnderTwoUsageSets(t *testing.T) {
	t.Parallel()
	empty := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	live := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	live.Selector.Usage = usageFromCanonical("live")
	idx := testIndex(empty, live)

	got, err := idx.List(ListQuery{Roles: []string{"disk"}})
	require.NoError(t, err)
	require.Len(t, got, 2, "the same role under two usage sets lists both deliverables")
	assert.Empty(t, got[0].Usage.String())
	assert.Equal(t, "live", got[1].Usage.String())

	got, err = idx.List(ListQuery{Roles: []string{"disk"}, Usage: []string{"live"}})
	require.NoError(t, err)
	require.Len(t, got, 1, "adding a usage filter narrows the same-role pair to one deliverable")
	assert.Equal(t, "live", got[0].Usage.String())
}

func TestListSortsByUsage(t *testing.T) {
	t.Parallel()
	idx := usageSplitIndex()

	got, err := idx.List(ListQuery{Architecture: "amd64", Target: "metal", Representation: "iso"})
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []string{"", "install", "install,install-offline", "live"}, usageStrings(got))
	assert.Empty(t, got[0].Usage.String(), "empty usage set sorts first when other key fields match")
}

func usageSplitIndex() *Index {
	empty := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	install := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	install.Selector.Usage = usageFromCanonical("install")
	offline := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	offline.Selector.Usage = usageFromCanonical("install,install-offline")
	live := testEntry("amd64", "metal", "iso", "disk", "none", standardFileMediaType)
	live.Selector.Usage = usageFromCanonical("live")
	other := testEntry("amd64", "metal", "qcow2", "disk", "none", standardFileMediaType)
	return testIndex(live, offline, other, install, empty)
}

func assertDeliverableKeys(t *testing.T, got []Deliverable, want [][4]string) {
	t.Helper()
	keys := make([][4]string, 0, len(got))
	for _, deliverable := range got {
		keys = append(keys, [4]string{
			deliverable.Architecture,
			deliverable.Target,
			deliverable.Representation,
			deliverable.Usage.String(),
		})
	}
	assert.Equal(t, want, keys)
}

func usageStrings(got []Deliverable) []string {
	out := make([]string, 0, len(got))
	for _, deliverable := range got {
		out = append(out, deliverable.Usage.String())
	}
	return out
}
