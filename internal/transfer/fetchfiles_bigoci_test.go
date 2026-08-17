package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/file"
	"github.com/imgoci/go/internal/index"
	mpmocks "github.com/imgoci/go/internal/multipart/mocks"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

const testRepo = "example.com/test"

func TestFetchFilesBigOCIHappyGzip(t *testing.T) {
	t.Parallel()
	content := []byte("hello imgoci bigoci gzip")
	fx := bigociGzipFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(fx.stored)).Once()

	var (
		mu    sync.Mutex
		snaps []Progress
	)
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
		Progress: func(p Progress) {
			mu.Lock()
			snaps = append(snaps, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("dest %q, want %q", got, content)
	}
	assertProgressMonotoneTerminal(t, snaps, int64(len(content)))
	assertCacheEntryAbsent(t, filepath.Dir(dest), digest.FromBytes(fx.stored))
	blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
}

func TestFetchFilesBigOCIReportsLatestAbsoluteProgress(t *testing.T) {
	t.Parallel()
	content := []byte("hello imgoci bigoci gzip")
	fx := bigociGzipFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ digest.Digest, path string, report func(int64, int)) error {
			if report != nil {
				report(11, 1)
				report(17, 3)
			}
			return writePullTo(fx.stored)(context.Background(), testRepo, fx.entry.Digest, path, report)
		}).Once()

	var (
		mu    sync.Mutex
		snaps []Progress
	)
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
		Progress: func(p Progress) {
			mu.Lock()
			snaps = append(snaps, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProgressMonotoneTerminal(t, snaps, int64(len(content)))
	last := snaps[len(snaps)-1]
	if last.WireBytes != 17 {
		t.Fatalf("WireBytes = %d, want latest 17 not a sum", last.WireBytes)
	}
	if last.Retries != 3 {
		t.Fatalf("Retries = %d, want latest 3 not a sum", last.Retries)
	}
}

func TestFetchFilesNilProgressSkipsMultipartReport(t *testing.T) {
	t.Parallel()
	content := []byte("quiet-bigoci")
	fx := bigociNoneFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ digest.Digest, path string, report func(int64, int)) error {
			if report != nil {
				t.Error("nil Progress must not install a multipart report")
			}
			return writePullTo(fx.stored)(context.Background(), testRepo, fx.entry.Digest, path, report)
		}).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchFilesBigOCIWrongStoredDigest(t *testing.T) {
	t.Parallel()
	fx := bigociNoneFixture(t, "disk", []byte("payload"))
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo([]byte("wrong-stored-bytes"))).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err == nil {
		t.Fatal("expected digest error")
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	assertAbsent(t, dest)
	assertStoredDirRetained(t, filepath.Dir(dest))
	blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
}

// TestFetchFilesBigOCIOnePartProfile rejects the committed one-part artifact.
// The fixture is a valid BigOCI v1 file, so the part count spec §8 rule 2
// requires is the only thing left for the imgoci profile to reject.
func TestFetchFilesBigOCIOnePartProfile(t *testing.T) {
	t.Parallel()
	fx := loadBigOCIArtifact(t, bigOCIFixtureOnePart)
	entry := bigociEntry("disk", fx.stored, fx.stored, compressionNone, fx.manifest)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err == nil {
		t.Fatal("expected profile error")
	}
	if !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("error %v is not ErrInvalidDocument", err)
	}
	if !strings.Contains(err.Error(), "at least 2 parts") {
		t.Fatalf("error %v does not reject the fixture for its part count", err)
	}
	mp.AssertNotCalled(t, "PullTo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	assertAbsent(t, dest)
}

func TestFetchFilesBigOCISharedFileDigestSequential(t *testing.T) {
	t.Parallel()
	content := []byte("shared-stored-bytes")
	disk := bigociNoneFixture(t, "disk", content)
	kernel := disk
	kernel.entry.Role = "kernel"
	kernel.entry.Filename = "kernel.img"

	dir := t.TempDir()
	diskPath := filepath.Join(dir, "disk.img")
	kernelPath := filepath.Join(dir, "kernel.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, disk.entry.Digest.String(), disk.entry.MediaType).
		Return(disk.manifest, index.MediaTypeManifest, nil).Times(2)
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, disk.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(disk.stored)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{disk.entry, kernel.entry},
		ByRole:     map[string]string{"disk": diskPath, "kernel": kernelPath},
		Workers:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, diskPath, content)
	assertFileBytes(t, kernelPath, content)
}

