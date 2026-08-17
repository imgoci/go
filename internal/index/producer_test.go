package index

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

const pinnedSpecCommit = "5b957102eeda16498fdcb80a738431b83abd4197"

func TestProducerRegistriesMatchPinnedSpec(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "conformance", "SPEC_COMMIT"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(raw))
	if got != pinnedSpecCommit {
		t.Fatalf("SPEC_COMMIT %q, want %q; review the §5.4 producer registries", got, pinnedSpecCommit)
	}
	assertRegistrySet(t, "targets", producerTargets(), pinnedTargets())
	assertRegistrySet(t, "representations", producerRepresentations(), pinnedRepresentations())
	assertRegistrySet(t, "roles", producerRoles(), pinnedRoles())
	assertRegistrySet(t, "compressions", producerCompressions(), pinnedCompressions())
}

func TestBuildAcceptsPinnedPublicSelectorValues(t *testing.T) {
	t.Parallel()
	for _, target := range pinnedTargets() {
		t.Run("target/"+target, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			m.Entries[0].Selector.Target = target
			if _, err := Build(m); err != nil {
				t.Fatalf("Build rejected public target %q: %v", target, err)
			}
		})
	}
	for _, representation := range pinnedRepresentations() {
		t.Run("representation/"+representation, func(t *testing.T) {
			t.Parallel()
			if _, err := Build(representationModel(representation)); err != nil {
				t.Fatalf("Build rejected public representation %q: %v", representation, err)
			}
		})
	}
	for _, role := range pinnedRoles() {
		t.Run("role/"+role, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			m.Entries[0].Selector.Role = role
			if _, err := Build(m); err != nil {
				t.Fatalf("Build rejected public role %q: %v", role, err)
			}
		})
	}
	for _, compression := range pinnedCompressions() {
		t.Run("compression/"+compression, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			m.Entries[0].Selector.Compression = compression
			if _, err := Build(m); err != nil {
				t.Fatalf("Build rejected public compression %q: %v", compression, err)
			}
		})
	}
}

func TestBuildAcceptsPrivateSelectorForm(t *testing.T) {
	t.Parallel()
	fields := []struct {
		name   string
		mutate func(*Selector)
	}{
		{name: "target", mutate: func(s *Selector) { s.Target = "x-acme-cloud" }},
		{name: "representation", mutate: func(s *Selector) { s.Representation = "x-acme-cloud" }},
		{name: "role", mutate: func(s *Selector) { s.Role = "x-acme-cloud" }},
		{name: "compression", mutate: func(s *Selector) { s.Compression = "x-acme-cloud" }},
	}
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			tc.mutate(&m.Entries[0].Selector)
			if _, err := Build(m); err != nil {
				t.Fatalf("Build rejected x-acme-cloud %s: %v", tc.name, err)
			}
		})
	}
}

func TestBuildDoesNotEnforceArchitectureRegistry(t *testing.T) {
	t.Parallel()
	m := minimalModel()
	m.Entries[0].Selector.Architecture = "qemuu"
	if _, err := Build(m); err != nil {
		t.Fatalf("Build rejected syntax-valid architecture %q: %v", "qemuu", err)
	}
}

func TestProducerOnlyViolations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Model)
		value   func() *Value
		wantSub string
	}{
		{
			name: "bare unknown target",
			mutate: func(m *Model) {
				m.Entries[0].Selector.Target = "qemuu"
			},
			value: func() *Value {
				return validValue(validDescriptor(
					"amd64", "qemuu", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				))
			},
			wantSub: AnnotationTarget,
		},
		{
			name: "bare unknown representation",
			mutate: func(m *Model) {
				m.Entries[0].Selector.Representation = "qemuu"
			},
			value: func() *Value {
				return validValue(validDescriptor(
					"amd64", "x-test-target", "qemuu", "x-test-file", "none",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				))
			},
			wantSub: AnnotationRepresentation,
		},
		{
			name: "bare unknown role",
			mutate: func(m *Model) {
				m.Entries[0].Selector.Role = "qemuu"
			},
			value: func() *Value {
				return validValue(validDescriptor(
					"amd64", "x-test-target", "x-test-format", "qemuu", "none",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				))
			},
			wantSub: AnnotationRole,
		},
		{
			name: "bare unknown compression",
			mutate: func(m *Model) {
				m.Entries[0].Selector.Compression = "qemuu"
			},
			value: func() *Value {
				return validValue(validDescriptor(
					"amd64", "x-test-target", "x-test-format", "x-test-file", "qemuu",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				))
			},
			wantSub: AnnotationCompression,
		},
		{
			name: "descriptor org.opencontainers.image.version",
			mutate: func(m *Model) {
				m.Entries[0].Annotations = map[string]string{AnnotationVersion: "1"}
			},
			value: func() *Value {
				v := validValue()
				v.Manifests[0].Annotations[AnnotationVersion] = "1"
				return v
			},
			wantSub: AnnotationVersion,
		},
		{
			name: "root io.imgoci.usage",
			mutate: func(m *Model) {
				m.Annotations = map[string]string{AnnotationUsage: "live"}
			},
			value: func() *Value {
				v := validValue()
				v.Annotations[AnnotationUsage] = "live"
				return v
			},
			wantSub: AnnotationUsage,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			tc.mutate(m)
			_, err := Build(m)
			if err == nil {
				t.Fatal("Build accepted producer-only violation")
			}
			if !errors.Is(err, ErrRule) {
				t.Fatalf("Build error %v is not ErrRule", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Build error %v does not name %q", err, tc.wantSub)
			}
			if verr := Validate(tc.value()); verr != nil {
				t.Fatalf("Validate rejected producer-only violation: %v", verr)
			}
		})
	}
}

