package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/opencontainers/go-digest"
)

// storedDirName is the content-addressed cache directory under the reserved
// staging entry.
const storedDirName = "stored"

// cacheKeyPrefix is the filename prefix for a stored entry. Combined with the
// untruncated 64-hex SHA-256 digest it is the identity of the cached bytes.
const cacheKeyPrefix = "sha256-"

// lockFileSuffix is appended to an entry path to name its sibling lock file.
const lockFileSuffix = ".lock"

// ErrCacheVerify reports that a fetched stored-cache entry failed full
// digest re-verification. Callers match with [errors.Is].
var ErrCacheVerify = errors.New("cache verify")

// StoredCache is the content-addressed cache of completed BigOCI stored
// files under `<parent>/.imgoci-stage/stored/`.
//
// Entries are named `sha256-<full 64-hex>` of `io.bigoci.file.digest`. The
// key is the identity of the bytes, untruncated, so distinct deliverables
// that share a stored file share the entry.
//
// [StoredCache.With] never trusts a cached file: a secure reopen and a full
// digest re-hash run before reuse. A pre-planted or poisoned entry only
// forces a re-pull; it cannot corrupt the bytes the use callback observes.
//
// Retention (ARCHITECTURE.md §9.6): a verified entry is retained when a
// later step fails so the next call can reuse it, and is removed on
// successful commit via [StoredCache.Remove]. Removal is best-effort:
// a failure keeps the entry as the documented fallback. A size-bounded
// cache and a Clean API are deferred until real usage shows retention
// patterns.
//
// Locking (ARCHITECTURE.md §9.7): each entry has a sibling `<entry>.lock`.
// The lock file is created with [os.O_CREATE]|[os.O_EXCL] when absent. On
// unix, waiters then take [syscall.Flock] LOCK_EX; waiting is
// context-cancellable by polling LOCK_NB with exponential backoff (chosen
// over a blocked flock goroutine so cancel cannot leave a waiter holding a
// lock it no longer wants). On other platforms flock is unavailable;
// exclusive create of the lock file is the lock, and waiters poll for the
// name to become creatable, bounded by ctx — a crashed holder leaves a
// stale lock until it is removed by hand or ctx is canceled. Windows
// behavior is best-effort. The split mirrors [createSecure]'s unix/other
// files.
//
// The zero value is not usable; [NewStoredCache] returns a ready one.
type StoredCache struct {
	// root is the absolute `<parent>/.imgoci-stage/stored` directory.
	root string
}

// NewStoredCache prepares the stored-cache directory beside parent.
//
// It creates parent if needed, then reuses [mkdirStaging] for both
// `.imgoci-stage` and `.imgoci-stage/stored` so the reserved directory's
// ownership and mode checks apply to the cache root as well.
func NewStoredCache(parent string) (*StoredCache, error) {
	if parent == "" {
		return nil, errors.New("file: stored cache parent is empty")
	}
	abs, err := filepath.Abs(parent)
	if err != nil {
		return nil, fmt.Errorf("file: stored cache parent: %w", err)
	}
	if err := os.MkdirAll(abs, destDirPerm); err != nil {
		return nil, fmt.Errorf("file: stored cache parent: %w", err)
	}

	stageRoot := filepath.Join(abs, stageEntryName)
	if err := mkdirStaging(stageRoot); err != nil {
		return nil, err
	}
	root := filepath.Join(stageRoot, storedDirName)
	if err := mkdirStaging(root); err != nil {
		return nil, err
	}

	return &StoredCache{root: root}, nil
}