func TestFetchFilesBigOCIConcurrentSameSelection(t *testing.T) {
	t.Parallel()
	content := []byte("concurrent-stored-bytes")
	fx := bigociNoneFixture(t, "disk", content)
	dir := t.TempDir()
	dest := filepath.Join(dir, "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Times(2)
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ digest.Digest, path string, _ func(int64, int)) error {
			time.Sleep(settleWorkers)
			return writePullTo(fx.stored)(context.Background(), testRepo, fx.entry.Digest, path, nil)
		}).Once()

	var verified atomic.Int32
	releaseCommit := make(chan struct{})
	progress := func(p Progress) {
		if p.Phase != PhaseStaging || p.CompletedFiles != 1 {
			return
		}
		if verified.Add(1) == 2 {
			close(releaseCommit)
		}
		select {
		case <-releaseCommit:
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for both fetches to verify")
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	run := func() {
		defer wg.Done()
		errCh <- FetchFiles(t.Context(), FetchFilesRequest{
			Manifests:  m,
			Blobs:      blobs,
			Multipart:  mp,
			Repository: testRepo,
			Entries:    []Entry{fx.entry},
			ByRole:     map[string]string{"disk": dest},
			Workers:    1,
			Progress:   progress,
		})
	}
	wg.Add(2)
	go run()
	go run()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertFileBytes(t, dest, content)
}

func TestFetchFilesBigOCIFilenamesLookLikeCacheEntries(t *testing.T) {
	t.Parallel()
	a := bigociNoneFixture(t, "disk", []byte("role-a-bytes"))
	stored := bigociNoneFixture(t, "kernel", []byte("role-stored-bytes"))
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a")
	storedPath := filepath.Join(dir, "a.imgoci-stored")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, a.entry.Digest.String(), a.entry.MediaType).
		Return(a.manifest, index.MediaTypeManifest, nil).Once()
	m.EXPECT().Get(mock.Anything, stored.entry.Digest.String(), stored.entry.MediaType).
		Return(stored.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, a.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(a.stored)).Once()
	mp.EXPECT().PullTo(mock.Anything, testRepo, stored.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(stored.stored)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{a.entry, stored.entry},
		ByRole:     map[string]string{"disk": aPath, "kernel": storedPath},
		Workers:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, aPath, a.content)
	assertFileBytes(t, storedPath, stored.content)
	if strings.Contains(storedPath, string(filepath.Separator)+".imgoci-stage"+string(filepath.Separator)) {
		t.Fatal("destination must not live under the reserved staging entry")
	}
}

func TestFetchFilesBigOCIPreplantedWrongCacheRepulled(t *testing.T) {
	t.Parallel()
	content := []byte("correct-stored-bytes")
	fx := bigociNoneFixture(t, "disk", content)
	dir := t.TempDir()
	dest := filepath.Join(dir, "disk.img")
	plantStored(t, dir, digest.FromBytes(fx.stored), []byte("poisoned-cache-bytes"))

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(fx.stored)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, dest, content)
}

func TestFetchFilesBigOCINilMultipart(t *testing.T) {
	t.Parallel()
	fx := bigociNoneFixture(t, "disk", []byte("payload"))
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err == nil {
		t.Fatal("expected wiring error")
	}
	if !strings.Contains(err.Error(), "bigoci retrieval not configured") {
		t.Fatalf("error %v does not name the missing port", err)
	}
	assertAbsent(t, dest)
	blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
}