func TestBuildRejectsMalformedPrivateSelector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
	}{
		{name: "x-foo", value: "x-foo"},
		{name: "x--foo", value: "x--foo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := minimalModel()
			m.Entries[0].Selector.Target = tc.value
			_, err := Build(m)
			if err == nil {
				t.Fatalf("Build accepted %q", tc.value)
			}
			if !errors.Is(err, ErrRule) {
				t.Fatalf("error %v is not ErrRule", err)
			}
		})
	}
}

func representationModel(representation string) *Model {
	switch representation {
	case "incus-vm":
		return &Model{
			Name:    "example",
			Version: "1",
			Entries: []ModelEntry{
				publicEntry("incus", "incus-vm", "disk", "disk.qcow2", testManifestDigest1, testContentDigestA),
				publicEntry(
					"incus",
					"incus-vm",
					"metadata",
					"metadata.tar.xz",
					testManifestDigest2,
					testContentDigestB,
				),
			},
		}
	case "linux-netboot":
		return &Model{
			Name:    "example",
			Version: "1",
			Entries: []ModelEntry{
				publicEntry("qemu", "linux-netboot", "kernel", "vmlinuz", testManifestDigest1, testContentDigestA),
			},
		}
	default:
		return &Model{
			Name:    "example",
			Version: "1",
			Entries: []ModelEntry{
				publicEntry("qemu", representation, "disk", "disk.img", testManifestDigest1, testContentDigestA),
			},
		}
	}
}

func publicEntry(target, representation, role, filename, manifestDigest, contentDigest string) ModelEntry {
	return ModelEntry{
		Digest: digest.Digest(manifestDigest),
		Size:   1,
		Selector: Selector{
			Architecture:   "amd64",
			Target:         target,
			Representation: representation,
			Role:           role,
			Compression:    "none",
		},
		ContentDigest: digest.Digest(contentDigest),
		ContentSize:   0,
		Filename:      filename,
	}
}

func assertRegistrySet(t *testing.T, name string, got map[string]struct{}, want []string) {
	t.Helper()
	wantSet := make(map[string]struct{}, len(want))
	for _, v := range want {
		wantSet[v] = struct{}{}
	}
	if !maps.Equal(got, wantSet) {
		t.Fatalf("%s registry %v, want %v", name, got, wantSet)
	}
}

// The pinned registries below repeat the spec §5.4 public values as literals so
// TestProducerRegistriesMatchPinnedSpec fails when the production tables drift.
// Deriving them from the production constants or sets makes that test vacuous.

// pinnedTargets returns the spec §5.4 public target registry.
func pinnedTargets() []string {
	return []string{
		"aliyun",
		"applehv",
		"aws",
		"azure",
		"azurestack",
		"digitalocean",
		"exoscale",
		"gcp",
		"hetzner",
		"hyperv",
		"ibmcloud",
		"incus",
		"kubevirt",
		"metal",
		"nutanix",
		"openstack",
		"oraclecloud",
		"powervs",
		"proxmoxve",
		"qemu",
		"virtualbox",
		"vmware",
		"vultr",
	}
}

// pinnedRepresentations returns the spec §5.4 public representation registry.
func pinnedRepresentations() []string {
	return []string{
		"raw",
		"raw-4kn",
		"qcow2",
		"incus-vm",
		"iso",
		"linux-netboot",
	}
}

// pinnedRoles returns the spec §5.4 public role registry.
func pinnedRoles() []string {
	return []string{
		"disk",
		"kernel",
		"initramfs",
		"metadata",
		"rootfs",
	}
}

// pinnedCompressions returns the spec §5.4 public compression registry.
func pinnedCompressions() []string {
	return []string{
		"none",
		"gzip",
		"xz",
		"zstd",
	}
}
