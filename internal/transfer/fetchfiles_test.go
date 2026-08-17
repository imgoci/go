package transfer

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"maps"
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

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/file"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	"github.com/imgoci/go/internal/jcs"
	mpmocks "github.com/imgoci/go/internal/multipart/mocks"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

const (
	testWorkers   = 2
	testEntryN    = 4
	settleWorkers = 50 * time.Millisecond
)

type fileFixture struct {
	role     string
	content  []byte
	stored   []byte
	manifest []byte
	entry    Entry
	layer    digest.Digest
}

func TestFetchFilesPreflightBeforeNetwork(t *testing.T) {
	t.Parallel()
	m := regmocks.NewMockManifests(t)
	b := regmocks.NewMockBlobs(t)
	dir := t.TempDir()
	shared := filepath.Join(dir, "out")

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     b,
		Entries: []Entry{
			{Role: "disk", MediaType: index.MediaTypeManifest, ArtifactType: index.ArtifactTypeFile},
			{Role: "kernel", MediaType: index.MediaTypeManifest, ArtifactType: index.ArtifactTypeFile},
		},
		ByRole: map[string]string{"disk": shared, "kernel": shared},
	})
	if err == nil {
		t.Fatal("expected preflight error")
	}
	m.AssertNotCalled(t, "Get", mock.Anything, mock.Anything, mock.Anything)
	b.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
}

func TestFetchFilesWorkerHighWater(t *testing.T) { //nolint:gocognit // concurrent high-water probe
	t.Parallel()
	dir := t.TempDir()
	fixtures := make([]fileFixture, testEntryN)
	byRole := make(map[string]string, testEntryN)
	for i := range fixtures {
		role := string(rune('a' + i))
		fixtures[i] = noneFixture(t, role, []byte("content-"+role))
		byRole[role] = filepath.Join(dir, role+".img")
	}

	var inFlight, highWater atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, testEntryN)

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string) ([]byte, string, error) {
			cur := inFlight.Add(1)
			for {
				old := highWater.Load()
				if cur <= old || highWater.CompareAndSwap(old, cur) {
					break
				}
			}
			started <- struct{}{}
			<-release
			inFlight.Add(-1)
			for _, fx := range fixtures {
				if fx.entry.Digest.String() == ref {
					return fx.manifest, index.MediaTypeManifest, nil
				}
			}
			return nil, "", ErrNotFound
		}).Times(testEntryN)

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest) (io.ReadCloser, error) {
			for _, fx := range fixtures {
				if fx.layer == dgst {
					return io.NopCloser(bytes.NewReader(fx.stored)), nil
				}
			}
			return nil, ErrNotFound
		}).Times(testEntryN)

	entries := make([]Entry, len(fixtures))
	for i, fx := range fixtures {
		entries[i] = fx.entry
	}

	done := make(chan error, 1)
	go func() {
		done <- FetchFiles(t.Context(), FetchFilesRequest{
			Manifests: m,
			Blobs:     blobs,
			Entries:   entries,
			ByRole:    byRole,
			Workers:   testWorkers,
		})
	}()

	for range testWorkers {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for workers")
		}
	}
	time.Sleep(settleWorkers)
	if got := highWater.Load(); got > testWorkers {
		t.Fatalf("worker high-water %d exceeds Workers=%d", got, testWorkers)
	}
	if got := inFlight.Load(); got != testWorkers {
		t.Fatalf("in-flight Get calls %d, want %d", got, testWorkers)
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFetchFilesLastRoleVerifyFailureZeroCommits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := bigociNoneFixture(t, "disk", []byte("disk-bytes"))
	ok := noneFixture(t, "initrd", []byte("initrd-bytes"))
	bad := noneFixture(t, "kernel", []byte("kernel-bytes"))
	diskPath := filepath.Join(dir, "disk.img")
	initrdPath := filepath.Join(dir, "initrd.img")
	kernelPath := filepath.Join(dir, "kernel.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, big.entry.Digest.String(), big.entry.MediaType).
		Return(big.manifest, index.MediaTypeManifest, nil).Maybe()
	m.EXPECT().Get(mock.Anything, ok.entry.Digest.String(), ok.entry.MediaType).
		Return(ok.manifest, index.MediaTypeManifest, nil).Maybe()
	m.EXPECT().Get(mock.Anything, bad.entry.Digest.String(), bad.entry.MediaType).
		Return([]byte("not-the-manifest"), index.MediaTypeManifest, nil).Once()

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, ok.layer).
		Return(io.NopCloser(bytes.NewReader(ok.stored)), nil).Maybe()
	mp := mpmocks.NewMockMultipart(t)
	mp.EXPECT().PullTo(mock.Anything, testRepo, big.entry.Digest, mock.Anything, mock.Anything).
		RunAndReturn(writePullTo(big.stored)).Maybe()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests:  m,
		Blobs:      blobs,
		Multipart:  mp,
		Repository: testRepo,
		Entries:    []Entry{big.entry, ok.entry, bad.entry},
		ByRole: map[string]string{
			"disk":   diskPath,
			"initrd": initrdPath,
			"kernel": kernelPath,
		},
		Workers: 1,
	})
	if err == nil {
		t.Fatal("expected verify error")
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	assertAbsent(t, diskPath)
	assertAbsent(t, initrdPath)
	assertAbsent(t, kernelPath)
}

