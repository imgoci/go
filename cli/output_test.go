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
		"amd64\tx-test-target\tx-test-format\tx-test-file\tgzip\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tx-test-target\tx-test-format\tx-test-file\tnone\tapplication/vnd.imgoci.file.v1\n"+
		"amd64\tx-test-target\tx-test-format\tx-test-file\tzstd\tapplication/vnd.bigoci.file.v1\n",
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
		"amd64\tincus\tincus-vm\tdisk\tnone\tdisk.qcow2\tapplication/vnd.imgoci.file.v1\t"+
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t0\n"+
		"amd64\tincus\tincus-vm\tmetadata\tnone\tmetadata.tar.xz\tapplication/vnd.imgoci.file.v1\t"+
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\t0\n",
		buf.String(),
	)
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
