//go:build e2e

package e2e

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"

	imgoci "github.com/imgoci/go"
)

// TestPublishRoundTrip is the self-hosting matrix: Publish, Fetch, Resolve,
// and FetchFiles against zot and CNCF Distribution, every v1 compression the
// producer can emit, and three release shapes.
//
// linux-netboot follows spec §5.4: kernel is required; initramfs and rootfs
// are optional. Nil Roles applies the default-role rule, which returns every
// present role, so the kernel+initramfs pair is fetched together.
func TestPublishRoundTrip(t *testing.T) {
	t.Parallel()
	compressions := []string{"none", "gzip", "xz", "zstd"}
	shapes := []string{"single-role", "linux-netboot", "shared-digest"}
	for _, reg := range e2eRegistries() {
		t.Run(reg.name, func(t *testing.T) {
			t.Parallel()
			host := startRegistry(t, reg.image)
			for _, compression := range compressions {
				t.Run(compression, func(t *testing.T) {
					t.Parallel()
					for _, shape := range shapes {
						t.Run(shape, func(t *testing.T) {
							t.Parallel()
							runPublishRoundTrip(t, host, compression, shape)
						})
					}
				})
			}
		})
	}
}

func runPublishRoundTrip(t *testing.T, host, compression, shape string) {
	t.Helper()
	repo := testRepo(t)
	spec, query, files, shared := roundTripSpec(t, compression, shape)
	client := newE2EClient(t, e2eCreds{})
	published, err := client.Publish(t.Context(), tagRef(host, repo), spec)
	if err != nil {
		t.Fatal(err)
	}

	rel := mustFetch(t, client, tagRef(host, repo))
	if rel.Digest() != published {
		t.Fatalf("Fetch digest %s, want published %s", rel.Digest(), published)
	}

	tagBytes := getIndexRaw(t, host, repo, e2eTag, e2eCreds{})
	digestBytes := getIndexRaw(t, host, repo, published.String(), e2eCreds{})
	if !bytes.Equal(tagBytes, digestBytes) {
		t.Fatal("tag GET and digest GET returned different index bytes")
	}
	if digest.FromBytes(tagBytes) != published {
		t.Fatalf("tagged index digest %s, want published %s", digest.FromBytes(tagBytes), published)
	}

	if shared {
		entries := rel.Index().Entries()
		if len(entries) != 2 {
			t.Fatalf("shared-digest entries = %d, want 2", len(entries))
		}
		if entries[0].Digest != entries[1].Digest {
			t.Fatalf("shared-digest manifests %s and %s, want one digest twice", entries[0].Digest, entries[1].Digest)
		}
	}

	sel := mustResolve(t, client, rel, query)
	dir := t.TempDir()
	mustFetchFiles(t, client, rel, sel, imgoci.ToDir(dir))
	for _, file := range files {
		assertFileContent(t, filepath.Join(dir, file.filename), file.content)
	}

	if shape == "shared-digest" {
		metal := mustResolve(t, client, rel, imgoci.ResolveQuery{
			Architecture:   "amd64",
			Target:         "metal",
			Representation: "raw",
			Compressions:   []string{compression},
		})
		metalDir := t.TempDir()
		mustFetchFiles(t, client, rel, metal, imgoci.ToDir(metalDir))
		assertFileContent(t, filepath.Join(metalDir, "disk.raw"), files[0].content)
	}
}

type roundTripFile struct {
	filename string
	content  []byte
}

func roundTripSpec(
	t *testing.T,
	compression, shape string,
) (imgoci.ReleaseSpec, imgoci.ResolveQuery, []roundTripFile, bool) {
	t.Helper()
	dir := t.TempDir()
	switch shape {
	case "single-role":
		content := repeatingBytes("roundtrip-qcow2-", 1024)
		path := writeStoredSource(t, dir, "disk.qcow2", compression, content)
		spec := imgoci.ReleaseSpec{
			Name:    "e2e",
			Version: "1",
			Files: []imgoci.FileSpec{{
				Source: imgoci.FromFile(path),
				Selector: imgoci.Selector{
					Architecture:   "amd64",
					Target:         "qemu",
					Representation: "qcow2",
					Role:           "disk",
					Compression:    compression,
				},
				Filename: "disk.qcow2",
			}},
		}
		query := imgoci.ResolveQuery{
			Architecture:   "amd64",
			Target:         "qemu",
			Representation: "qcow2",
			Compressions:   []string{compression},
		}
		return spec, query, []roundTripFile{{filename: "disk.qcow2", content: content}}, false
	case "linux-netboot":
		kernel := repeatingBytes("roundtrip-kernel-", 512)
		initramfs := repeatingBytes("roundtrip-initramfs-", 512)
		kernelPath := writeStoredSource(t, dir, "vmlinuz", compression, kernel)
		initramfsPath := writeStoredSource(t, dir, "initramfs.img", compression, initramfs)
		sel := func(role string) imgoci.Selector {
			return imgoci.Selector{
				Architecture:   "amd64",
				Target:         "metal",
				Representation: "linux-netboot",
				Role:           role,
				Compression:    compression,
			}
		}
		spec := imgoci.ReleaseSpec{
			Name:    "e2e",
			Version: "1",
			Files: []imgoci.FileSpec{
				{Source: imgoci.FromFile(kernelPath), Selector: sel("kernel"), Filename: "vmlinuz"},
				{Source: imgoci.FromFile(initramfsPath), Selector: sel("initramfs"), Filename: "initramfs.img"},
			},
		}
		query := imgoci.ResolveQuery{
			Architecture:   "amd64",
			Target:         "metal",
			Representation: "linux-netboot",
			Compressions:   []string{compression},
		}
		return spec, query, []roundTripFile{
			{filename: "vmlinuz", content: kernel},
			{filename: "initramfs.img", content: initramfs},
		}, false
	case "shared-digest":
		content := repeatingBytes("roundtrip-shared-", 1024)
		path := writeStoredSource(t, dir, "shared.bin", compression, content)
		spec := imgoci.ReleaseSpec{
			Name:    "e2e",
			Version: "1",
			Files: []imgoci.FileSpec{
				{
					Source: imgoci.FromFile(path),
					Selector: imgoci.Selector{
						Architecture:   "amd64",
						Target:         "qemu",
						Representation: "qcow2",
						Role:           "disk",
						Compression:    compression,
					},
					Filename: "disk.qcow2",
				},
				{
					Source: imgoci.FromFile(path),
					Selector: imgoci.Selector{
						Architecture:   "amd64",
						Target:         "metal",
						Representation: "raw",
						Role:           "disk",
						Compression:    compression,
					},
					Filename: "disk.raw",
				},
			},
		}
		query := imgoci.ResolveQuery{
			Architecture:   "amd64",
			Target:         "qemu",
			Representation: "qcow2",
			Compressions:   []string{compression},
		}
		return spec, query, []roundTripFile{{filename: "disk.qcow2", content: content}}, true
	default:
		t.Fatalf("unknown shape %q", shape)
		return imgoci.ReleaseSpec{}, imgoci.ResolveQuery{}, nil, false
	}
}

func writeStoredSource(t *testing.T, dir, name, compression string, content []byte) string {
	t.Helper()
	return writeTempBytes(t, dir, storedSourceName(name, compression), compressBytes(t, compression, content))
}
