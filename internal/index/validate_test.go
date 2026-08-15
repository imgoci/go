package index

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestValidateRule1(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mut  func(*Value)
	}{
		{name: "wrong schemaVersion", mut: func(v *Value) { v.SchemaVersion = 1 }},
		{name: "missing schemaVersion", mut: func(v *Value) { v.schemaVersionSet = false }},
		{name: "wrong mediaType", mut: func(v *Value) { v.MediaType = "application/json" }},
		{name: "wrong artifactType", mut: func(v *Value) { v.ArtifactType = "application/vnd.example.release.v1" }},
		{name: "empty manifests", mut: func(v *Value) { v.Manifests = nil }},
		{name: "missing annotations", mut: func(v *Value) { v.annotationsSet = false }},
		{name: "missing name", mut: func(v *Value) { delete(v.Annotations, AnnotationName) }},
		{name: "missing version", mut: func(v *Value) { delete(v.Annotations, AnnotationVersion) }},
		{name: "version with space", mut: func(v *Value) { v.Annotations[AnnotationVersion] = "1 2" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := cloneValue(validValue())
			tc.mut(v)
			assertRule(t, Validate(v), specRuleRoot)
		})
	}
}

func TestValidateRule2(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mut  func(*Value)
	}{
		{name: "wrong descriptor mediaType", mut: func(v *Value) { v.Manifests[0].MediaType = "application/json" }},
		{name: "long_s_lookalike_mediaType", mut: func(v *Value) {
			v.Manifests[0].MediaType = "application/vnd.oci.image.manife\u017Ft.v1+json"
		}},
		{name: "missing artifactType", mut: func(v *Value) { v.Manifests[0].artifactTypeSet = false }},
		{name: "missing digest", mut: func(v *Value) { v.Manifests[0].digestSet = false }},
		{name: "size zero", mut: func(v *Value) { v.Manifests[0].Size = 0 }},
		{name: "size too large", mut: func(v *Value) { v.Manifests[0].Size = maxManifestSize + 1 }},
		{name: "missing filename", mut: func(v *Value) { delete(v.Manifests[0].Annotations, AnnotationFilename) }},
		{
			name: "missing content digest",
			mut:  func(v *Value) { delete(v.Manifests[0].Annotations, AnnotationContentDigest) },
		},
		{name: "missing annotations", mut: func(v *Value) { v.Manifests[0].annotationsSet = false }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := cloneValue(validValue())
			tc.mut(v)
			assertRule(t, Validate(v), specRuleDescriptor)
		})
	}
}

func TestValidateRule3(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mut  func(*Value)
	}{
		{name: "uppercase architecture", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationArchitecture] = "AMD64"
		}},
		{name: "overlong architecture token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationArchitecture] = strings.Repeat("a", 129) + "/v7"
		}},
		{name: "malformed filename", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationFilename] = "not/a-filename"
		}},
		{name: "oversized content", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationContentSize] = "9223372036854775808"
		}},
		{name: "leading zero content size", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationContentSize] = "01"
		}},
		{name: "malformed artifactType", mut: func(v *Value) {
			v.Manifests[0].ArtifactType = "application"
		}},
		{name: "bad name token", mut: func(v *Value) {
			v.Annotations[AnnotationName] = "Example"
		}},
		{name: "bad content digest", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationContentDigest] = "sha256:ZZ"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := cloneValue(validValue())
			tc.mut(v)
			assertRule(t, Validate(v), specRuleSyntax)
		})
	}
}

func TestValidateRule4(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    *Value
	}{
		{
			name: "raw missing disk",
			v: validValue(validDescriptor(
				"amd64", "x-test-target", "raw", "x-test-file", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			)),
		},
		{
			name: "linux-netboot missing kernel",
			v: validValue(validDescriptor(
				"amd64", "metal", "linux-netboot", "initramfs", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			)),
		},
		{
			name: "incus-vm missing metadata",
			v: validValue(validDescriptor(
				"amd64", "incus", "incus-vm", "disk", "none",
				"disk.qcow2", testContentDigestA, "0", testManifestDigest1, 1,
			)),
		},
		{
			name: "incus-vm wrong target",
			v: validValue(
				validDescriptor(
					"amd64", "qemu", "incus-vm", "disk", "none",
					"disk.qcow2", testContentDigestA, "0", testManifestDigest1, 1,
				),
				validDescriptor(
					"amd64", "qemu", "incus-vm", "metadata", "none",
					"metadata.tar.xz", testContentDigestB, "0", testManifestDigest2, 1,
				),
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRule(t, Validate(tc.v), specRuleRoles)
		})
	}
}

func TestEqualMediaTypeASCIIOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "long_s_not_s", a: "manife\u017Ft", b: "manifest", want: false},
		{name: "s_not_long_s", a: "manifest", b: "manife\u017Ft", want: false},
		{name: "kelvin_not_k", a: "\u212A", b: "k", want: false},
		{name: "k_not_kelvin", a: "k", b: "\u212A", want: false},
		{name: "ascii_k", a: "K", b: "k", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := equalMediaType(tc.a, tc.b); got != tc.want {
				t.Fatalf("equalMediaType(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestValidateRule4ReportsFirstDeliverable(t *testing.T) {
	t.Parallel()
	v := validValue(
		validDescriptor(
			"arm64", "qemu", "raw", "x-test-file", "none",
			"a", testContentDigestA, "0", testManifestDigest1, 1,
		),
		validDescriptor(
			"amd64", "qemu", "raw", "x-test-file", "none",
			"b", testContentDigestB, "0", testManifestDigest2, 1,
		),
	)
	err := Validate(v)
	assertRule(t, err, specRuleRoles)
	if err != nil && !strings.Contains(err.Error(), "deliverable arm64, qemu, raw") {
		t.Fatalf("want first-appearing deliverable arm64, got %v", err)
	}
}

func TestValidateRule5(t *testing.T) {
	t.Parallel()
	d1 := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "0", testManifestDigest1, 1,
	)
	d2 := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "0", testManifestDigest2, 1,
	)
	assertRule(t, Validate(validValue(d1, d2)), specRuleSelector)
}

func TestValidateRule6(t *testing.T) {
	t.Parallel()
	gzip := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	none := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestB, "1", testManifestDigest2, 1,
	)
	assertRule(t, Validate(validValue(gzip, none)), specRuleFileIdentity)
}

func TestValidateRule7(t *testing.T) {
	t.Parallel()
	a := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	b := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-other", "none",
		"a", testContentDigestA, "1", testManifestDigest2, 1,
	)
	assertRule(t, Validate(validValue(a, b)), specRuleFilename)
}

func TestValidateRule8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    *Value
	}{
		{
			name: "shared digest different compression",
			v: validValue(
				validDescriptor(
					"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
					"a", testContentDigestA, "1", testManifestDigest1, 1,
				),
				validDescriptor(
					"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1,
				),
			),
		},
		{
			name: "shared digest different artifactType",
			v: func() *Value {
				left := validDescriptor(
					"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				)
				right := validDescriptor(
					"arm64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "0", testManifestDigest1, 1,
				)
				right.ArtifactType = ArtifactTypeBigOCI
				return validValue(left, right)
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRule(t, Validate(tc.v), specRuleSharedManifest)
		})
	}
}

func TestValidateRule9(t *testing.T) {
	t.Parallel()
	none := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest2, 1,
	)
	gzip := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	assertRule(t, Validate(validValue(none, gzip)), specRuleOrder)
}

func TestValidateNil(t *testing.T) {
	t.Parallel()
	assertRule(t, Validate(nil), specRuleRoot)
}

func TestConformancePass(t *testing.T) {
	t.Parallel()
	for _, name := range listFixtures(t, "pass", conformancePassCount) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := decodeFixture(t, "pass", name)
			if err := Validate(v); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestConformanceFail(t *testing.T) {
	t.Parallel()
	for _, name := range listFixtures(t, "fail", conformanceFailCount) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := decodeFixture(t, "fail", name)
			want := failFixtureRule(name)
			if want == 0 {
				t.Fatalf("unmapped fail fixture %s", name)
			}
			assertRule(t, Validate(v), want)
		})
	}
}

func listFixtures(t *testing.T, kind string, want int) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtureRoot, kind))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != want {
		t.Fatalf("%s fixtures: got %d, want %d", kind, len(entries), want)
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		names = append(names, ent.Name())
	}
	return names
}

func decodeFixture(t *testing.T, kind, name string) *Value {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, kind, name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return v
}

func failFixtureRule(name string) int {
	t := map[string]int{
		"wrong-index-media-type.json":             specRuleRoot,
		"wrong-index-artifact-type.json":          specRuleRoot,
		"missing-artifact-type.json":              specRuleDescriptor,
		"wrong-descriptor-media-type.json":        specRuleDescriptor,
		"missing-content-digest.json":             specRuleDescriptor,
		"missing-filename-with-legacy-title.json": specRuleDescriptor,
		"malformed-artifact-type.json":            specRuleSyntax,
		"malformed-filename.json":                 specRuleSyntax,
		"overlong-architecture-token.json":        specRuleSyntax,
		"oversized-content.json":                  specRuleSyntax,
		"missing-disk-role.json":                  specRuleRoles,
		"missing-incus-disk-role.json":            specRuleRoles,
		"missing-incus-metadata-role.json":        specRuleRoles,
		"missing-linux-netboot-kernel.json":       specRuleRoles,
		"wrong-incus-target.json":                 specRuleRoles,
		"duplicate-selector-tuple.json":           specRuleSelector,
		"inconsistent-file-content.json":          specRuleFileIdentity,
		"duplicate-role-filename.json":            specRuleFilename,
		"inconsistent-shared-manifest.json":       specRuleSharedManifest,
		"inconsistent-shared-artifact-type.json":  specRuleSharedManifest,
		"noncanonical-order.json":                 specRuleOrder,
	}
	rule, ok := t[name]
	if !ok {
		return 0
	}
	return rule
}

func assertRule(t *testing.T, err error, rule int) {
	t.Helper()
	if err == nil {
		t.Fatalf("want spec §6 rule %d, got success", rule)
	}
	want := "spec §6 rule " + strconv.Itoa(rule)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name %s", err, want)
	}
}
