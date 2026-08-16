package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	imgoci "github.com/imgoci/go"
)

func TestDecodePublishDocumentRejectsUnknownMembers(t *testing.T) {
	t.Parallel()

	_, err := decodePublishDocument([]byte(`{"name":"n","version":"1","extra":true,"files":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extra")
}

func TestDecodePublishDocumentRejectsTrailingData(t *testing.T) {
	t.Parallel()

	_, err := decodePublishDocument([]byte(`{"name":"n","version":"1","files":[]}{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trailing data")
}

func TestDocumentToReleaseSpecRequiresMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  publishDocument
		want string
	}{
		{
			name: "missing name",
			doc:  publishDocument{Version: "1", Files: []publishFile{{Path: "a"}}},
			want: "name is required",
		},
		{
			name: "missing version",
			doc:  publishDocument{Name: "n", Files: []publishFile{{Path: "a"}}},
			want: "version is required",
		},
		{name: "missing files", doc: publishDocument{Name: "n", Version: "1"}, want: "files is required"},
		{
			name: "missing path",
			doc: publishDocument{Name: "n", Version: "1", Files: []publishFile{{
				Architecture: "amd64", Target: "qemu", Representation: "qcow2", Role: "disk", Compression: "none",
			}}},
			want: "path is required",
		},
		{
			name: "missing architecture",
			doc: publishDocument{Name: "n", Version: "1", Files: []publishFile{{
				Path: "a", Target: "qemu", Representation: "qcow2", Role: "disk", Compression: "none",
			}}},
			want: "architecture is required",
		},
		{
			name: "missing compression",
			doc: publishDocument{Name: "n", Version: "1", Files: []publishFile{{
				Path: "a", Architecture: "amd64", Target: "qemu", Representation: "qcow2", Role: "disk",
			}}},
			want: "compression is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := documentToReleaseSpec(tt.doc, t.TempDir())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDocumentToReleaseSpecMapsLosslessly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	doc := publishDocument{
		Name:        "example",
		Version:     "1",
		Annotations: map[string]string{"note": "root"},
		Files: []publishFile{{
			Path:           "disk.qcow2",
			Filename:       "disk.qcow2",
			Architecture:   "amd64",
			Target:         "qemu",
			Representation: "qcow2",
			Role:           "disk",
			Compression:    "gzip",
			Annotations:    map[string]string{"note": "file"},
			Multipart:      &publishMultipart{PartSize: 16 << 20},
		}},
	}

	got, err := documentToReleaseSpec(doc, dir)
	require.NoError(t, err)
	assert.Equal(t, "example", got.Name)
	assert.Equal(t, "1", got.Version)
	assert.Equal(t, map[string]string{"note": "root"}, got.Annotations)
	require.Len(t, got.Files, 1)
	assert.Equal(t, imgoci.FromFile(filepath.Join(dir, "disk.qcow2")), got.Files[0].Source)
	assert.Equal(t, imgoci.Selector{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Role:           "disk",
		Compression:    "gzip",
	}, got.Files[0].Selector)
	assert.Equal(t, "disk.qcow2", got.Files[0].Filename)
	assert.Equal(t, map[string]string{"note": "file"}, got.Files[0].Annotations)
	require.NotNil(t, got.Files[0].Multipart)
	assert.Equal(t, int64(16<<20), got.Files[0].Multipart.PartSize)
}

func TestDocumentToReleaseSpecOmitsMultipartWhenAbsent(t *testing.T) {
	t.Parallel()

	doc := publishDocument{
		Name:    "example",
		Version: "1",
		Files: []publishFile{{
			Path:           "/abs/disk.qcow2",
			Filename:       "disk.qcow2",
			Architecture:   "amd64",
			Target:         "qemu",
			Representation: "qcow2",
			Role:           "disk",
			Compression:    "none",
		}},
	}

	got, err := documentToReleaseSpec(doc, t.TempDir())
	require.NoError(t, err)
	require.Len(t, got.Files, 1)
	assert.Equal(t, imgoci.FromFile("/abs/disk.qcow2"), got.Files[0].Source)
	assert.Nil(t, got.Files[0].Multipart)
	assert.Nil(t, got.Annotations)
	assert.Nil(t, got.Files[0].Annotations)
}

func TestLoadReleaseSpecReadsRelativePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "disk.bin"), []byte("payload"), 0o600))
	specPath := filepath.Join(dir, "spec.json")
	require.NoError(t, os.WriteFile(specPath, []byte(`{
  "name": "example",
  "version": "1",
  "files": [{
    "path": "disk.bin",
    "filename": "disk.bin",
    "architecture": "amd64",
    "target": "qemu",
    "representation": "qcow2",
    "role": "disk",
    "compression": "none"
  }]
}`), 0o600))

	got, err := loadReleaseSpec(specPath)
	require.NoError(t, err)
	assert.Equal(t, imgoci.FromFile(filepath.Join(dir, "disk.bin")), got.Files[0].Source)
}

func TestQueryFlagsMapListAndResolve(t *testing.T) {
	t.Parallel()

	var list queryFlags
	list.architecture = "amd64"
	list.roles.values = []string{"disk", "metadata"}
	assert.Equal(t, imgoci.ListQuery{
		Architecture: "amd64",
		Roles:        []string{"disk", "metadata"},
	}, list.listQuery())

	var resolve queryFlags
	resolve.architecture = "amd64"
	resolve.target = "qemu"
	resolve.representation = "qcow2"
	resolve.compressions.values = []string{"gzip", "none"}
	got, err := resolve.resolveQuery()
	require.NoError(t, err)
	assert.Equal(t, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Compressions:   []string{"gzip", "none"},
	}, got)
	assert.Equal(t, imgoci.Capabilities{}, got.Capabilities)

	resolve.capabilities.values = []string{"application/vnd.imgoci.file.v1", "application/vnd.bigoci.file.v1"}
	got, err = resolve.resolveQuery()
	require.NoError(t, err)
	want, err := imgoci.NewCapabilities(
		"application/vnd.imgoci.file.v1",
		"application/vnd.bigoci.file.v1",
	)
	require.NoError(t, err)
	assert.Equal(t, want, got.Capabilities)
}