func TestFetchFilesBigOCINonePrecheckMismatch(t *testing.T) {
	t.Parallel()
	fx := bigociNoneFixture(t, "disk", []byte("payload"))
	entry := fx.entry
	entry.ContentDigest = digest.FromBytes([]byte("nope"))

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)

	dest := filepath.Join(t.TempDir(), "disk.img")
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err == nil {
		t.Fatal("expected precheck error")
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	mp.AssertNotCalled(t, "PullTo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	assertAbsent(t, dest)
}

// TestFetchFilesBigOCICommittedFixtureIgnoresTitle retrieves the committed
// two-part artifact and requires the decoded output to be named from
// io.imgoci.filename.
//
// The fixture carries an OCI title that differs from that filename. Spec §8
// says a BigOCI title is informational and has no imgoci meaning, so nothing
// anywhere under the destination parent may be written under the title.
func TestFetchFilesBigOCICommittedFixtureIgnoresTitle(t *testing.T) {
	t.Parallel()
	fx := loadBigOCIArtifact(t, bigOCIFixtureTwoPart)
	entry := bigociEntry("disk", fx.stored, fx.stored, compressionNone, fx.manifest)
	if fx.title == "" || fx.title == entry.Filename {
		t.Fatalf("fixture title %q must differ from the imgoci filename %q", fx.title, entry.Filename)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, entry.Filename)

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(fx.stored)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFileBytes(t, dest, fx.stored)
	assertNoFileNamed(t, dir, fx.title)
}

// TestFetchFilesBigOCIAssembledDigestMismatch serves an assembled stored file
// of exactly the declared length whose bytes hash differently.
//
// Spec §8 rule 2 makes the whole-file digest and the whole-file size two
// independent checks. The served length equals io.bigoci.file.size, so the
// size check passes and whole-file digest re-verification
// ([file.ErrCacheVerify]) is the only check that can reject it.
func TestFetchFilesBigOCIAssembledDigestMismatch(t *testing.T) {
	t.Parallel()
	fx := loadBigOCIArtifact(t, bigOCIFixtureTwoPart)
	served := bytes.Clone(fx.stored)
	served[len(served)-1] ^= 0xff
	entry := bigociEntry("disk", fx.stored, fx.stored, compressionNone, fx.manifest)
	if int64(len(served)) != entry.ContentSize {
		t.Fatalf("served %d bytes, want the declared %d", len(served), entry.ContentSize)
	}
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(served)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	if !errors.Is(err, file.ErrCacheVerify) {
		t.Fatalf("error %v is not the assembled whole-file digest failure", err)
	}
	assertAbsent(t, dest)
}

// TestFetchFilesBigOCIAssembledSizeMismatch serves the true stored file
// against a manifest whose io.bigoci.file.size is one byte too large.
//
// The whole-file digest still matches the assembled bytes, so digest
// re-verification succeeds ([file.ErrCacheVerify] is absent) and only the size
// half of the spec §8 rule 2 check can fail. Compression is gzip because under
// "none" the stored-equals-content precheck would fail first.
func TestFetchFilesBigOCIAssembledSizeMismatch(t *testing.T) {
	t.Parallel()
	content := []byte("bigoci-assembled-size-boundary")
	stored := gzipBytes(t, content)
	manifest := bigOCIManifestWithAnnotation(
		t,
		mustBigOCIManifest(t, stored),
		annotationBigOCIFileSize,
		strconv.FormatInt(int64(len(stored))+1, decimalBase),
	)
	entry := bigociEntry("disk", content, stored, "gzip", manifest)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(stored)).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	if !strings.Contains(err.Error(), "stored file") {
		t.Fatalf("error %v is not the assembled whole-file failure", err)
	}
	if errors.Is(err, file.ErrCacheVerify) {
		t.Fatalf("error %v failed the digest check, not the size check", err)
	}
	assertAbsent(t, dest)
}

