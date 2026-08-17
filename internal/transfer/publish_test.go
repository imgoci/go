package transfer

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/mock"

	"github.com/imgoci/go/internal/decomp"
	"github.com/imgoci/go/internal/filemanifest"
	"github.com/imgoci/go/internal/index"
	regmocks "github.com/imgoci/go/internal/registry/mocks"
)

const publishTag = "v1"

type callLog struct {
	mu     sync.Mutex
	ops    []string
	puts   []string
	bodies map[string][]byte
}

func (c *callLog) add(op string) {
	c.mu.Lock()
	c.ops = append(c.ops, op)
	c.mu.Unlock()
}

func (c *callLog) addPut(ref string) {
	c.mu.Lock()
	c.ops = append(c.ops, "put:"+ref)
	c.puts = append(c.puts, ref)
	c.mu.Unlock()
}

func (c *callLog) recordBody(ref string, raw []byte) {
	c.mu.Lock()
	if c.bodies == nil {
		c.bodies = make(map[string][]byte)
	}
	c.bodies[ref] = bytes.Clone(raw)
	c.mu.Unlock()
}

func (c *callLog) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.ops))
	copy(out, c.ops)
	return out
}

func (c *callLog) putRefs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.puts))
	copy(out, c.puts)
	return out
}

func (c *callLog) body(ref string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bodies[ref]
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testSel(role, compression string) index.Selector {
	return index.Selector{
		Architecture:   "amd64",
		Target:         "x-test-target",
		Representation: "x-test-format",
		Role:           role,
		Compression:    compression,
	}
}

func publishEntry(path, role, compression, filename string) PublishEntry {
	return PublishEntry{
		SourcePath: path,
		Selector:   testSel(role, compression),
		Filename:   filename,
	}
}

func manifestRef(t *testing.T, stored digest.Digest, size int64) string {
	t.Helper()
	raw, err := filemanifest.BuildStandard(filemanifest.BuildInput{
		LayerDigest: stored,
		LayerSize:   size,
	})
	if err != nil {
		t.Fatal(err)
	}
	return digest.FromBytes(raw).String()
}

func recordingPorts(t *testing.T, log *callLog, exists map[digest.Digest]bool) Ports {
	t.Helper()
	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, raw []byte) error {
			log.addPut(ref)
			log.recordBody(ref, raw)
			return nil
		}).Maybe()

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Exists(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest) (bool, error) {
			log.add("exists:" + dgst.String())
			return exists[dgst], nil
		}).Maybe()
	blobs.EXPECT().Push(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest, _ int64, r io.Reader) error {
			log.add("push:" + dgst.String())
			_, _ = io.Copy(io.Discard, r)
			return nil
		}).Maybe()

	return Ports{Manifests: manifests, Blobs: blobs}
}

