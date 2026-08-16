package file

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
)

// sha512HexPairs is half the hex length of a SHA-512 digest (128 hex chars).
const sha512HexPairs = 64

func TestNewStoredCacheCreatesRoot(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	c, err := NewStoredCache(parent)
	if err != nil {
		t.Fatal(err)
	}
	if c.root == "" {
		t.Fatal("empty root")
	}
	info, err := os.Lstat(c.root)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("root is not a directory: %v", info.Mode())
	}
	want := filepath.Join(parent, stageEntryName, storedDirName)
	if c.root != want {
		t.Fatalf("root %q, want %q", c.root, want)
	}
}

func TestNewStoredCacheEmptyParent(t *testing.T) {
	t.Parallel()

	_, err := NewStoredCache("")
	if err == nil {
		t.Fatal("expected empty parent error")
	}
}

func TestStoredCacheMissFetchesThenUse(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-payload-miss")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	var fetches, uses atomic.Int32
	got, err := withCount(c, key, countingWriteFetch(payload, &fetches), &uses, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertUseBytes(t, got, payload)
	assertCounts(t, &fetches, &uses, 1, 1)
}

func TestStoredCacheReuseSkipsFetch(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-payload-reuse")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	populateCache(t, c, key, payload)

	var fetches, uses atomic.Int32
	got, err := withCount(c, key, func(string) error {
		fetches.Add(1)
		return errors.New("fetch should not run")
	}, &uses, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertUseBytes(t, got, payload)
	assertCounts(t, &fetches, &uses, 0, 1)
}

func TestStoredCachePoisonedEntryRefetched(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-payload-poison")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	plantEntry(t, c, key, []byte("poisoned-bytes"))

	var fetches, uses atomic.Int32
	got, err := withCount(c, key, countingWriteFetch(payload, &fetches), &uses, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertUseBytes(t, got, payload)
	assertCounts(t, &fetches, &uses, 1, 1)
}

func TestStoredCacheFetchVerifyFailure(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-payload-bad-fetch")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	var fetches, uses atomic.Int32
	_, err := withCount(c, key, countingWriteFetch([]byte("wrong-bytes"), &fetches), &uses, nil)
	if err == nil {
		t.Fatal("expected verify error")
	}
	if !errors.Is(err, ErrCacheVerify) {
		t.Fatalf("error %v is not ErrCacheVerify", err)
	}
	assertCounts(t, &fetches, &uses, 1, 0)
	assertEntryAbsent(t, c, key)
}

func TestStoredCacheUseFailureRetains(t *testing.T) {
	t.Parallel()

	payload := []byte("stored-payload-use-fail")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	var fetches, uses atomic.Int32
	_, err := withCount(c, key, countingWriteFetch(payload, &fetches), &uses, errors.New("decode failed"))
	if err == nil {
		t.Fatal("expected use error")
	}
	assertCounts(t, &fetches, &uses, 1, 1)
	ok, usableErr := entryUsable(key, entryPath(t, c, key))
	if usableErr != nil {
		t.Fatal(usableErr)
	}
	if !ok {
		t.Fatal("verified entry was not retained after use failure")
	}
}

func TestStoredCacheConcurrentWithSameKey(t *testing.T) {
	t.Parallel()

	payload := []byte("shared-stored-bytes")
	key := digest.FromBytes(payload)
	c := newTestCache(t)

	var fetches, uses atomic.Int32
	fetch := func(dst string) error {
		fetches.Add(1)
		time.Sleep(lockPollMax)
		return os.WriteFile(dst, payload, stagedPerm)
	}

	const goroutines = 2
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			errCh <- c.With(context.Background(), key, fetch, func(path string) error {
				uses.Add(1)
				got, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if string(got) != string(payload) {
					return errors.New("use saw wrong bytes")
				}
				return nil
			})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches %d, want 1", fetches.Load())
	}
	if uses.Load() != goroutines {
		t.Fatalf("uses %d, want %d", uses.Load(), goroutines)
	}
}

func TestStoredCacheContextCancel(t *testing.T) {
	t.Parallel()

	t.Run("already canceled", func(t *testing.T) {
		t.Parallel()
		c := newTestCache(t)
		payload := []byte("cancel-already")
		key := digest.FromBytes(payload)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := c.With(ctx, key, func(string) error {
			t.Error("fetch called")
			return nil
		}, func(string) error {
			t.Error("use called")
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", err)
		}
	})

	t.Run("waiting on held lock", func(t *testing.T) {
		t.Parallel()
		c := newTestCache(t)
		payload := []byte("cancel-waiting")
		key := digest.FromBytes(payload)

		held := make(chan struct{})
		release := make(chan struct{})
		holderDone := make(chan error, 1)
		go func() {
			holderDone <- c.With(context.Background(), key, writeFetch(payload), func(string) error {
				close(held)
				<-release
				return nil
			})
		}()
		<-held

		ctx, cancel := context.WithCancel(context.Background())
		var waiterFetch, waiterUse atomic.Int32
		waiterDone := make(chan error, 1)
		go func() {
			waiterDone <- c.With(ctx, key, func(string) error {
				waiterFetch.Add(1)
				return errors.New("waiter fetch")
			}, func(string) error {
				waiterUse.Add(1)
				return errors.New("waiter use")
			})
		}()
		time.Sleep(lockPollMax)
		cancel()
		err := <-waiterDone
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter err %v, want context.Canceled", err)
		}
		if waiterFetch.Load() != 0 {
			t.Fatalf("waiter fetch called %d times", waiterFetch.Load())
		}
		if waiterUse.Load() != 0 {
			t.Fatalf("waiter use called %d times", waiterUse.Load())
		}
		close(release)
		if err := <-holderDone; err != nil {
			t.Fatal(err)
		}
	})
}

func TestStoredCacheRemove(t *testing.T) {
	t.Parallel()

	payload := []byte("remove-me")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	populateCache(t, c, key, payload)

	if err := c.Remove(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	assertEntryAbsent(t, c, key)
	assertStoredDirEmpty(t, c)

	if err := c.Remove(t.Context(), key); err != nil {
		t.Fatalf("idempotent Remove: %v", err)
	}

	var fetches atomic.Int32
	err := c.With(t.Context(), key, countingWriteFetch(payload, &fetches), func(path string) error {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if string(got) != string(payload) {
			t.Fatalf("use saw %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches %d after Remove, want 1", fetches.Load())
	}
}

func TestStoredCacheRemoveCanceled(t *testing.T) {
	t.Parallel()

	payload := []byte("remove-canceled")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	populateCache(t, c, key, payload)

	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- c.With(context.Background(), key, func(string) error {
			return errors.New("fetch should not run")
		}, func(string) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := c.Remove(ctx, key)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Remove err %v, want context.Canceled", err)
	}
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
}

func TestStoredCacheSymlinkTreatedAbsent(t *testing.T) {
	t.Parallel()

	payload := []byte("real-stored-bytes")
	key := digest.FromBytes(payload)
	c := newTestCache(t)
	path := entryPath(t, c, key)
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("not-the-payload"), stagedPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Skipf("symlink: %v", err)
	}

	var fetches atomic.Int32
	err := c.With(context.Background(), key, countingWriteFetch(payload, &fetches), func(p string) error {
		got, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if string(got) != string(payload) {
			t.Fatalf("use saw %q, want %q", got, payload)
		}
		info, statErr := os.Lstat(p)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			t.Fatal("use path is still a symlink")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches %d, want 1", fetches.Load())
	}
	if got := readFile(t, elsewhere); got != "not-the-payload" {
		t.Fatalf("symlink target mutated: %q", got)
	}
}

func TestStoredCacheRejectsUnusableStaging(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("staging owner/mode checks are unix-only")
	}

	parent := t.TempDir()
	stage := filepath.Join(parent, stageEntryName)
	if err := os.Mkdir(stage, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stage, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := NewStoredCache(parent)
	if err == nil {
		t.Fatal("expected unusable staging directory")
	}
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("error %v is not ErrInvalidPlan", err)
	}
}

func TestStoredCacheKeyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  digest.Digest
	}{
		{name: "empty", key: ""},
		{name: "truncated", key: digest.Digest("sha256:abcd")},
		{name: "not sha256", key: digest.Digest("sha512:" + strings.Repeat("ab", sha512HexPairs))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestCache(t)
			err := c.With(
				context.Background(),
				tc.key,
				func(string) error { return nil },
				func(string) error { return nil },
			)
			if err == nil {
				t.Fatal("expected key error")
			}
			if err := c.Remove(t.Context(), tc.key); err == nil {
				t.Fatal("expected Remove key error")
			}
		})
	}
}

func TestStoredCacheNilCallbacks(t *testing.T) {
	t.Parallel()

	c := newTestCache(t)
	key := digest.FromBytes([]byte("x"))
	if err := c.With(context.Background(), key, nil, func(string) error { return nil }); err == nil {
		t.Fatal("expected nil fetch error")
	}
	if err := c.With(context.Background(), key, func(string) error { return nil }, nil); err == nil {
		t.Fatal("expected nil use error")
	}
}

func newTestCache(t *testing.T) *StoredCache {
	t.Helper()
	c, err := NewStoredCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func withCount(
	c *StoredCache,
	key digest.Digest,
	fetch func(string) error,
	uses *atomic.Int32,
	useErr error,
) ([]byte, error) {
	var got []byte
	err := c.With(context.Background(), key, fetch, func(path string) error {
		uses.Add(1)
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		got = b

		return useErr
	})

	return got, err
}

func assertUseBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if string(got) != string(want) {
		t.Fatalf("use saw %q, want %q", got, want)
	}
}

func assertCounts(t *testing.T, fetches, uses *atomic.Int32, wantFetch, wantUse int32) {
	t.Helper()
	if fetches.Load() != wantFetch {
		t.Fatalf("fetches %d, want %d", fetches.Load(), wantFetch)
	}
	if uses.Load() != wantUse {
		t.Fatalf("uses %d, want %d", uses.Load(), wantUse)
	}
}

func writeFetch(payload []byte) func(string) error {
	return func(dst string) error {
		return os.WriteFile(dst, payload, stagedPerm)
	}
}

func countingWriteFetch(payload []byte, fetches *atomic.Int32) func(string) error {
	return func(dst string) error {
		fetches.Add(1)
		return os.WriteFile(dst, payload, stagedPerm)
	}
}

func populateCache(t *testing.T, c *StoredCache, key digest.Digest, payload []byte) {
	t.Helper()
	err := c.With(context.Background(), key, writeFetch(payload), func(string) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func plantEntry(t *testing.T, c *StoredCache, key digest.Digest, contents []byte) {
	t.Helper()
	if err := os.WriteFile(entryPath(t, c, key), contents, stagedPerm); err != nil {
		t.Fatal(err)
	}
}

func entryPath(t *testing.T, c *StoredCache, key digest.Digest) string {
	t.Helper()
	name, err := cacheEntryName(key)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(c.root, name)
}

func assertEntryAbsent(t *testing.T, c *StoredCache, key digest.Digest) {
	t.Helper()
	path := entryPath(t, c, key)
	ok, err := entryUsable(key, path)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("entry is still usable")
	}
}

func assertStoredDirEmpty(t *testing.T, c *StoredCache) {
	t.Helper()
	entries, err := os.ReadDir(c.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("stored dir not empty after Remove: %v", names)
	}
}