// TestFetchFilesBigOCINonePrecheckSizeMismatch moves only the
// io.bigoci.file.size annotation.
//
// Spec §8 requires the BigOCI whole-file digest and size to equal the imgoci
// content digest and size when compression is "none".
// [TestFetchFilesBigOCINonePrecheckMismatch] moves the digest; here the digest
// annotation still equals the entry ContentDigest, so the size half of that
// equality is the only check that can fail. Nothing is pulled.
func TestFetchFilesBigOCINonePrecheckSizeMismatch(t *testing.T) {
	t.Parallel()
	fx := loadBigOCIArtifact(t, bigOCIFixtureTwoPart)
	manifest := bigOCIManifestWithAnnotation(
		t,
		fx.manifest,
		annotationBigOCIFileSize,
		strconv.FormatInt(int64(len(fx.stored))+1, decimalBase),
	)
	entry := bigociEntry("disk", fx.stored, fx.stored, compressionNone, manifest)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	if !strings.Contains(err.Error(), "none precheck") {
		t.Fatalf("error %v is not the compression=none equality failure", err)
	}
	mp.AssertNotCalled(t, "PullTo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	assertAbsent(t, dest)
}

func TestFetchFilesBigOCICommitSucceedsWhenCacheLockHeld(t *testing.T) {
	t.Parallel()
	content := []byte("lock-held-after-commit")
	fx := bigociNoneFixture(t, "disk", content)
	dir := t.TempDir()
	dest := filepath.Join(dir, "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(fx.stored)).Once()

	key := digest.FromBytes(fx.stored)
	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	var (
		mu    sync.Mutex
		snaps []Progress
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	err := FetchFiles(ctx, FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
		Progress: func(p Progress) {
			mu.Lock()
			snaps = append(snaps, p)
			mu.Unlock()
			if p.Phase != PhaseStaging || p.CompletedFiles != 1 {
				return
			}
			cache, cacheErr := file.NewStoredCache(dir)
			if cacheErr != nil {
				t.Errorf("NewStoredCache: %v", cacheErr)
				return
			}
			go func() {
				holderDone <- cache.With(context.Background(), key, func(string) error {
					return errors.New("holder must reuse the committed cache entry")
				}, func(string) error {
					close(held)
					<-release
					return nil
				})
			}()
			select {
			case <-held:
			case <-time.After(5 * time.Second):
				t.Error("timed out waiting to hold cache lock")
				return
			}
			cancel()
		},
	})
	if err != nil {
		t.Fatalf("committed fetch failed while cache lock held: %v", err)
	}
	assertFileBytes(t, dest, content)
	assertProgressMonotoneTerminal(t, snaps, int64(len(content)))
	close(release)
	if holdErr := <-holderDone; holdErr != nil {
		t.Fatal(holdErr)
	}
}

func TestFetchFilesBigOCIDecodeCanceled(t *testing.T) {
	t.Parallel()
	content := bytes.Repeat([]byte("x"), 1<<20)
	fx := bigociNoneFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, fx.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ digest.Digest, path string, _ func(int64, int)) error {
			if err := os.WriteFile(path, fx.stored, 0o600); err != nil {
				return err
			}
			cancel()
			return nil
		}).Once()

	started := time.Now()
	err := FetchFiles(ctx, FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{fx.entry},
		ByRole:     map[string]string{"disk": dest},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v is not context.Canceled", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("decode cancel took %s, want prompt return", time.Since(started))
	}
	assertAbsent(t, dest)
}

func TestFetchFilesBigOCIFirstErrorAbortsStoredDecode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := bigociNoneFixture(t, "disk", bytes.Repeat([]byte("y"), 1<<16))
	bad := noneFixture(t, "kernel", []byte("kernel-bytes"))
	diskPath := filepath.Join(dir, "disk.img")
	kernelPath := filepath.Join(dir, "kernel.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, good.entry.Digest.String(), good.entry.MediaType).
		Return(good.manifest, index.MediaTypeManifest, nil).Maybe()
	m.EXPECT().Get(mock.Anything, bad.entry.Digest.String(), bad.entry.MediaType).
		Return([]byte("not-the-manifest"), index.MediaTypeManifest, nil).Once()

	blobs := regmocks.NewMockBlobs(t)
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, good.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, _ digest.Digest, path string, _ func(int64, int)) error {
			if err := os.WriteFile(path, good.stored, 0o600); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return errors.New("stored decode was not canceled")
			}
		}).Maybe()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{good.entry, bad.entry},
		ByRole:     map[string]string{"disk": diskPath, "kernel": kernelPath},
		Workers:    2,
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	assertAbsent(t, diskPath)
	assertAbsent(t, kernelPath)
}