func TestPublishOrderManifestsAfterBlobsIndexLast(t *testing.T) {
	t.Parallel()
	alpha := []byte("alpha-bytes")
	bravo := []byte("bravo-bytes")
	a := writeTemp(t, "a.bin", alpha)
	b := writeTemp(t, "b.bin", bravo)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			publishEntry(a, "disk", compressionNone, "a"),
			publishEntry(b, "kernel", compressionNone, "b"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ops := log.snapshot()
	puts := log.putRefs()
	if len(puts) != 3 {
		t.Fatalf("puts %v, want 2 manifests + index", puts)
	}
	if puts[len(puts)-1] != publishTag {
		t.Fatalf("index PUT is not last: %v", puts)
	}
	assertBlobBeforeManifest(t, ops, digest.FromBytes(alpha), int64(len(alpha)))
	assertBlobBeforeManifest(t, ops, digest.FromBytes(bravo), int64(len(bravo)))
}

func assertBlobBeforeManifest(t *testing.T, ops []string, stored digest.Digest, size int64) {
	t.Helper()
	pushAt, existsAt, putAt := -1, -1, -1
	man := "put:" + manifestRef(t, stored, size)
	for i, op := range ops {
		switch op {
		case "exists:" + stored.String():
			existsAt = i
		case "push:" + stored.String():
			pushAt = i
		case man:
			putAt = i
		}
	}
	if existsAt < 0 || putAt < 0 {
		t.Fatalf("missing exists/put for %s in %v", stored, ops)
	}
	if existsAt > putAt {
		t.Fatalf("manifest PUT before exists for %s: %v", stored, ops)
	}
	if pushAt < 0 {
		t.Fatalf("missing push for %s in %v", stored, ops)
	}
	if pushAt > putAt {
		t.Fatalf("manifest PUT before blob push for %s: %v", stored, ops)
	}
}

func TestPublishDedupeSamePath(t *testing.T) {
	t.Parallel()
	data := []byte("shared-bytes")
	path := writeTemp(t, "shared.bin", data)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	stored := digest.FromBytes(data)
	man := "put:" + manifestRef(t, stored, int64(len(data)))

	var snaps []Progress
	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			publishEntry(path, "disk", compressionNone, "a"),
			publishEntry(path, "kernel", compressionNone, "b"),
		},
		Progress: func(p Progress) { snaps = append(snaps, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var pushes, exists, manPuts int
	for _, op := range log.snapshot() {
		if op == "push:"+stored.String() {
			pushes++
		}
		if op == "exists:"+stored.String() {
			exists++
		}
		if op == man {
			manPuts++
		}
	}
	if exists != 1 || pushes != 1 || manPuts != 1 {
		t.Fatalf("same path: exists=%d pushes=%d manifests=%d ops=%v", exists, pushes, manPuts, log.snapshot())
	}
	if snaps[len(snaps)-1].WireBytes != int64(len(data)) {
		t.Fatalf("WireBytes = %d, want one copy of the blob", snaps[len(snaps)-1].WireBytes)
	}
}

func TestPublishDedupeIdenticalBytesDifferentPaths(t *testing.T) {
	t.Parallel()
	data := []byte("twin-bytes")
	a := writeTemp(t, "a.bin", data)
	b := writeTemp(t, "b.bin", data)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	stored := digest.FromBytes(data)
	man := "put:" + manifestRef(t, stored, int64(len(data)))

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			publishEntry(a, "disk", compressionNone, "a"),
			publishEntry(b, "kernel", compressionNone, "b"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var pushes, manPuts int
	for _, op := range log.snapshot() {
		if op == "push:"+stored.String() {
			pushes++
		}
		if op == man {
			manPuts++
		}
	}
	if pushes != 1 || manPuts != 1 {
		t.Fatalf("identical bytes: pushes=%d manifests=%d ops=%v", pushes, manPuts, log.snapshot())
	}
}

func TestPublishExistsSkip(t *testing.T) {
	t.Parallel()
	data := []byte("cached-bytes")
	path := writeTemp(t, "cached.bin", data)
	stored := digest.FromBytes(data)
	log := &callLog{}
	ports := recordingPorts(t, log, map[digest.Digest]bool{stored: true})

	var snaps []Progress
	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:      publishTag,
		Name:     "example",
		Version:  "1",
		Entries:  []PublishEntry{publishEntry(path, "x-test-file", compressionNone, "a")},
		Progress: func(p Progress) { snaps = append(snaps, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range log.snapshot() {
		if op == "push:"+stored.String() {
			t.Fatalf("Exists-skip still pushed stored blob: %v", log.snapshot())
		}
	}
	if snaps[len(snaps)-1].WireBytes != 0 {
		t.Fatalf("WireBytes = %d, want 0 for Exists-skip", snaps[len(snaps)-1].WireBytes)
	}
	man := "put:" + manifestRef(t, stored, int64(len(data)))
	found := false
	for _, op := range log.snapshot() {
		if op == man {
			found = true
		}
	}
	if !found {
		t.Fatalf("Exists-skip still needs a manifest PUT: %v", log.snapshot())
	}
}

func TestPublishEmptyConfigPushedOnceBeforeManifests(t *testing.T) {
	t.Parallel()
	alpha := []byte("alpha-empty-cfg")
	bravo := []byte("bravo-empty-cfg")
	a := writeTemp(t, "a.bin", alpha)
	b := writeTemp(t, "b.bin", bravo)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			publishEntry(a, "disk", compressionNone, "a"),
			publishEntry(b, "kernel", compressionNone, "b"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEmptyConfigBeforePuts(t, log.snapshot(), true)
}

func TestPublishEmptyConfigExistsSkip(t *testing.T) {
	t.Parallel()
	data := []byte("empty-cfg-skip")
	path := writeTemp(t, "skip.bin", data)
	log := &callLog{}
	ports := recordingPorts(t, log, map[digest.Digest]bool{
		filemanifest.EmptyConfigDigest: true,
	})

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{publishEntry(path, "x-test-file", compressionNone, "a")},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEmptyConfigBeforePuts(t, log.snapshot(), false)
}

func assertEmptyConfigBeforePuts(t *testing.T, ops []string, wantPush bool) {
	t.Helper()
	emptyExists := "exists:" + filemanifest.EmptyConfigDigest.String()
	emptyPush := "push:" + filemanifest.EmptyConfigDigest.String()
	existsN := countOp(ops, emptyExists)
	pushN := countOp(ops, emptyPush)
	if existsN != 1 {
		t.Fatalf("empty-config exists=%d, want 1; ops=%v", existsN, ops)
	}
	wantPushN := 0
	if wantPush {
		wantPushN = 1
	}
	if pushN != wantPushN {
		t.Fatalf("empty-config push=%d, want %d; ops=%v", pushN, wantPushN, ops)
	}
	existsAt := indexOfOp(ops, emptyExists)
	pushAt := indexOfOp(ops, emptyPush)
	if wantPush && existsAt > pushAt {
		t.Fatalf("empty-config push before exists: %v", ops)
	}
	foundPut := false
	for i, op := range ops {
		if !strings.HasPrefix(op, "put:") {
			continue
		}
		foundPut = true
		if existsAt > i {
			t.Fatalf("empty-config exists after Manifests.Put: %v", ops)
		}
		if wantPush && pushAt > i {
			t.Fatalf("empty-config push after Manifests.Put: %v", ops)
		}
	}
	if !foundPut {
		t.Fatalf("no manifest PUTs in %v", ops)
	}
}

func countOp(ops []string, want string) int {
	n := 0
	for _, op := range ops {
		if op == want {
			n++
		}
	}
	return n
}

func indexOfOp(ops []string, want string) int {
	for i, op := range ops {
		if op == want {
			return i
		}
	}
	return -1
}

func TestPublishStatRecheckFailure(t *testing.T) {
	t.Parallel()
	path := writeTemp(t, "mut.bin", []byte("original-bytes"))
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{publishEntry(path, "x-test-file", compressionNone, "a")},
		Progress: func(p Progress) {
			if p.Phase == PhaseUpload && p.CompletedFiles == 0 {
				if werr := os.WriteFile(path, []byte("original-bytes-mutated"), 0o600); werr != nil {
					t.Error(werr)
				}
			}
		},
	})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if !strings.Contains(err.Error(), "mutated") {
		t.Fatalf("error should name mutation: %v", err)
	}
}

func TestPublishStrictGzipFailsBeforeUpload(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = gzip.NewWriter(&buf)
	if _, err := w.Write([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "two.gz", buf.Bytes())

	manifests := regmocks.NewMockManifests(t)
	blobs := regmocks.NewMockBlobs(t)
	_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs}, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{publishEntry(path, "x-test-file", "gzip", "a")},
	})
	if err == nil || !errors.Is(err, decomp.ErrDecode) {
		t.Fatalf("err = %v, want ErrDecode", err)
	}
	manifests.AssertNotCalled(t, "Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	blobs.AssertNotCalled(t, "Exists", mock.Anything, mock.Anything)
	blobs.AssertNotCalled(t, "Push", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestPublishWorkerBound(t *testing.T) {
	t.Parallel()
	const n = 4
	entries := make([]PublishEntry, n)
	for i := range n {
		name := string(rune('a' + i))
		// Distinct producer-defined roles keep every entry in its own
		// spec §6 rule 5 tuple without exhausting the five public roles.
		entries[i] = publishEntry(
			writeTemp(t, name+".bin", []byte("content-"+name)),
			"x-test-file-"+name,
			compressionNone,
			name,
		)
	}

	var inFlight, highWater atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, n)
	log := &callLog{}

	manifests := regmocks.NewMockManifests(t)
	manifests.EXPECT().Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, ref, _ string, _ []byte) error {
			log.addPut(ref)
			return nil
		}).Times(n + 1)

	blobs := regmocks.NewMockBlobs(t)
	blobs.EXPECT().Exists(mock.Anything, filemanifest.EmptyConfigDigest).
		Return(false, nil).Once()
	blobs.EXPECT().Push(mock.Anything, filemanifest.EmptyConfigDigest, filemanifest.EmptyConfigSize, mock.Anything).
		RunAndReturn(func(_ context.Context, _ digest.Digest, _ int64, r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			return nil
		}).Once()
	blobs.EXPECT().Exists(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest) (bool, error) {
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
			log.add("exists:" + dgst.String())
			return false, nil
		}).Times(n)
	blobs.EXPECT().Push(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, dgst digest.Digest, _ int64, r io.Reader) error {
			log.add("push:" + dgst.String())
			_, _ = io.Copy(io.Discard, r)
			return nil
		}).Times(n)

	done := make(chan error, 1)
	go func() {
		_, err := Publish(t.Context(), Ports{Manifests: manifests, Blobs: blobs}, PublishRequest{
			Tag:     publishTag,
			Name:    "example",
			Version: "1",
			Entries: entries,
			Workers: 2,
		})
		done <- err
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("third worker started while bound to 2")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if highWater.Load() > 2 {
		t.Fatalf("high water %d, want <= 2", highWater.Load())
	}
}

func TestPublishProgressMonotoneTerminalOnce(t *testing.T) {
	t.Parallel()
	data := []byte("progress-bytes")
	path := writeTemp(t, "p.bin", data)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	var snaps []Progress
	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:      publishTag,
		Name:     "example",
		Version:  "1",
		Entries:  []PublishEntry{publishEntry(path, "x-test-file", compressionNone, "a")},
		Progress: func(p Progress) { snaps = append(snaps, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) < 3 {
		t.Fatalf("got %d snapshots", len(snaps))
	}
	assertPublishProgress(t, snaps, int64(len(data)))
}

func assertPublishProgress(t *testing.T, snaps []Progress, wire int64) {
	t.Helper()
	if snaps[0].Direction != DirectionPublish || snaps[0].Phase != PhaseHashing {
		t.Fatalf("initial %+v", snaps[0])
	}
	phases, indexN := publishProgressPhases(t, snaps)
	last := snaps[len(snaps)-1]
	if last.Phase != PhaseIndex || last.CompletedFiles != 1 {
		t.Fatalf("terminal %+v", last)
	}
	if indexN != 1 {
		t.Fatalf("index-phase snapshots %d, want 1", indexN)
	}
	if last.WireBytes != wire {
		t.Fatalf("WireBytes = %d", last.WireBytes)
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

func publishProgressPhases(t *testing.T, snaps []Progress) ([]string, int) {
	t.Helper()
	var files int
	var completed int64
	var wire int64
	var retries int
	var fallbacks int
	var phases []string
	indexN := 0
	for i, s := range snaps {
		if s.Direction != DirectionPublish {
			t.Fatalf("snap %d direction %q", i, s.Direction)
		}
		if s.CompletedFiles < files || s.CompletedBytes < completed ||
			s.WireBytes < wire || s.Retries < retries || s.Fallbacks < fallbacks {
			t.Fatalf("snap %d not monotone: %+v", i, s)
		}
		files = s.CompletedFiles
		completed = s.CompletedBytes
		wire = s.WireBytes
		retries = s.Retries
		fallbacks = s.Fallbacks
		if len(phases) == 0 || phases[len(phases)-1] != s.Phase {
			phases = append(phases, s.Phase)
		}
		if s.Phase == PhaseIndex {
			indexN++
		}
	}
	return phases, indexN
}

func TestPublishSharedBlobDisagreement(t *testing.T) {
	t.Parallel()
	payload := gzipBytes(t, []byte("same-bytes"))
	a := writeTemp(t, "a.gz", payload)
	b := writeTemp(t, "b.gz", payload)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			{
				SourcePath: a,
				Selector: index.Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "disk",
					Compression:    "gzip",
				},
				Filename: "a",
			},
			{
				SourcePath: b,
				Selector: index.Selector{
					Architecture:   "arm64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "kernel",
					Compression:    compressionNone,
				},
				Filename: "b",
			},
		},
	})
	if !errors.Is(err, ErrSharedBlob) {
		t.Fatalf("err = %v, want ErrSharedBlob", err)
	}
	if ops := log.snapshot(); len(ops) != 0 {
		t.Fatalf("port calls = %v, want none before upload", ops)
	}
}

