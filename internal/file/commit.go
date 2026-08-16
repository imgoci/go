package file

import (
	"fmt"
	"os"
	"slices"
)

// CommitError is the result of a commit-phase failure at one role.
//
// Commit is per-file atomic, not transactional across files. Committed is
// the prefix of [Plan.Commit]'s order whose rename already succeeded
// (roles 1..N−1 when role N failed). Role is the failing role. Files at
// Role and later remain in staging until [Plan.Cleanup]. A retry restages
// and recommits every selected role; it does not skip the prefix.
type CommitError struct {
	// Committed is the roles whose staged files were already renamed onto
	// their final paths, in the order they were committed.
	Committed []string
	// Role is the role whose fsync, close, rename, or parent-directory
	// fsync failed.
	Role string
	// Err is the underlying failure.
	Err error
}

// Error describes the failing role and the underlying error.
func (e *CommitError) Error() string {
	if e == nil {
		return "commit error"
	}

	return fmt.Sprintf("file: commit role %q: %v", e.Role, e.Err)
}

// Unwrap returns the underlying error so [errors.Is] and [errors.As] see
// through a [*CommitError].
func (e *CommitError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// newCommitError snapshots the committed prefix so later appends cannot
// alias the slice stored on the error.
func newCommitError(committed []string, role string, err error) *CommitError {
	return &CommitError{Committed: slices.Clone(committed), Role: role, Err: err}
}

// Commit publishes each staged role in order.
//
// For every role it reopens the staged path securely, fsyncs, closes the
// handle (close-before-rename, required on Windows-like platforms), renames
// staged onto final, and fsyncs the parent directory where that is durable.
// The first failure returns a [*CommitError]. Roles not listed in order are
// left staged.
//
// After the last successful rename Commit returns nil. Staging cleanup is
// the caller's job via [Plan.Cleanup] (transfer defers it best-effort) and
// is not folded into this result.
func (p *Plan) Commit(order []string) error {
	committed := make([]string, 0, len(order))
	for _, role := range order {
		if err := p.commitRole(role); err != nil {
			return newCommitError(committed, role, err)
		}
		committed = append(committed, role)
	}

	return nil
}

// commitRole publishes one staged role: secure reopen, fsync, close, rename,
// parent fsync. The handle is closed before rename on every platform.
func (p *Plan) commitRole(role string) error {
	p.mu.Lock()
	rs, ok := p.roles[role]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown role %q", role)
	}
	if rs == nil || rs.staged == "" {
		return fmt.Errorf("role %q was not staged", role)
	}

	f, err := reopenSecure(rs.staged)
	if err != nil {
		return err
	}
	if rs.info != nil {
		opened, statErr := f.Stat()
		if statErr != nil {
			_ = f.Close()

			return statErr
		}
		if !os.SameFile(rs.info, opened) {
			_ = f.Close()

			return errAbsent
		}
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()

		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if err := p.renameFile(rs.staged, rs.final); err != nil {
		return err
	}

	p.mu.Lock()
	rs.staged = ""
	rs.info = nil
	p.mu.Unlock()

	return syncDir(rs.parent)
}

// renameFile is [os.Rename] unless a test replaced Plan.rename.
func (p *Plan) renameFile(oldpath, newpath string) error {
	if p.rename != nil {
		return p.rename(oldpath, newpath)
	}

	return os.Rename(oldpath, newpath)
}
