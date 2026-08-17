package imgoci

import (
	"slices"
	"testing"

	"github.com/opencontainers/go-digest"
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
	return &Index{
		digest:  digest.Digest("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		name:    "example",
		version: "1",
		entries: entries,
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