// With serializes work on key under the per-entry exclusive lock.
//
// Under the lock it securely reopens the entry and re-verifies the full
// digest. On a match it calls use(path) and does not call fetch. Otherwise
// it treats the path as absent, calls fetch(dst) to populate it, re-verifies,
// and then calls use. Fetch-then-verify failure returns an error and removes
// the unusable entry. use is called only for a verified file; a use error
// retains that verified entry (ARCHITECTURE.md §9.6).
//
// The exclusive lock is held for the whole call, including use, so concurrent
// With on the same key share one fetch.
func (c *StoredCache) With(
	ctx context.Context,
	key digest.Digest,
	fetch func(dst string) error,
	use func(path string) error,
) error {
	if c == nil || c.root == "" {
		return errors.New("file: stored cache is not initialized")
	}
	if fetch == nil {
		return errors.New("file: stored cache fetch is nil")
	}
	if use == nil {
		return errors.New("file: stored cache use is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	path, lockPath, err := c.paths(key)
	if err != nil {
		return err
	}
	held, err := acquireKeyLock(ctx, lockPath)
	if err != nil {
		return fmt.Errorf("file: stored cache lock %s: %w", lockPath, err)
	}
	defer func() { _ = held.unlock() }()

	if err := populate(key, path, fetch); err != nil {
		return err
	}

	return use(path)
}

// Remove deletes a verified cache entry after a successful commit.
//
// It takes the per-key lock so a concurrent [StoredCache.With] cannot use
// the file while it is being removed. Waiting for the lock is bounded by
// ctx, matching [StoredCache.With]. A missing entry is not an error.
// The sibling lock file is removed while the flock is still held: waiters
// already in [acquireKeyLock] keep a valid flock on the old inode, and a
// later arrival exclusively creates a fresh lock file — the race
// [openLockFile] already tolerates.
func (c *StoredCache) Remove(ctx context.Context, key digest.Digest) error {
	if c == nil || c.root == "" {
		return errors.New("file: stored cache is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	path, lockPath, err := c.paths(key)
	if err != nil {
		return err
	}
	held, err := acquireKeyLock(ctx, lockPath)
	if err != nil {
		return fmt.Errorf("file: stored cache lock %s: %w", lockPath, err)
	}
	defer func() { _ = held.unlock() }()

	if err := removePath(path); err != nil {
		return err
	}
	return removePath(lockPath)
}

// populate ensures path is a verified stored file for key, fetching on miss
// or after a failed reopen/re-hash.
func populate(key digest.Digest, path string, fetch func(string) error) error {
	ok, err := entryUsable(key, path)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	err = removePath(path)
	if err != nil {
		return err
	}
	err = fetch(path)
	if err != nil {
		_ = removePath(path)

		return err
	}
	ok, err = entryUsable(key, path)
	if err != nil {
		_ = removePath(path)

		return err
	}
	if !ok {
		_ = removePath(path)

		return fmt.Errorf("file: stored cache fetch for %s failed digest re-verification: %w", key, ErrCacheVerify)
	}

	return nil
}

// paths returns the entry path and its sibling lock path for key.
func (c *StoredCache) paths(key digest.Digest) (string, string, error) {
	name, err := cacheEntryName(key)
	if err != nil {
		return "", "", err
	}
	entry := filepath.Join(c.root, name)

	return entry, entry + lockFileSuffix, nil
}

// cacheEntryName maps a SHA-256 digest to the untruncated entry filename
// `sha256-<64-hex>`. The colon in the digest form is not used in the name.
func cacheEntryName(key digest.Digest) (string, error) {
	if err := key.Validate(); err != nil {
		return "", fmt.Errorf("file: stored cache key: %w", err)
	}
	if alg := key.Algorithm(); alg != digest.SHA256 {
		return "", fmt.Errorf("file: stored cache key must be sha256, not %s", alg)
	}

	return cacheKeyPrefix + key.Encoded(), nil
}

// entryUsable reports whether path is a securely reopened regular file whose
// full SHA-256 matches key. A missing path, symlink, ownership/mode mismatch,
// or digest mismatch is treated as absent (false, nil) so the caller re-pulls
// rather than using untrusted bytes. Other I/O errors are returned.
func entryUsable(key digest.Digest, path string) (bool, error) {
	f, err := reopenSecure(path)
	if err != nil {
		if errors.Is(err, errAbsent) {
			return false, nil
		}

		return false, err
	}
	defer func() { _ = f.Close() }()

	got, err := hashOpened(f)
	if err != nil {
		return false, err
	}

	return got == key, nil
}

// hashOpened SHA-256s f from offset 0. Integrity always comes from this
// re-hash; the cache filename is not trusted on its own.
func hashOpened(f *os.File) (digest.Digest, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	return digest.SHA256.FromReader(f)
}

// removePath deletes path. A missing path is not an error.
func removePath(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}