func TestFetchFilesFirstErrorPrefersDigestMismatch(t *testing.T) {
	t.Parallel()
	const trials = 50
	for i := range trials {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			assertFirstErrorPrefersDigest(t)
		})
	}
}

func TestFetchFilesMismatchTables(t *testing.T) {
	t.Parallel()
	fx := noneFixture(t, "disk", []byte("payload"))
	tests := []struct {
		name    string
		mut     func(*Entry, *[]byte, *string)
		wantIs  error
		wantSub string
		pull    bool
	}{
		{
			name: "digest mismatch",
			mut: func(_ *Entry, raw *[]byte, _ *string) {
				*raw = []byte("other-bytes")
			},
			wantIs: ErrDigestMismatch,
		},
		{
			name: "size mismatch",
			mut: func(e *Entry, _ *[]byte, _ *string) {
				e.Size++
			},
			wantIs: ErrDigestMismatch,
		},
		{
			name: "content-type mismatch",
			mut: func(_ *Entry, _ *[]byte, ct *string) {
				*ct = "application/json"
			},
			wantIs:  ErrInvalidDocument,
			wantSub: "content type",
		},
		{
			name: "artifactType mismatch",
			mut: func(e *Entry, _ *[]byte, _ *string) {
				e.ArtifactType = index.ArtifactTypeBigOCI
			},
			wantIs:  ErrInvalidDocument,
			wantSub: "artifactType",
		},
		{
			name: "mediaType identity mismatch",
			mut: func(e *Entry, _ *[]byte, ct *string) {
				e.MediaType = "application/vnd.other"
				*ct = "application/vnd.other"
			},
			wantIs:  ErrInvalidDocument,
			wantSub: "mediaType",
		},
		{
			name: "validate standard",
			mut: func(e *Entry, raw *[]byte, _ *string) {
				*raw = []byte(`{"not":"a-manifest"}`)
				e.Digest = digest.FromBytes(*raw)
				e.Size = int64(len(*raw))
			},
			wantIs: ErrInvalidDocument,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := fx.entry
			raw := append([]byte(nil), fx.manifest...)
			ct := index.MediaTypeManifest
			tc.mut(&entry, &raw, &ct)

			m := regmocks.NewMockManifests(t)
			m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
				Return(raw, ct, nil).Once()
			blobs := regmocks.NewMockBlobs(t)
			if tc.pull {
				blobs.EXPECT().Pull(mock.Anything, fx.layer).
					Return(io.NopCloser(bytes.NewReader(fx.stored)), nil).Once()
			}

			dest := filepath.Join(t.TempDir(), "disk.img")
			err := FetchFiles(t.Context(), FetchFilesRequest{
				Manifests: m,
				Blobs:     blobs,
				Entries:   []Entry{entry},
				ByRole:    map[string]string{"disk": dest},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("error %v is not %v", err, tc.wantIs)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %v does not contain %q", err, tc.wantSub)
			}
			assertAbsent(t, dest)
			blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
		})
	}
}

