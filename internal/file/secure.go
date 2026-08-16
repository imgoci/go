package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// stagedPerm is owner read and write, nobody else. Staged bytes come off a
// network and are unverified until commit, so the conservative mode is the
// default; the process umask can only narrow it further.
const stagedPerm os.FileMode = 0o600

// errAbsent reports that a path is missing or failed the secure type,
// ownership, or mode checks. Callers treat it as "not there" and recreate
// rather than use the path.
var errAbsent = errors.New("absent")

// createSecure creates path exclusively with no-follow semantics and
// validates regular type, ownership, and mode on the opened handle.
func createSecure(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL|noFollow, stagedPerm)
	if err != nil {
		return nil, err
	}
	if err := validateOpened(f, path); err != nil {
		_ = f.Close()
		_ = os.Remove(path)

		return nil, err
	}

	return f, nil
}

// reopenSecure opens an existing staged path with no-follow semantics and
// the same type, ownership, and mode checks as [createSecure]. A missing
// path or a mismatch (symlink, non-regular, wrong owner, wrong mode, replaced
// inode) is [errAbsent] so the caller treats it as not there.
func reopenSecure(path string) (*os.File, error) {
	observed, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errAbsent
		}

		return nil, err
	}
	if !observed.Mode().IsRegular() {
		return nil, errAbsent
	}

	f, err := os.OpenFile(path, os.O_RDWR|noFollow, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || isNoFollowErr(err) {
			return nil, errAbsent
		}

		return nil, err
	}

	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(observed, opened) {
		_ = f.Close()

		return nil, errAbsent
	}
	if err := validateAccess(opened); err != nil {
		_ = f.Close()

		return nil, errAbsent
	}

	return f, nil
}

// validateOpened checks the file represented by f. Path is used only to name
// the refused file in errors; type, owner, and mode come from the handle.
func validateOpened(f *os.File, path string) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("file: %s is not a regular file", path)
	}
	if err := validateAccess(info); err != nil {
		return fmt.Errorf("file: %s is not safe to use: %w", path, err)
	}

	return nil
}
