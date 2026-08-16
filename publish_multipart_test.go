package imgoci

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	mpmocks "github.com/imgoci/go/internal/multipart/mocks"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

func TestPublishMultipartSpecAccepted(t *testing.T) {
	t.Parallel()
	spec := validReleaseSpec(t, []byte("payload"))
	spec.Files[0].Multipart = &MultipartSpec{}
	var constructed int
	c := clientWithTransferPorts(t, &publishManifests{}, &publishBlobs{}, &constructed)
	_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
	if err != nil {
		t.Fatal(err)
	}
	if constructed != 1 {
		t.Fatalf("adapter constructions = %d, want 1", constructed)
	}
}

func TestPublishNegativePartSizeRejectedBeforeIO(t *testing.T) {
	t.Parallel()
	spec := validReleaseSpec(t, []byte("payload"))
	spec.Files[0].Multipart = &MultipartSpec{PartSize: -1}
	var constructed int
	c := clientWithTransferPorts(t, &publishManifests{}, &publishBlobs{}, &constructed)
	_, err := c.Publish(t.Context(), "example.com/os/example:v1", spec)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if !strings.Contains(err.Error(), "multipart part size must be >= 0") {
		t.Fatalf("err = %v", err)
	}
	if constructed != 0 {
		t.Fatal("adapter must not be constructed for a negative part size")
	}
}

func TestPublishMixedStandardAndMultipart(t *testing.T) {
	t.Parallel()
	stdPath := writePublishFile(t, "std.bin", []byte("standard-bytes"))
	mpData := []byte("0123456789abcdef")
	mpPath := writePublishFile(t, "mp.bin", mpData)
	stored := digest.FromBytes(mpData)
	raw := rootBigOCIManifest(t, stored, int64(len(mpData)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, "example.com/os/example", mpPath, int64(8)).Return(desc, nil)

	var indexBody []byte
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, body []byte) error {
			if ref == "v1" {
				indexBody = append([]byte(nil), body...)
			}
			return nil
		})

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Exists(mock.Anything, mock.Anything).Return(false, nil)
	blobs.EXPECT().Push(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			return nil
		})

	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	c.newAdapter = func(string, string, clientSettings) (adapterPorts, error) {
		return adapterPorts{manifests: manifests, blobs: blobs, multipart: mp}, nil
	}

	spec := ReleaseSpec{
		Name:    "example",
		Version: "1",
		Files: []FileSpec{
			{
				Source: FromFile(stdPath),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "file-a",
					Compression:    "none",
				},
				Filename: "a",
			},
			{
				Source: FromFile(mpPath),
				Selector: Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "file-b",
					Compression:    "none",
				},
				Filename:  "b",
				Multipart: &MultipartSpec{PartSize: 8},
			},
		},
	}
	_, err = c.Publish(t.Context(), "example.com/os/example:v1", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexBody) == 0 {
		t.Fatal("index PUT missing")
	}
	v, err := index.Decode(indexBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Manifests) != 2 {
		t.Fatalf("manifests %d, want 2", len(v.Manifests))
	}
	var sawStandard, sawBigOCI bool
	for _, d := range v.Manifests {
		switch {
		case index.EqualMediaType(d.ArtifactType, index.ArtifactTypeFile):
			sawStandard = true
		case index.EqualMediaType(d.ArtifactType, index.ArtifactTypeBigOCI):
			sawBigOCI = true
			if d.Digest != desc.Digest {
				t.Fatalf("bigoci digest %s, want %s", d.Digest, desc.Digest)
			}
		}
	}
	if !sawStandard || !sawBigOCI {
		t.Fatalf("artifact types standard=%v bigoci=%v", sawStandard, sawBigOCI)
	}
}

func TestDefaultAdapterWiresMultipart(t *testing.T) {
	t.Parallel()
	ports, err := defaultAdapter("example.com", "os/example", clientSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if ports.multipart == nil {
		t.Fatal("defaultAdapter must wire the BigOCI adapter")
	}
	if ports.manifests == nil || ports.blobs == nil {
		t.Fatal("defaultAdapter must wire Manifests and Blobs")
	}
}

func rootBigOCIManifest(t *testing.T, fileDigest digest.Digest, size int64) []byte {
	t.Helper()
	part := digest.FromBytes([]byte("part"))
	raw, err := json.Marshal(map[string]any{
		"mediaType":    index.MediaTypeManifest,
		"artifactType": index.ArtifactTypeBigOCI,
		"layers": []any{
			map[string]any{"digest": part.String(), "mediaType": filemanifest.MediaTypePart, "size": 6},
			map[string]any{"digest": part.String(), "mediaType": filemanifest.MediaTypePart, "size": 6},
		},
		"annotations": map[string]any{
			"io.bigoci.file.digest": fileDigest.String(),
			"io.bigoci.file.size":   strconv.FormatInt(size, 10),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
