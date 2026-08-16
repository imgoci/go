//go:build unix

package file

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// noFollow keeps create and reopen from traversing a symbolic link planted
// at the staged path, closing the race between the pre-open check and the
// open itself.
const noFollow = syscall.O_NOFOLLOW

// groupOtherBits is the permission mask that grants group or other any
// access. Staged files must not have these bits set.
const groupOtherBits os.FileMode = 0o077

// groupOtherWriteBits is the permission mask that grants group or other
// write access. A reused staging directory must not have these bits set.
const groupOtherWriteBits os.FileMode = 0o022

// validateAccess proves that info describes a file owned privately by the
// process's effective user. Group- or world-accessible files are refused.
func validateAccess(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect owner: unexpected file metadata %T", info.Sys())
	}

	want := os.Geteuid()
	if int64(stat.Uid) != int64(want) {
		return fmt.Errorf("owned by user %d, current effective user is %d", stat.Uid, want)
	}
	if permissions := info.Mode().Perm(); permissions&groupOtherBits != 0 {
		return fmt.Errorf("permissions %04o grant access to group or other users", permissions)
	}

	return nil
}

// validateStagingDir proves that info describes a directory owned privately
// by the process's effective user with no group or other write access.
func validateStagingDir(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect owner: unexpected file metadata %T", info.Sys())
	}

	want := os.Geteuid()
	if int64(stat.Uid) != int64(want) {
		return fmt.Errorf("owned by user %d, current effective user is %d", stat.Uid, want)
	}
	if permissions := info.Mode().Perm(); permissions&groupOtherWriteBits != 0 {
		return fmt.Errorf("permissions %04o grant write access to group or other users", permissions)
	}

	return nil
}

// isNoFollowErr reports an open that failed because the path was a symlink
// (or otherwise could not be opened without following).
func isNoFollowErr(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK)
}

// syncDir flushes the directory at path, making a just-renamed entry in it
// durable. [Plan.Commit] calls it after each rename, because a rename is
// metadata and metadata can reach disk after the data it names.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}

	if err := dir.Sync(); err != nil {
		_ = dir.Close()

		return err
	}

	return dir.Close()
}
