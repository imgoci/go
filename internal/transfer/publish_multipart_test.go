package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/imgoci/bigoci"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	mpmocks "github.com/imgoci/go/internal/multipart/mocks"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

const publishRepo = "example.com/os/example"

func TestDefaultBigOCIPartSizeMatchesLibrary(t *testing.T) {
	t.Parallel()
	if defaultBigOCIPartSize != int64(bigoci.DefaultPartSize) {
		t.Fatalf("defaultBigOCIPartSize = %d, want bigoci.DefaultPartSize %d",
			defaultBigOCIPartSize, bigoci.DefaultPartSize)
	}
}

func TestPlannedParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		stored, part, want int64
	}{
		{stored: 0, part: 8, want: 0},
		{stored: 1, part: 8, want: 1},
		{stored: 8, part: 8, want: 1},
		{stored: 9, part: 8, want: 2},
		{stored: 16, part: 8, want: 2},
		{stored: 1, part: 0, want: 1},
		{stored: defaultBigOCIPartSize, part: 0, want: 1},
		{stored: defaultBigOCIPartSize + 1, part: 0, want: 2},
	}
	for _, tc := range tests {
		if got := plannedParts(tc.stored, tc.part); got != tc.want {
			t.Fatalf("plannedParts(%d,%d)=%d want %d", tc.stored, tc.part, got, tc.want)
		}
	}
}

func TestPublishMultipartPushGetAndIndex(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdef") // 16 bytes
	path := writeTemp(t, "mp.bin", data)
	stored := digest.FromBytes(data)
	raw := bigOCIManifest(t, stored, int64(len(data)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, path, int64(8), mock.Anything).Return(desc, nil).Once()

	var puts []string
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil).Once()
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, _ []byte) error {
			puts = append(puts, ref)
			return nil
		}).Once()

	blobs := regmocks.NewMockBlobs(t)

	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(puts) != 1 || puts[0] != publishTag {
		t.Fatalf("puts %v, want only index tag", puts)
	}
}

func TestPublishMultipartReportsLatestAbsoluteProgress(t *testing.T) {
	t.Parallel()
	stdData := []byte("standard-bytes")
	mpData := []byte("0123456789abcdef")
	stdPath := writeTemp(t, "std.bin", stdData)
	mpPath := writeTemp(t, "mix.bin", mpData)
	storedMP := digest.FromBytes(mpData)
	raw := bigOCIManifest(t, storedMP, int64(len(mpData)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, mpPath, int64(8), mock.Anything).
		RunAndReturn(func(_ context.Context, _, _ string, _ int64, report func(int64, int)) (ocispec.Descriptor, error) {
			if report != nil {
				report(9, 1)
				report(16, 2)
			}
			return desc, nil
		})

	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Exists(mock.Anything, mock.Anything).Return(false, nil)
	blobs.EXPECT().Push(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			return nil
		})

	var snaps []Progress
	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{
			publishEntry(stdPath, "file-a", compressionNone, "a"),
			multipartEntry(mpPath, "file-b", 8),
		},
		Progress: func(p Progress) { snaps = append(snaps, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) == 0 {
		t.Fatal("expected progress snapshots")
	}
	phases, indexN := publishProgressPhases(t, snaps)
	last := snaps[len(snaps)-1]
	if last.Phase != PhaseIndex || last.CompletedFiles != 2 {
		t.Fatalf("terminal %+v", last)
	}
	if indexN != 1 {
		t.Fatalf("index-phase snapshots %d, want 1", indexN)
	}
	if last.WireBytes != int64(len(stdData))+16 {
		t.Fatalf("WireBytes = %d, want standard plus latest multipart", last.WireBytes)
	}
	if last.Retries != 2 {
		t.Fatalf("Retries = %d, want latest multipart 2", last.Retries)
	}
	wantPhases := []string{PhaseHashing, PhaseUpload, PhaseIndex}
	if len(phases) != len(wantPhases) {
		t.Fatalf("phases %v, want %v", phases, wantPhases)
	}
	for i, phase := range wantPhases {
		if phases[i] != phase {
			t.Fatalf("phases %v, want %v", phases, wantPhases)
		}
	}
}

func TestPublishMultipartDigestMismatchNoIndexPut(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdef")
	path := writeTemp(t, "bad-digest.bin", data)
	raw := bigOCIManifest(t, digest.FromBytes([]byte("other")), int64(len(data)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, path, int64(8), mock.Anything).Return(desc, nil)

	var puts []string
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, _ []byte) error {
			puts = append(puts, ref)
			return nil
		}).Maybe()

	blobs := regmocks.NewMockBlobs(t)
	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 8)},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	for _, ref := range puts {
		if ref == publishTag {
			t.Fatal("index PUT must not run after digest mismatch")
		}
	}
}

