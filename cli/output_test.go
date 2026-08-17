package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	imgoci "github.com/imgoci/go"
)

func TestWriteDeliverablesIsDeterministic(t *testing.T) {
	t.Parallel()

	idx := mustParseIndexFile(t, "../testdata/canonical/pass/multiple-transport-alternatives.json")
	deliverables, err := idx.List(imgoci.ListQuery{})
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, writeDeliverables(&buf, deliverables))
	assert.Equal(t, ""+
		"amd64\tx-test-target\tx-test-format\t\tx-test-file\tgzip\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tx-test-target\tx-test-format\t\tx-test-file\tnone\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tx-test-target\tx-test-format\t\tx-test-file\tzstd\tapplication/vnd.bigoci.file.v1\n",
		buf.String(),
	)
}

func TestWriteDeliverablesEmptyMatchPrintsNothing(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, writeDeliverables(&buf, nil))
	assert.Empty(t, buf.String())
}

func TestWriteResolvedIsDeterministic(t *testing.T) {
	t.Parallel()

	idx := mustParseIndexFile(t, "../testdata/canonical/pass/incus-vm.json")
	sel, err := idx.Resolve(imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "incus",
		Representation: "incus-vm",
		Compressions:   []string{"none"},
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, writeResolved(&buf, sel))
	assert.Equal(t, ""+
		"amd64\tincus\tincus-vm\t\tdisk\tnone\tdisk.qcow2\tapplication/vnd.imgoci.file.v1\t"+
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t0\n"+
		"amd64\tincus\tincus-vm\t\tmetadata\tnone\tmetadata.tar.xz\tapplication/vnd.imgoci.file.v1\t"+
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\t0\n",
		buf.String(),
	)
}

func TestWriteDeliverablesRendersUsageColumn(t *testing.T) {
	t.Parallel()

	empty, err := imgoci.NewUsage()
	require.NoError(t, err)
	compound, err := imgoci.NewUsage("install-offline", "install")
	require.NoError(t, err)

	alt := imgoci.TransportAlternative{
		Compression:  "none",
		ArtifactType: "application/vnd.imgoci.file.v1",
	}
	deliverables := []imgoci.Deliverable{
		{
			Architecture:   "amd64",
			Target:         "metal",
			Representation: "iso",
			Usage:          empty,
			Roles: []imgoci.DeliverableRole{{
				Role:         "disk",
				Alternatives: []imgoci.TransportAlternative{alt},
			}},
		},
		{
			Architecture:   "amd64",
			Target:         "metal",
			Representation: "iso",
			Usage:          compound,
			Roles: []imgoci.DeliverableRole{{
				Role:         "disk",
				Alternatives: []imgoci.TransportAlternative{alt},
			}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, writeDeliverables(&buf, deliverables))
	assert.Equal(t, ""+
		"amd64\tmetal\tiso\t\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tmetal\tiso\tinstall,install-offline\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n",
		buf.String(),
	)
	assertTSVColumnCount(t, buf.String(), 7)
}

func TestWriteResolvedRendersUsageColumn(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"annotations":{"io.imgoci.name":"example",` +
		`"org.opencontainers.image.version":"1"},` +
		`"artifactType":"application/vnd.imgoci.release.v1","manifests":[` +
		cliUsageDescriptor("", "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") + `,` +
		cliUsageDescriptor("install,install-offline",
			"sha256:2222222222222222222222222222222222222222222222222222222222222222",
			"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") +
		`],"mediaType":"application/vnd.oci.image.index.v1+json","schemaVersion":2}`)
	idx, err := imgoci.ParseIndex(raw)
	require.NoError(t, err)

	empty, err := idx.Resolve(imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Compressions:   []string{"none"},
	})
	require.NoError(t, err)
	var emptyBuf bytes.Buffer
	require.NoError(t, writeResolved(&emptyBuf, empty))
	assert.Equal(t, ""+
		"amd64\tx-test-target\tx-test-format\t\tdisk\tnone\tdisk.bin\tapplication/vnd.imgoci.file.v1\t"+
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t0\n",
		emptyBuf.String(),
	)
	assertTSVColumnCount(t, emptyBuf.String(), 10)

	compound, err := idx.Resolve(imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Usage:          []string{"install", "install-offline"},
		Compressions:   []string{"none"},
	})
	require.NoError(t, err)
	var compoundBuf bytes.Buffer
	require.NoError(t, writeResolved(&compoundBuf, compound))
	assert.Equal(t, ""+
		"amd64\tx-test-target\tx-test-format\tinstall,install-offline\tdisk\tnone\tdisk.bin\t"+
		"application/vnd.imgoci.file.v1\t"+
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\t0\n",
		compoundBuf.String(),
	)
	assertTSVColumnCount(t, compoundBuf.String(), 10)
}

func TestRenderProgressHasEveryField(t *testing.T) {
	t.Parallel()

	line := renderProgress(imgoci.Progress{
		Direction:      "publish",
		Phase:          "upload",
		TotalFiles:     2,
		CompletedFiles: 1,
		TotalBytes:     100,
		CompletedBytes: 40,
		WireBytes:      41,
		Retries:        3,
		Fallbacks:      1,
	}, progressPrecision)
	assert.Equal(t,
		"imgoci: progress publish upload pct=40 files=1/2 bytes=40/100 wire=41 retries=3 fallbacks=1 elapsed=100ms\n",
		line,
	)
	assert.True(t, strings.HasPrefix(line, "imgoci: "))
}

// mustParseIndexFile parses a canonical index fixture or fails the test.
func mustParseIndexFile(t *testing.T, path string) *imgoci.Index {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	idx, err := imgoci.ParseIndex(raw)
	require.NoError(t, err)

	return idx
}

// cliUsageDescriptor renders one canonical file-entry descriptor. An empty
// usage omits the annotation, which is how the empty set is encoded.
func cliUsageDescriptor(usage, manifestDigest, contentDigest string) string {
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

func assertTSVColumnCount(t *testing.T, listing string, want int) {
	t.Helper()

	for line := range strings.SplitSeq(strings.TrimSuffix(listing, "\n"), "\n") {
		assert.Len(t, strings.Split(line, "\t"), want, line)
	}
}