func TestPublishFileIdentityBeforeUpload(t *testing.T) {
	t.Parallel()
	plain := writeTemp(t, "a.bin", []byte("content-a"))
	gz := writeTemp(t, "b.gz", gzipBytes(t, []byte("content-b")))
	log := &callLog{}
	ports := recordingPorts(t, log, nil)

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			publishEntry(plain, "x-test-file", compressionNone, "same"),
			{
				SourcePath: gz,
				Selector: index.Selector{
					Architecture:   "amd64",
					Target:         "x-test-target",
					Representation: "x-test-format",
					Role:           "x-test-file",
					Compression:    "gzip",
				},
				Filename: "same",
			},
		},
	})
	if !errors.Is(err, index.ErrRule) {
		t.Fatalf("err = %v, want index.ErrRule", err)
	}
	if ops := log.snapshot(); len(ops) != 0 {
		t.Fatalf("port calls = %v, want none before upload", ops)
	}
}

func TestPublishFileIdentityDifferentUsage(t *testing.T) {
	t.Parallel()
	a := writeTemp(t, "a.bin", []byte("content-a"))
	b := writeTemp(t, "b.bin", []byte("content-b"))
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	empty := testSel("x-test-file", compressionNone)
	live := empty
	live.Usage = "live"

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			{SourcePath: a, Selector: empty, Filename: "a"},
			{SourcePath: b, Selector: live, Filename: "b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ops := log.snapshot(); len(ops) == 0 {
		t.Fatal("expected upload after distinct usage identities")
	}
}