func TestMapStoredCacheErrReword(t *testing.T) {
	t.Parallel()
	key := digest.FromBytes([]byte("x"))
	wrapped := fmt.Errorf(
		"file: stored cache fetch for %s failed after a complete reword: %w",
		key,
		file.ErrCacheVerify,
	)
	got := mapStoredCacheErr(wrapped)
	if !errors.Is(got, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", got)
	}
	if !errors.Is(got, file.ErrCacheVerify) {
		t.Fatalf("error %v is not file.ErrCacheVerify", got)
	}
	substringOnly := errors.New("file: stored cache fetch for " + key.String() + " failed digest re-verification")
	if mapped := mapStoredCacheErr(substringOnly); errors.Is(mapped, ErrDigestMismatch) {
		t.Fatalf("substring-only error mapped to ErrDigestMismatch: %v", mapped)
	}
}

func bigociNoneFixture(t *testing.T, role string, content []byte) fileFixture {
	t.Helper()
	return newBigOCIFixture(t, role, content, content, compressionNone)
}

func bigociGzipFixture(t *testing.T, role string, content []byte) fileFixture {
	t.Helper()
	return newBigOCIFixture(t, role, content, gzipBytes(t, content), "gzip")
}

func newBigOCIFixture(t *testing.T, role string, content, stored []byte, compression string) fileFixture {
	t.Helper()
	manifest := mustBigOCIManifest(t, stored)
	return fileFixture{
		role:     role,
		content:  content,
		stored:   stored,
		manifest: manifest,
		layer:    digest.FromBytes(stored),
		entry:    bigociEntry(role, content, stored, compression, manifest),
	}
}

func bigociEntry(role string, content, _ []byte, compression string, manifest []byte) Entry {
	return Entry{
		Role:          role,
		MediaType:     index.MediaTypeManifest,
		ArtifactType:  index.ArtifactTypeBigOCI,
		Compression:   compression,
		Digest:        digest.FromBytes(manifest),
		Size:          int64(len(manifest)),
		ContentDigest: digest.FromBytes(content),
		ContentSize:   int64(len(content)),
		Filename:      role + ".img",
	}
}

// mustBigOCIManifest builds a valid two-part BigOCI v1 manifest for stored.
// The part size halves the stored file, so every runtime unit fixture has the
// at-least-two-parts shape spec §8 rule 2 requires.
// [TestBigOCIFixturesAreValidBigOCIV1] pins this encoder to the committed
// artifacts under testdata/bigoci/v1.
func mustBigOCIManifest(t *testing.T, stored []byte) []byte {
	t.Helper()
	return bigOCIManifestBytes(t, stored, bigOCIHalfPartSize(len(stored)), bigOCITitle)
}

// assertNoFileNamed requires no entry under root to be named name.
func assertNoFileNamed(t *testing.T, root, name string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == name {
			return fmt.Errorf("%s was written from the BigOCI title", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writePullTo(payload []byte) func(context.Context, string, digest.Digest, string, func(int64, int)) error {
	return func(_ context.Context, _ string, _ digest.Digest, path string, _ func(int64, int)) error {
		return os.WriteFile(path, payload, 0o600)
	}
}

func plantStored(t *testing.T, parent string, key digest.Digest, contents []byte) {
	t.Helper()
	dir := filepath.Join(parent, ".imgoci-stage", "stored")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sha256-"+key.Encoded()), contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q, want %q", path, got, want)
	}
}

func assertCacheEntryAbsent(t *testing.T, parent string, key digest.Digest) {
	t.Helper()
	path := filepath.Join(parent, ".imgoci-stage", "stored", "sha256-"+key.Encoded())
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("cache entry %s retained after successful commit", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertStoredDirRetained(t *testing.T, parent string) {
	t.Helper()
	dir := filepath.Join(parent, ".imgoci-stage", "stored")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stored cache directory should remain after failure: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("stored cache path %s is not a directory", dir)
	}
}