func TestFetchFilesNonePrecheckMismatch(t *testing.T) {
	t.Parallel()
	fx := noneFixture(t, "disk", []byte("payload"))
	entry := fx.entry
	entry.ContentDigest = digest.FromBytes([]byte("nope"))

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, entry.Digest.String(), entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)

	dest := filepath.Join(t.TempDir(), "disk.img")
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{entry},
		ByRole:    map[string]string{"disk": dest},
	})
	if err == nil {
		t.Fatal("expected precheck error")
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
	assertAbsent(t, dest)
}

func TestFetchFilesHappyGzip(t *testing.T) {
	t.Parallel()
	content := []byte("hello imgoci gzip")
	fx := gzipFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, fx.layer).
		Return(io.NopCloser(bytes.NewReader(fx.stored)), nil).Once()

	var (
		mu    sync.Mutex
		snaps []Progress
	)
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{fx.entry},
		ByRole:    map[string]string{"disk": dest},
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
	if snaps[len(snaps)-1].WireBytes != int64(len(fx.stored)) {
		t.Fatalf("WireBytes %d, want %d", snaps[len(snaps)-1].WireBytes, len(fx.stored))
	}
}

// TestFetchFilesLayerSizeOverstatedRejected is the spec §8 layer-size
// reproduction: a well-formed gzip blob of N bytes whose layer digest is the
// digest of exactly those N bytes, inside a canonical standard manifest that
// declares layers[0].size = N+1. Content digest and content size are
// correct, so the digest checks all pass and only the raw stored-size
// equality can reject the entry.
func TestFetchFilesLayerSizeOverstatedRejected(t *testing.T) {
	t.Parallel()
	content := []byte("hello imgoci overstated layer size")
	fx := overstatedSizeFixture(t, "disk", content)
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, fx.layer).
		Return(io.NopCloser(bytes.NewReader(fx.stored)), nil).Once()

	var (
		mu    sync.Mutex
		snaps []Progress
	)
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{fx.entry},
		ByRole:    map[string]string{"disk": dest},
		Progress: func(p Progress) {
			mu.Lock()
			snaps = append(snaps, p)
			mu.Unlock()
		},
	})
	if err == nil {
		t.Fatal("declared layer size N+1 over an N-byte blob verified clean")
	}
	if !errors.Is(err, decomp.ErrSizeMismatch) {
		t.Fatalf("error %v is not decomp.ErrSizeMismatch", err)
	}
	if errors.Is(err, decomp.ErrDecode) {
		t.Fatalf("stored-size underrun was reclassified as a decode failure: %v", err)
	}
	assertAbsent(t, dest)
	assertNoSuccessTerminal(t, &mu, &snaps)
}