func TestPublishSameSourceDifferentUsageBothReachIndex(t *testing.T) {
	t.Parallel()
	data := []byte("shared-bytes")
	path := writeTemp(t, "shared.bin", data)
	log := &callLog{}
	ports := recordingPorts(t, log, nil)
	empty := testSel("x-test-file", compressionNone)
	live := empty
	live.Usage = "live"
	stored := digest.FromBytes(data)

	_, err := Publish(t.Context(), ports, PublishRequest{
		Tag:     publishTag,
		Name:    "example",
		Version: "1",
		Entries: []PublishEntry{
			{SourcePath: path, Selector: empty, Filename: "shared.bin"},
			{SourcePath: path, Selector: live, Filename: "shared.bin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSharedBlobOnce(t, log, stored, int64(len(data)))
	assertBothUsageEntries(t, log.body(publishTag), stored)
}

func assertSharedBlobOnce(t *testing.T, log *callLog, stored digest.Digest, size int64) {
	t.Helper()
	man := "put:" + manifestRef(t, stored, size)
	var pushes, exists, manPuts int
	ops := log.snapshot()
	for _, op := range ops {
		switch op {
		case "push:" + stored.String():
			pushes++
		case "exists:" + stored.String():
			exists++
		case man:
			manPuts++
		}
	}
	if exists != 1 || pushes != 1 || manPuts != 1 {
		t.Fatalf("shared source: exists=%d pushes=%d manifests=%d ops=%v", exists, pushes, manPuts, ops)
	}
}

func assertBothUsageEntries(t *testing.T, raw []byte, stored digest.Digest) {
	t.Helper()
	value, err := index.Decode(raw)
	if err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(value.Manifests) != 2 {
		t.Fatalf("index manifests = %d, want both request entries", len(value.Manifests))
	}
	got := map[string]index.Descriptor{}
	for _, desc := range value.Manifests {
		got[desc.Selector().Usage] = desc
	}
	emptyDesc, okEmpty := got[""]
	liveDesc, okLive := got["live"]
	if !okEmpty || !okLive {
		t.Fatalf("index usage sets = %v, want empty and live", usageKeys(got))
	}
	if emptyDesc.Filename() != "shared.bin" || liveDesc.Filename() != "shared.bin" {
		t.Fatalf("filenames = %q, %q", emptyDesc.Filename(), liveDesc.Filename())
	}
	if emptyDesc.ContentDigest() != stored || liveDesc.ContentDigest() != stored {
		t.Fatalf("content digests = %s, %s; want %s", emptyDesc.ContentDigest(), liveDesc.ContentDigest(), stored)
	}
	if emptyDesc.Digest == "" || emptyDesc.Digest != liveDesc.Digest {
		t.Fatalf("manifest digests = %s, %s; want the same de-duplicated digest", emptyDesc.Digest, liveDesc.Digest)
	}
}

func usageKeys(got map[string]index.Descriptor) []string {
	out := make([]string, 0, len(got))
	for key := range got {
		out = append(out, key)
	}
	return out
}

func TestOracleIndexDoesNotWrapErrRule(t *testing.T) {
	t.Parallel()
	err := oracleIndex([]byte(`{"schemaVersion":2}`))
	if err == nil {
		t.Fatal("expected self-oracle failure")
	}
	if errors.Is(err, index.ErrRule) {
		t.Fatalf("self-oracle must not wrap ErrRule: %v", err)
	}
	if !strings.Contains(err.Error(), "index self-oracle") {
		t.Fatalf("err = %v, want self-oracle wording", err)
	}
}
