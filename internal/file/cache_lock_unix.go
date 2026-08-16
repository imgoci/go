//go:build unix

package file

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// acquireKeyLock opens or creates path with [os.O_CREATE]|[os.O_EXCL], then
// waits for an exclusive [syscall.Flock]. Waiting polls LOCK_EX|LOCK_NB with
// exponential backoff and returns [context.Context.Err] when ctx is done,
// instead of blocking in flock on a goroutine that would still acquire after
// cancel.
func acquireKeyLock(ctx context.Context, path string) (*lockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	delay := lockPollMin
	for {
		err = flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &lockHandle{file: f}, nil
		}
		if !isLockBusy(err) {
			_ = f.Close()

			return nil, err
		}
		if waitErr := waitLockPoll(ctx, &delay); waitErr != nil {
			_ = f.Close()

			return nil, waitErr
		}
	}
}

// unlock drops the flock and closes the lock file. The name is left in
// place unless [StoredCache.Remove] already unlinked it under the hold;
// waiters that opened the old inode keep a valid flock. A crashed holder
// is released by the kernel.
func (h *lockHandle) unlock() error {
	if h == nil || h.file == nil {
		return nil
	}
	err := flock(h.file, syscall.LOCK_UN)
	closeErr := h.file.Close()
	h.file = nil
	if err != nil {
		return err
	}

	return closeErr
}

// openLockFile creates path exclusively when absent, or opens the existing
// regular lock file. A planted symlink fails the no-follow open.
func openLockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|noFollow, stagedPerm)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	f, err = os.OpenFile(path, os.O_RDWR|noFollow, 0)
	if err != nil {
		if isNoFollowErr(err) {
			return nil, fmt.Errorf("file: lock file %s is not usable", path)
		}

		return nil, err
	}

	return f, nil
}

// isLockBusy reports a non-blocking flock that would have to wait.
func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

// flock calls [syscall.Flock] on f. The file descriptor is range-checked so
// the conversion to int cannot overflow.
func flock(f *os.File, how int) error {
	fd := f.Fd()
	if fd > uintptr(^uint(0)>>1) {
		return fmt.Errorf("file: lock file descriptor %d overflows int", fd)
	}

	return syscall.Flock(int(fd), how)
}