// TestFetchFilesStandardIntegrityBoundaries drives the spec §8 standard-path
// integrity checks one at a time. Every row keeps the retrieved manifest and
// the file entry mutually consistent except for the single value it moves, so
// exactly one check can reject it.
//
// The layer-blob rows serve a blob of the declared length whose bytes are not
// the bytes layers[0].digest names. The gzip row flips only the gzip header
// MTIME, so the blob still decodes to the declared content and the sole
// remaining check is layer-digest verification on the blob stream: the [Blobs]
// port owns it (the registry adapter's Pull wraps go-oci-blob's verified
// reader) and [decomp.BoundedReader] triggers it with the exact-limit EOF
// probe. The none row flips a stored byte under a plain reader to prove the
// transfer's own post-decode content comparison rejects wrong layer bytes.
func TestFetchFilesStandardIntegrityBoundaries(t *testing.T) {
	t.Parallel()
	content := []byte("hello imgoci standard integrity boundaries")
	tests := []standardIntegrityCase{
		{
			name:       "gzip header mtime flipped under declared layer digest",
			fixture:    gzipFixture,
			served:     flipGzipMTime,
			verifyPort: true,
			wantSub:    "layer blob",
			wantPull:   true,
		},
		{
			name:     "none stored byte flipped under declared layer digest",
			fixture:  noneFixture,
			served:   flipFirstStoredByte,
			wantSub:  "content",
			wantPull: true,
		},
		{
			name:     "compressed content digest mismatch after decode",
			fixture:  gzipFixture,
			mutate:   func(e *Entry) { e.ContentDigest = digest.FromBytes([]byte("not-the-decoded-bytes")) },
			wantSub:  "content",
			wantPull: true,
		},
		{
			name:     "compressed decoded size short of declared content size",
			fixture:  gzipFixture,
			mutate:   func(e *Entry) { e.ContentSize++ },
			wantSub:  "content",
			wantPull: true,
		},
		{
			name:    "none content size disagrees with layer size",
			fixture: noneFixture,
			mutate:  func(e *Entry) { e.ContentSize++ },
			wantSub: "none precheck",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fx := tc.fixtureFor(t, content)
			served := tc.servedBlob(t, fx.stored)

			m := regmocks.NewMockManifests(t)
			m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
				Return(fx.manifest, index.MediaTypeManifest, nil).Once()
			var mismatched atomic.Bool
			blobs := regmocks.NewMockBlobs(t)
			if tc.wantPull {
				blobs.EXPECT().Pull(mock.Anything, fx.layer).
					RunAndReturn(func(_ context.Context, dgst digest.Digest) (io.ReadCloser, error) {
						if tc.verifyPort {
							return newPortBlobReader(served, dgst, &mismatched), nil
						}
						return io.NopCloser(bytes.NewReader(served)), nil
					}).Once()
			}

			var (
				mu    sync.Mutex
				snaps []Progress
			)
			dest := filepath.Join(t.TempDir(), "disk.img")
			err := FetchFiles(t.Context(), FetchFilesRequest{
				Manifests: m,
				Blobs:     blobs,
				Entries:   []Entry{fx.entry},
				ByRole:    map[string]string{"disk": dest},
				Progress: func(p Progress) {
					mu.Lock()
					snaps = append(snaps, p)
					mu.Unlock()
				},
			})
			tc.assertRejected(t, standardIntegrityResult{
				err:        err,
				dest:       dest,
				mismatched: &mismatched,
				blobs:      blobs,
				mu:         &mu,
				snaps:      &snaps,
			})
		})
	}
}

// TestFetchFilesNoAlternativeAfterIntegrityFailure holds spec §8:773-777: an
// integrity failure fails the complete resolved result and must not make the
// consumer select another transport alternative. Both the selected entry and a
// second, honest alternative of the same role are servable, so a fallback would
// succeed and commit; the mock call log must show it was never asked for.
func TestFetchFilesNoAlternativeAfterIntegrityFailure(t *testing.T) {
	t.Parallel()
	selected := gzipFixture(t, "disk", []byte("hello imgoci selected alternative"))
	selected.entry.ContentDigest = digest.FromBytes([]byte("not-the-decoded-bytes"))
	alt := noneFixture(t, "disk", []byte("hello imgoci fallback alternative"))
	if alt.entry.Digest == selected.entry.Digest || alt.layer == selected.layer {
		t.Fatal("the alternative must be a distinct manifest and layer")
	}

	var (
		mu    sync.Mutex
		gets  = map[string]int{}
		pulls = map[digest.Digest]int{}
		snaps []Progress
	)
	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string) ([]byte, string, error) {
			mu.Lock()
			gets[ref]++
			mu.Unlock()
			switch ref {
			case selected.entry.Digest.String():
				return selected.manifest, index.MediaTypeManifest, nil
			case alt.entry.Digest.String():
				return alt.manifest, index.MediaTypeManifest, nil
			default:
				return nil, "", ErrNotFound
			}
		})
	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest) (io.ReadCloser, error) {
			mu.Lock()
			pulls[dgst]++
			mu.Unlock()
			switch dgst {
			case selected.layer:
				return io.NopCloser(bytes.NewReader(selected.stored)), nil
			case alt.layer:
				return io.NopCloser(bytes.NewReader(alt.stored)), nil
			default:
				return nil, ErrNotFound
			}
		})

	dest := filepath.Join(t.TempDir(), "disk.img")
	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{selected.entry},
		ByRole:    map[string]string{"disk": dest},
		Progress: func(p Progress) {
			mu.Lock()
			snaps = append(snaps, p)
			mu.Unlock()
		},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}

	assertAbsent(t, dest)
	assertNoSuccessTerminal(t, &mu, &snaps)

	mu.Lock()
	defer mu.Unlock()
	wantGets := map[string]int{selected.entry.Digest.String(): 1}
	wantPulls := map[digest.Digest]int{selected.layer: 1}
	if !maps.Equal(gets, wantGets) {
		t.Fatalf("manifest fetches %v, want %v: no alternative may be fetched", gets, wantGets)
	}
	if !maps.Equal(pulls, wantPulls) {
		t.Fatalf("layer pulls %v, want %v: no alternative may be fetched", pulls, wantPulls)
	}
}

