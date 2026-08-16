package file

import (
	"context"
	"os"
	"time"
)

// lockPollMin is the first sleep between non-blocking lock attempts.
const lockPollMin = time.Millisecond

// lockPollMax caps the exponential backoff between lock attempts.
const lockPollMax = 50 * time.Millisecond

// lockPollGrow doubles the poll delay each wait until [lockPollMax].
const lockPollGrow = 2

// lockHandle is a held exclusive per-key lock. [lockHandle.unlock] releases
// it. On unix the open file carries a flock; on other platforms the file's
// exclusive create is the lock.
type lockHandle struct {
	// file is the open lock file kept for the hold's lifetime.
	file *os.File
}

// waitLockPoll sleeps *delay or until ctx is done, then grows *delay toward
// [lockPollMax]. Both unix flock polling and the lock-file-only fallback use
// this wait so cancellation is the same on every platform.
func waitLockPoll(ctx context.Context, delay *time.Duration) error {
	timer := time.NewTimer(*delay)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}

		return ctx.Err()
	case <-timer.C:
	}
	*delay = min(*delay*lockPollGrow, lockPollMax)

	return nil
}
