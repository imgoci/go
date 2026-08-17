package index

import (
	"errors"
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
		// spec §5.1: the release version is 1 to 128 printable ASCII characters,
		// so C0 control characters and DEL are excluded.
		{name: "version too long", mut: func(v *Value) {
			v.Annotations[AnnotationVersion] = strings.Repeat("9", maxReleaseVersion+1)
		}},
		{name: "version with control character", mut: func(v *Value) {
			v.Annotations[AnnotationVersion] = "1\x01"
		}},
		{name: "version with DEL", mut: func(v *Value) {
			v.Annotations[AnnotationVersion] = "1\x7f"
		}},
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
		// spec §5.2: each of the eight required descriptor annotations is
		// individually mandatory.
		{name: "missing architecture", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationArchitecture)
		}},
		{name: "missing target", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationTarget)
		}},
		{name: "missing representation", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationRepresentation)
		}},
		{name: "missing role", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationRole)
		}},
		{name: "missing compression", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationCompression)
		}},
		{name: "missing content size", mut: func(v *Value) {
			delete(v.Manifests[0].Annotations, AnnotationContentSize)
		}},
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
		// spec §5.3: target, representation, role, and compression are each
		// validated as a basic token on their own.
		{name: "malformed target", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationTarget] = "x-test-target-"
		}},
		{name: "malformed representation", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationRepresentation] = "X-Test-Format"
		}},
		{name: "malformed role", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationRole] = "x..test"
		}},
		{name: "empty compression", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationCompression] = ""
		}},
		// spec §5.3: a basic token holds at most 128 ASCII bytes.
		{name: "overlong target token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationTarget] = strings.Repeat("t", maxBasicTokenBytes+1)
		}},
		// spec §5.3: a filename holds at most 255 ASCII bytes.
		{name: "overlong filename", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationFilename] = "a" + strings.Repeat("b", maxFilenameBytes-1) + "a"
		}},
		// spec §5.3: a filename is one path component and must not be . or ..
		{name: "dot filename", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationFilename] = "."
		}},
		{name: "dot dot filename", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationFilename] = ".."
		}},
		// spec §5.3: content.digest hex digits must be lowercase.
		{name: "uppercase content digest", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationContentDigest] = "sha256:" + strings.Repeat("A", sha256HexLength)
		}},
		{name: "present empty usage", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = ""
		}},
		{name: "duplicate usage token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "install,install"
		}},
		{name: "descending usage tokens", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "live,install"
		}},
		{name: "malformed usage delimiter", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "a,,b"
		}},
		{name: "invalid usage token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "Live"
		}},
		{name: "trailing usage comma", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "a,"
		}},
		{name: "usage value 4097 bytes", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = usageValueOfLength(maxUsageBytes + 1)
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
		// spec §5.4: the raw-4kn and iso representations each require the disk role.
		{
			name: "raw-4kn missing disk",
			v: validValue(validDescriptor(
				"amd64", "x-test-target", "raw-4kn", "x-test-file", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			)),
		},
		{
			name: "iso missing disk",
			v: validValue(validDescriptor(
				"amd64", "x-test-target", "iso", "x-test-file", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			)),
		},
		{
			name: "install-offline without install",
			v: withUsageValue(validValue(validDescriptor(
				"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
				"a", testContentDigestA, "0", testManifestDigest1, 1,
			)), 0, "install-offline"),
		},
		{
			name: "required role only under a different usage set",
			v: validValue(
				validDescriptor(
					"amd64", "metal", "linux-netboot", "kernel", "none",
					"vmlinuz", testContentDigestA, "0", testManifestDigest1, 1,
				),
				withUsage(validDescriptor(
					"amd64", "metal", "linux-netboot", "initramfs", "none",
					"initrd", testContentDigestB, "0", testManifestDigest2, 1,
				), "live"),
			),
		},
		{
			name: "incus-vm required roles split across usage sets",
			v: validValue(
				validDescriptor(
					"amd64", "incus", "incus-vm", "disk", "none",
					"disk.qcow2", testContentDigestA, "0", testManifestDigest1, 1,
				),
				withUsage(validDescriptor(
					"amd64", "incus", "incus-vm", "metadata", "none",
					"metadata.tar.xz", testContentDigestB, "0", testManifestDigest2, 1,
				), "live"),
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

// TestValidateAcceptsBoundaryValues holds the accepting side of the spec §5.1,
// §5.2, and §5.3 value boundaries: a check stricter than the spec fails here.
func TestValidateAcceptsBoundaryValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mut  func(*Value)
	}{
		// spec §5.3: a basic token may be 128 ASCII bytes long.
		{name: "target token 128 bytes", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationTarget] = strings.Repeat("t", maxBasicTokenBytes)
		}},
		// spec §5.3: a filename may be 255 ASCII bytes long.
		{name: "filename 255 bytes", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationFilename] = "a" + strings.Repeat("b", maxFilenameBytes-2) + "a"
		}},
		// spec §5.2: descriptor size may be 9007199254740991.
		{name: "size at 2^53-1", mut: func(v *Value) { v.Manifests[0].Size = maxManifestSize }},
		// spec §5.1: the release version may be 1 to 128 printable ASCII characters.
		{name: "version one character", mut: func(v *Value) { v.Annotations[AnnotationVersion] = "7" }},
		{name: "version 128 characters", mut: func(v *Value) {
			v.Annotations[AnnotationVersion] = strings.Repeat("9", maxReleaseVersion)
		}},
		{name: "usage value 4096 bytes", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = usageValueOfLength(maxUsageBytes)
		}},
		// spec §5.3: consumers accept every syntactically valid selector
		// value; the public registry is producer-only.
		{name: "unknown usage token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "custom-usage"
		}},
		{name: "private usage token", mut: func(v *Value) {
			v.Manifests[0].Annotations[AnnotationUsage] = "x-owner-name"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := cloneValue(validValue())
			tc.mut(v)
			if err := Validate(v); err != nil {
				t.Fatalf("Validate rejected a boundary value: %v", err)
			}
		})
	}
}

