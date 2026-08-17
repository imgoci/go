package imgoci

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "no values is the empty set", want: ""},
		{name: "nil slice is the empty set", values: nil, want: ""},
		{name: "empty slice is the empty set", values: []string{}, want: ""},
		{name: "single token", values: []string{"live"}, want: "live"},
		{
			name:   "unsorted input is canonicalized",
			values: []string{"live", "install", "install-offline"},
			want:   "install,install-offline,live",
		},
		{
			name:   "duplicates collapse",
			values: []string{"install", "install", "install-offline"},
			want:   "install,install-offline",
		},
		{name: "unknown token is accepted", values: []string{"custom-usage"}, want: "custom-usage"},
		{name: "private token is accepted", values: []string{"x-owner-name"}, want: "x-owner-name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewUsage(tc.values...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestNewUsageRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []string
	}{
		{name: "uppercase token", values: []string{"Live"}},
		{name: "empty token", values: []string{""}},
		{name: "comma inside a token", values: []string{"install,live"}},
		{name: "whitespace", values: []string{"live "}},
		{name: "token of 129 bytes", values: []string{strings.Repeat("a", 129)}},
		{name: "install-offline without install", values: []string{"install-offline"}},
		{name: "install-offline with only unrelated values", values: []string{"install-offline", "live"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewUsage(tc.values...)
			require.Error(t, err)
			assert.Equal(t, Usage{}, got, "a failed NewUsage must not return a partial set")
			assert.Contains(t, err.Error(), "usage")
		})
	}
}

// TestNewUsageRejectsOversizedSet pins the spec section 5.3 4096-byte bound on
// the serialized set, which no single valid token can reach on its own.
func TestNewUsageRejectsOversizedSet(t *testing.T) {
	t.Parallel()
	tokens := make([]string, 0, 33)
	for i := range 33 {
		tokens = append(tokens, fmt.Sprintf("%02d", i)+strings.Repeat("b", 126))
	}
	_, err := NewUsage(tokens...)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4096")
}

func TestUsageValues(t *testing.T) {
	t.Parallel()
	u, err := NewUsage("live", "install", "install-offline")
	require.NoError(t, err)
	assert.Equal(t, []string{"install", "install-offline", "live"}, u.Values())
	assert.Nil(t, Usage{}.Values(), "the empty set has no values")

	values := u.Values()
	values[0] = "mutated"
	assert.Equal(t, []string{"install", "install-offline", "live"}, u.Values(),
		"Values must return a fresh slice on every call")
}

// TestUsageIsComparable pins the property the whole design rests on: Usage is
// a comparable value, so Selector stays usable as a map key and equal sets
// compare equal regardless of the order they were built from.
func TestUsageIsComparable(t *testing.T) {
	t.Parallel()
	first, err := NewUsage("install", "live")
	require.NoError(t, err)
	second, err := NewUsage("live", "install", "live")
	require.NoError(t, err)
	// The == operator, not assert.Equal, is the property under test: Usage must
	// stay comparable so Selector can be a map key.
	sameSet := first == second
	assert.True(t, sameSet, "equal sets must compare equal")

	other, err := NewUsage("live")
	require.NoError(t, err)
	differentSet := first != other
	assert.True(t, differentSet, "different sets must not compare equal")

	seen := map[Selector]int{}
	seen[Selector{Architecture: "amd64", Usage: first}] = 1
	seen[Selector{Architecture: "amd64", Usage: second}] = 2
	assert.Len(t, seen, 1, "selectors with equal usage sets must share a map key")
	seen[Selector{Architecture: "amd64", Usage: other}] = 3
	assert.Len(t, seen, 2)
}

// TestParseIndexCarriesUsage proves the annotation survives the whole read
// path: canonical bytes to Decode to Validate to the public FileEntry.
func TestParseIndexCarriesUsage(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"annotations":{"io.imgoci.name":"example",` +
		`"org.opencontainers.image.version":"1"},` +
		`"artifactType":"application/vnd.imgoci.release.v1","manifests":[` +
		usageTestDescriptor("", testUsageManifestDigest1, testUsageContentDigestA) + `,` +
		usageTestDescriptor("install,install-offline", testUsageManifestDigest2, testUsageContentDigestB) +
		`],"mediaType":"application/vnd.oci.image.index.v1+json","schemaVersion":2}`)

	idx, err := ParseIndex(raw)
	require.NoError(t, err)

	entries := idx.Entries()
	require.Len(t, entries, 2)
	assert.Equal(t, Usage{}, entries[0].Selector.Usage, "an absent annotation is the empty set")
	assert.Empty(t, entries[0].Selector.Usage.String())

	want, err := NewUsage("install-offline", "install")
	require.NoError(t, err)
	assert.Equal(t, want, entries[1].Selector.Usage)
	assert.Equal(t, []string{"install", "install-offline"}, entries[1].Selector.Usage.Values())
}

const (
	testUsageManifestDigest1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testUsageManifestDigest2 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testUsageContentDigestA  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testUsageContentDigestB  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// usageTestDescriptor renders one canonical file-entry descriptor. An empty
// usage omits the annotation, which is how the empty set is encoded.
func usageTestDescriptor(usage, manifestDigest, contentDigest string) string {
	annotations := `{"io.imgoci.architecture":"amd64","io.imgoci.compression":"none",` +
		`"io.imgoci.content.digest":"` + contentDigest + `","io.imgoci.content.size":"0",` +
		`"io.imgoci.filename":"disk.bin","io.imgoci.representation":"x-test-format",` +
		`"io.imgoci.role":"disk","io.imgoci.target":"x-test-target"`
	if usage != "" {
		annotations += `,"io.imgoci.usage":"` + usage + `"`
	}
	annotations += `}`

	return `{"annotations":` + annotations +
		`,"artifactType":"application/vnd.imgoci.file.v1","digest":"` + manifestDigest +
		`","mediaType":"application/vnd.oci.image.manifest.v1+json","size":1}`
}