func TestFetchFilesRegistrySentinels(t *testing.T) {
	t.Parallel()
	fx := noneFixture(t, "disk", []byte("payload"))
	tests := []struct {
		name   string
		getErr error
	}{
		{name: "not found", getErr: ErrNotFound},
		{name: "unauthorized", getErr: ErrUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := regmocks.NewMockManifests(t)
			m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
				Return(nil, "", tc.getErr).Once()
			blobs := regmocks.NewMockBlobs(t)
			dest := filepath.Join(t.TempDir(), "disk.img")
			err := FetchFiles(t.Context(), FetchFilesRequest{
				Manifests: m,
				Blobs:     blobs,
				Entries:   []Entry{fx.entry},
				ByRole:    map[string]string{"disk": dest},
			})
			if !errors.Is(err, tc.getErr) {
				t.Fatalf("error %v is not %v", err, tc.getErr)
			}
			blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
		})
	}
}

func TestFetchFilesCommitErrorUnwrapped(t *testing.T) {
	t.Parallel()
	fx := noneFixture(t, "disk", []byte("payload"))
	dest := filepath.Join(t.TempDir(), "disk.img")

	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, fx.entry.Digest.String(), fx.entry.MediaType).
		Return(fx.manifest, index.MediaTypeManifest, nil).Once()
	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Pull(mock.Anything, fx.layer).
		Return(io.NopCloser(bytes.NewReader(fx.stored)), nil).Once()

	err := FetchFiles(t.Context(), FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{fx.entry},
		ByRole:    map[string]string{"disk": dest},
		Progress: func(p Progress) {
			if p.CompletedFiles == p.TotalFiles && p.Phase == PhaseStaging {
				if mkdirErr := os.Mkdir(dest, 0o755); mkdirErr != nil {
					t.Errorf("mkdir dest: %v", mkdirErr)
				}
			}
		},
	})
	var ce *file.CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("error %T %v is not *file.CommitError", err, err)
	}
	if ce.Role != "disk" {
		t.Fatalf("CommitError.Role %q", ce.Role)
	}
}