// TestValidateRejectsWholeIndexForLaterDescriptor covers spec §6: an invalid
// entry anywhere invalidates the whole index, so validation does not stop after
// a valid first descriptor.
func TestValidateRejectsWholeIndexForLaterDescriptor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		mut  func(*Descriptor)
		rule int
	}{
		{
			name: "second descriptor missing filename annotation",
			mut:  func(d *Descriptor) { delete(d.Annotations, AnnotationFilename) },
			rule: specRuleDescriptor,
		},
		{
			name: "second descriptor malformed filename",
			mut:  func(d *Descriptor) { d.Annotations[AnnotationFilename] = "not/a-filename" },
			rule: specRuleSyntax,
		},
	}
	first := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "0", testManifestDigest1, 1,
	)
	if err := Validate(validValue(first)); err != nil {
		t.Fatalf("first descriptor must be valid on its own: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			second := validDescriptor(
				"amd64", "x-test-target", "x-test-format", "x-test-other", "none",
				"b", testContentDigestB, "0", testManifestDigest2, 1,
			)
			tc.mut(&second)
			assertRule(t, Validate(validValue(first, second)), tc.rule)
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
			if got := EqualMediaType(tc.a, tc.b); got != tc.want {
				t.Fatalf("EqualMediaType(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
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

func TestValidateRule5DifferentUsage(t *testing.T) {
	t.Parallel()
	empty := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "0", testManifestDigest1, 1,
	)
	live := withUsage(validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"b", testContentDigestB, "0", testManifestDigest2, 1,
	), "live")
	if err := Validate(validValue(empty, live)); err != nil {
		t.Fatalf("distinct usage sets must be unique selectors: %v", err)
	}
}

// rule6Pair returns two valid transport alternatives for one file: the same
// (architecture, target, representation, role) under different compressions,
// agreeing on content digest, content size, and filename as spec §6 rule 6
// requires. The pair is already in canonical order.
func rule6Pair() (Descriptor, Descriptor) {
	gzip := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	none := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest2, 1,
	)
	return gzip, none
}

// TestValidateRule6 covers spec §6 rule 6: transport alternatives for one file
// must agree on content digest, content size, and filename.
//
// Each case mutates one of the three fields on an otherwise valid pair, and the
// baseline assertion proves the unmutated pair validates, so a failure can only
// come from the mutation.
func TestValidateRule6(t *testing.T) {
	t.Parallel()
	base := func() *Value {
		gzip, none := rule6Pair()
		return validValue(gzip, none)
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("unmutated pair must validate: %v", err)
	}

	tests := []struct {
		name string
		// mutate disagrees with the first alternative on one rule-6 field.
		mutate func(*Descriptor)
	}{
		{
			name: "different content digest",
			mutate: func(d *Descriptor) {
				d.Annotations[AnnotationContentDigest] = testContentDigestB
			},
		},
		{
			name: "different content size",
			mutate: func(d *Descriptor) {
				d.Annotations[AnnotationContentSize] = "2"
			},
		},
		{
			name: "different filename",
			mutate: func(d *Descriptor) {
				d.Annotations[AnnotationFilename] = "b"
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := base()
			tc.mutate(&v.Manifests[1])
			assertRule(t, Validate(v), specRuleFileIdentity)
		})
	}
}

func TestValidateRule6DifferentUsage(t *testing.T) {
	t.Parallel()
	empty := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	live := withUsage(validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"b", testContentDigestB, "2", testManifestDigest2, 1,
	), "live")
	if err := Validate(validValue(empty, live)); err != nil {
		t.Fatalf("different usage sets may carry different content identity: %v", err)
	}
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