func TestPublishMultipartSizeMismatchNoIndexPut(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdef")
	path := writeTemp(t, "bad-size.bin", data)
	stored := digest.FromBytes(data)
	raw := bigOCIManifest(t, stored, int64(len(data))+1)
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, path, int64(8), mock.Anything).Return(desc, nil)

	var puts []string
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, _ []byte) error {
			puts = append(puts, ref)
			return nil
		}).Maybe()

	blobs := regmocks.NewMockBlobs(t)
	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 8)},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	for _, ref := range puts {
		if ref == publishTag {
			t.Fatal("index PUT must not run after size mismatch")
		}
	}
}

func TestPublishMultipartFallbackUnderTwoParts(t *testing.T) {
	t.Parallel()
	data := []byte("tiny")
	path := writeTemp(t, "tiny.bin", data)
	log := &callLog{}
	mp := mpmocks.NewMockMultipart(t)
	ports := recordingPorts(t, log, nil)
	ports.Multipart = mp

	var snaps []Progress
	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:      publishTag,
		Name:     "example",
		Version:  "1",
		Repo:     publishRepo,
		Entries:  []PublishEntry{multipartEntry(path, "x-test-file", 16)},
		Progress: func(p Progress) { snaps = append(snaps, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	mp.AssertNotCalled(t, "Push", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	stored := digest.FromBytes(data)
	pushed := false
	for _, op := range log.snapshot() {
		if op == "push:"+stored.String() {
			pushed = true
		}
	}
	if !pushed {
		t.Fatalf("fallback must push stored blob: %v", log.snapshot())
	}
	last := snaps[len(snaps)-1]
	if last.Fallbacks != 1 {
		t.Fatalf("Fallbacks = %d, want 1 in terminal %+v", last.Fallbacks, last)
	}
	if last.Phase != PhaseIndex {
		t.Fatalf("terminal phase %q", last.Phase)
	}
}

func TestPublishMultipartZeroPartSizeUsesDefaultForPlanning(t *testing.T) {
	t.Parallel()
	data := []byte("small-default")
	path := writeTemp(t, "small.bin", data)
	log := &callLog{}
	mp := mpmocks.NewMockMultipart(t)
	ports := recordingPorts(t, log, nil)
	ports.Multipart = mp

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	mp.AssertNotCalled(t, "Push", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	if plannedParts(int64(len(data)), 0) >= minMultipartParts {
		t.Fatal("fixture must be smaller than two default parts")
	}
}

func TestPublishMixedStandardAndMultipartIndexLast(t *testing.T) {
	t.Parallel()
	stdData := []byte("standard-bytes")
	mpData := []byte("0123456789abcdef")
	stdPath := writeTemp(t, "std.bin", stdData)
	mpPath := writeTemp(t, "mix.bin", mpData)
	storedMP := digest.FromBytes(mpData)
	raw := bigOCIManifest(t, storedMP, int64(len(mpData)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, mpPath, int64(8), mock.Anything).Return(desc, nil)

	log := &callLog{}
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, _ []byte) error {
			log.addPut(ref)
			return nil
		})

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Exists(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest) (bool, error) {
			log.add("exists:" + dgst.String())
			return false, nil
		})
	blobs.EXPECT().Push(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest, _ int64, r io.Reader) error {
			log.add("push:" + dgst.String())
			_, _ = io.Copy(io.Discard, r)
			return nil
		})

	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{
			publishEntry(stdPath, "file-a", compressionNone, "a"),
			multipartEntry(mpPath, "file-b", 8),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	puts := log.putRefs()
	if len(puts) < 2 {
		t.Fatalf("puts %v, want standard manifest + index", puts)
	}
	if puts[len(puts)-1] != publishTag {
		t.Fatalf("index PUT is not last: %v", puts)
	}
	stdMan := manifestRef(t, digest.FromBytes(stdData), int64(len(stdData)))
	foundStd := false
	for _, ref := range puts[:len(puts)-1] {
		if ref == stdMan {
			foundStd = true
		}
		if ref == desc.Digest.String() {
			t.Fatal("multipart path must not Manifests.Put the file manifest")
		}
	}
	if !foundStd {
		t.Fatalf("missing standard manifest PUT in %v", puts)
	}
}

func TestPublishMultipartSkipsEmptyConfig(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdef")
	path := writeTemp(t, "no-empty.bin", data)
	stored := digest.FromBytes(data)
	raw := bigOCIManifest(t, stored, int64(len(data)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, path, int64(8), mock.Anything).Return(desc, nil)

	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil)
	manifests.EXPECT().Put(mock.Anything, publishTag, index.MediaTypeIndex, mock.Anything).Return(nil)
	blobs := regmocks.NewMockBlobs(t)

	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	blobs.AssertNotCalled(t, "Exists", mock.Anything, filemanifest.EmptyConfigDigest)
	blobs.AssertNotCalled(t, "Push", mock.Anything, filemanifest.EmptyConfigDigest, mock.Anything, mock.Anything)
}

func TestPublishMultipartIgnoresWrongDescriptorSize(t *testing.T) {
	t.Parallel()
	data := []byte("0123456789abcdef")
	path := writeTemp(t, "wrong-size.bin", data)
	stored := digest.FromBytes(data)
	raw := bigOCIManifest(t, stored, int64(len(data)))
	desc := ocispec.Descriptor{
		MediaType: index.MediaTypeManifest,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)) + 99,
	}

	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().Push(mock.Anything, publishRepo, path, int64(8), mock.Anything).Return(desc, nil).Once()

	var indexRaw []byte
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Get(mock.Anything, desc.Digest.String(), index.MediaTypeManifest).
		Return(raw, index.MediaTypeManifest, nil).Once()
	manifests.EXPECT().Put(mock.Anything, publishTag, index.MediaTypeIndex, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ string, body []byte) error {
			indexRaw = append([]byte(nil), body...)
			return nil
		}).Once()
	blobs := regmocks.NewMockBlobs(t)

	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs, Multipart: mp}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Repo:    publishRepo,
		Entries: []PublishEntry{multipartEntry(path, "x-test-file", 8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := index.Decode(indexRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Validate(value); err != nil {
		t.Fatal(err)
	}
	if err := index.VerifyCanonical(indexRaw); err != nil {
		t.Fatal(err)
	}
	if len(value.Manifests) != 1 {
		t.Fatalf("manifests %d, want 1", len(value.Manifests))
	}
	if value.Manifests[0].Size != int64(len(raw)) {
		t.Fatalf("index descriptor size %d, want verified document %d (not adapter %d)",
			value.Manifests[0].Size, len(raw), desc.Size)
	}
	if value.Manifests[0].Digest != desc.Digest {
		t.Fatalf("index descriptor digest %s, want %s", value.Manifests[0].Digest, desc.Digest)
	}
}

func TestPublishMultipartPartCeilingBeforeNetwork(t *testing.T) {
	t.Parallel()
	const (
		eightGiB int64 = 8 << 30
		oneMiB   int64 = 1 << 20
	)
	mp := mpmocks.NewMockMultipart(t)
	manifests := regmocks.NewMockManifests(t)
	blobs := regmocks.NewMockBlobs(t)

	err := checkMultipartPartCeiling(
		[]PublishEntry{multipartEntry("synthetic-8gib.bin", "x-test-file", oneMiB)},
		func(string) (int64, error) { return eightGiB, nil },
	)
	if err == nil {
		t.Fatal("expected part-ceiling error")
	}
	if !errors.Is(err, index.ErrRule) {
		t.Fatalf("error %v is not index.ErrRule", err)
	}
	if plannedParts(eightGiB, oneMiB) <= maxBigOCIParts {
		t.Fatalf("fixture must exceed the part ceiling")
	}
	mp.AssertNotCalled(t, "Push", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	manifests.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
	manifests.AssertNotCalled(t, "Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	blobs.AssertNotCalled(t, "Exists", mock.Anything, mock.Anything)
	blobs.AssertNotCalled(t, "Push", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func multipartEntry(path, role string, partSize int64) PublishEntry {
	e := publishEntry(path, role, compressionNone, role)
	e.Multipart = &MultipartPlan{PartSize: partSize}
	return e
}

func bigOCIManifest(t *testing.T, fileDigest digest.Digest, size int64) []byte {
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
