//go:build !unix

package file

import (
	"context"
	"errors"
	"io/fs"
	"os"
)

// acquireKeyLock treats exclusive create of path as the lock. There is no flock
// on this platform. Waiters poll [os.O_CREATE]|[os.O_EXCL] with the same
// context-cancellable backoff as unix. A crashed holder leaves a stale lock
// file until it is removed by hand; waiters return [context.Context.Err] when
// ctx is done.
func acquireKeyLock(ctx context.Context, path string) (*lockHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	delay := lockPollFloor
	for {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, stagedPerm)
		if err == nil {
			return &lockHandle{file: f}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if waitErr := waitLockPoll(ctx, &delay); waitErr != nil {
			return nil, waitErr
		}
	}
}

// unlock closes the lock file and removes its name so the next waiter can
// create it. Removal is the release; without it waiters spin forever.
func (h *lockHandle) unlock() error {
	if h == nil || h.file == nil {
		return nil
	}
	path := h.file.Name()
	closeErr := h.file.Close()
	h.file = nil
	removeErr := os.Remove(path)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
		return removeErr
	}

	return nil
}