func assertFirstErrorPrefersDigest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	a := noneFixture(t, "a", []byte("alpha-bytes"))
	b := noneFixture(t, "b", []byte("beta-bytes"))
	bBlocking := make(chan struct{})
	m := regmocks.NewMockManifests(t)
	m.EXPECT().Get(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, ref, _ string) ([]byte, string, error) {
			switch ref {
			case a.entry.Digest.String():
				select {
				case <-bBlocking:
				case <-ctx.Done():
					return nil, "", ctx.Err()
				}
				return []byte("not-the-manifest"), index.MediaTypeManifest, nil
			case b.entry.Digest.String():
				close(bBlocking)
				<-ctx.Done()
				return nil, "", ctx.Err()
			default:
				return nil, "", ErrNotFound
			}
		}).Times(2)
	blobs := regmocks.NewMockBlobs(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := FetchFiles(ctx, FetchFilesRequest{
		Manifests: m,
		Blobs:     blobs,
		Entries:   []Entry{a.entry, b.entry},
		ByRole: map[string]string{
			"a": filepath.Join(dir, "a.img"),
			"b": filepath.Join(dir, "b.img"),
		},
		Workers: 2,
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("error %v is not ErrDigestMismatch", err)
	}
	blobs.AssertNotCalled(t, "Pull", mock.Anything, mock.Anything)
}

func noneFixture(t *testing.T, role string, content []byte) fileFixture {
	t.Helper()
	return newFixture(t, role, content, content, compressionNone)
}

func gzipFixture(t *testing.T, role string, content []byte) fileFixture {
	t.Helper()
	return newFixture(t, role, content, gzipBytes(t, content), "gzip")
}

// overstatedSizeFixture builds a gzip fixture whose manifest declares
// layers[0].size one byte larger than the blob its layer digest names. Every
// other field, including the manifest digest, is consistent.
func overstatedSizeFixture(t *testing.T, role string, content []byte) fileFixture {
	t.Helper()
	fx := gzipFixture(t, role, content)
	fx.manifest = canonicalManifest(t, fx.layer, int64(len(fx.stored))+1)
	fx.entry.Digest = digest.FromBytes(fx.manifest)
	fx.entry.Size = int64(len(fx.manifest))
	return fx
}

func newFixture(t *testing.T, role string, content, stored []byte, compression string) fileFixture {
	t.Helper()
	layer := digest.FromBytes(stored)
	manifest := mustCanonicalManifest(t, stored)
	manDigest := digest.FromBytes(manifest)
	return fileFixture{
		role:     role,
		content:  content,
		stored:   stored,
		manifest: manifest,
		layer:    layer,
		entry: Entry{
			Role:          role,
			MediaType:     index.MediaTypeManifest,
			ArtifactType:  index.ArtifactTypeFile,
			Compression:   compression,
			Digest:        manDigest,
			Size:          int64(len(manifest)),
			ContentDigest: digest.FromBytes(content),
			ContentSize:   int64(len(content)),
			Filename:      role + ".img",
		},
	}
}

// mustCanonicalManifest encodes a canonical standard file manifest whose
// layer descriptor agrees with stored.
func mustCanonicalManifest(t *testing.T, stored []byte) []byte {
	t.Helper()
	return canonicalManifest(t, digest.FromBytes(stored), int64(len(stored)))
}

// canonicalManifest encodes a canonical standard file manifest declaring the
// given layer digest and size, which need not describe the same bytes.
func canonicalManifest(t *testing.T, layer digest.Digest, size int64) []byte {
	t.Helper()
	raw, err := jcs.Encode(map[string]any{
		"artifactType": index.ArtifactTypeFile,
		"config": map[string]any{
			"digest":    string(filemanifest.EmptyConfigDigest),
			"mediaType": filemanifest.MediaTypeEmpty,
			"size":      filemanifest.EmptyConfigSize,
		},
		"layers": []any{
			map[string]any{
				"digest":    layer.String(),
				"mediaType": filemanifest.MediaTypeLayer,
				"size":      size,
			},
		},
		"mediaType":     index.MediaTypeManifest,
		"schemaVersion": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gzipBytes(t *testing.T, p []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(p); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("path %s exists after failed fetch", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertProgressMonotoneTerminal(t *testing.T, snaps []Progress, bytes int64) {
	t.Helper()
	const files = 1
	if len(snaps) < 3 {
		t.Fatalf("got %d snapshots, want at least initial+verified+terminal", len(snaps))
	}

	if snaps[0].Phase != PhaseStaging || snaps[0].CompletedFiles != 0 || snaps[0].TotalFiles != files ||
		snaps[0].TotalBytes != bytes {
		t.Fatalf("initial %+v", snaps[0])
	}
	commitN := 0
	var prevFiles int
	var prevBytes int64
	var prevWire int64
	var prevRetries int
	for i, s := range snaps {
		if s.Direction != DirectionFetch {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.TotalFiles != files || s.TotalBytes != bytes {
			t.Fatalf("snap %d totals changed: %+v", i, s)
		}
		if s.CompletedFiles < prevFiles || s.CompletedBytes < prevBytes ||
			s.WireBytes < prevWire || s.Retries < prevRetries {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		prevFiles = s.CompletedFiles
		prevBytes = s.CompletedBytes
		prevWire = s.WireBytes
		prevRetries = s.Retries
		if s.Phase == PhaseCommit {
			commitN++
		}
	}
	last := snaps[len(snaps)-1]
	if last.Phase != PhaseCommit || last.CompletedFiles != files || last.CompletedBytes != bytes {
		t.Fatalf("terminal %+v", last)
	}
	if commitN != 1 {
		t.Fatalf("commit-phase snapshots %d, want 1", commitN)
	}
}