func TestValidateRule7DifferentUsage(t *testing.T) {
	t.Parallel()
	empty := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "gzip",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	live := withUsage(validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-other", "none",
		"a", testContentDigestB, "1", testManifestDigest2, 1,
	), "live")
	if err := Validate(validValue(empty, live)); err != nil {
		t.Fatalf("same filename under different usage sets must be valid: %v", err)
	}
}

// rule8Pair returns two valid descriptors that share a file-manifest digest
// and differ only in architecture, which spec §6 rule 8 permits. The pair is
// already in canonical order.
func rule8Pair() (Descriptor, Descriptor) {
	left := validDescriptor(
		"amd64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	right := validDescriptor(
		"arm64", "x-test-target", "x-test-format", "x-test-file", "none",
		"a", testContentDigestA, "1", testManifestDigest1, 1,
	)
	return left, right
}

// TestValidateRule8 covers spec §6 rule 8: two descriptors naming the same file
// manifest must agree on artifact type, descriptor size, compression, content
// digest, and content size.
//
// Each case mutates one of those fields on an otherwise valid shared pair. The
// pair differs in architecture, which keeps the file key distinct so rule 6 does
// not fire first on the content mutations.
//
// Descriptor media type is the sixth field rule 8 names and has no reachable
// case: rule 2 already requires every descriptor mediaType to identify
// [MediaTypeManifest], and rule 8 compares media types with [equalMediaType],
// so any spelling that survives rule 2 also compares equal under rule 8.
func TestValidateRule8(t *testing.T) {
	t.Parallel()
	base := func() *Value {
		left, right := rule8Pair()
		return validValue(left, right)
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("unmutated shared-digest pair must validate: %v", err)
	}

	tests := []struct {
		name string
		// mutate disagrees with the first descriptor on one rule-8 field.
		mutate func(*Descriptor)
	}{
		{
			name:   "different artifactType",
			mutate: func(d *Descriptor) { d.ArtifactType = ArtifactTypeBigOCI },
		},
		{
			name:   "different descriptor size",
			mutate: func(d *Descriptor) { d.Size = 2 },
		},
		{
			name:   "different compression",
			mutate: func(d *Descriptor) { d.Annotations[AnnotationCompression] = "gzip" },
		},
		{
			name:   "different content digest",
			mutate: func(d *Descriptor) { d.Annotations[AnnotationContentDigest] = testContentDigestB },
		},
		{
			name:   "different content size",
			mutate: func(d *Descriptor) { d.Annotations[AnnotationContentSize] = "2" },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := base()
			tc.mutate(&v.Manifests[1])
			assertRule(t, Validate(v), specRuleSharedManifest)
		})
	}
}

// TestValidateRule8PermittedDifferences covers spec §6 rule 8: descriptors that
// share a file-manifest digest may differ in architecture, target,
// representation, role, and filename.
//
// The role case also changes filename, because rule 7 forbids two roles in one
// deliverable from sharing a filename and would fire first. Filename is
// exercised again across two targets, where rule 7 does not apply. Every pair
// is written in canonical order so rule 9 does not fire first.
func TestValidateRule8PermittedDifferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    *Value
	}{
		{
			name: "architecture",
			v: validValue(
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				validDescriptor("arm64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
			),
		},
		{
			name: "target",
			v: validValue(
				validDescriptor("amd64", "x-test-other-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
			),
		},
		{
			name: "representation",
			v: validValue(
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				validDescriptor("amd64", "x-test-target", "x-test-other-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
			),
		},
		{
			name: "role with distinct filename",
			v: validValue(
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-other", "none",
					"b", testContentDigestA, "1", testManifestDigest1, 1),
			),
		},
		{
			name: "filename across targets",
			v: validValue(
				validDescriptor("amd64", "x-test-other-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"b", testContentDigestA, "1", testManifestDigest1, 1),
			),
		},
		{
			name: "usage",
			v: validValue(
				validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1),
				withUsage(validDescriptor("amd64", "x-test-target", "x-test-format", "x-test-file", "none",
					"a", testContentDigestA, "1", testManifestDigest1, 1), "live"),
			),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(tc.v); err != nil {
				t.Fatalf("permitted shared-manifest difference rejected: %v", err)
			}
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
	if !errors.Is(err, ErrRule) {
		t.Fatalf("error %v does not wrap ErrRule", err)
	}
	want := "spec §6 rule " + strconv.Itoa(rule)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name %s", err, want)
	}
}

// withUsage copies d and sets or deletes io.imgoci.usage.
func withUsage(d Descriptor, usage string) Descriptor {
	d.Annotations = copyStringMap(d.Annotations)
	if usage == "" {
		delete(d.Annotations, AnnotationUsage)
		return d
	}
	d.Annotations[AnnotationUsage] = usage
	return d
}

// withUsageValue copies v and sets usage on manifests[i].
func withUsageValue(v *Value, i int, usage string) *Value {
	out := cloneValue(v)
	out.Manifests[i] = withUsage(out.Manifests[i], usage)
	return out
}
