package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// stageDirPerm is owner-only access on the reserved staging directory and
// each per-call workspace.
const stageDirPerm os.FileMode = 0o700

// workspacePrefix is the [os.MkdirTemp] pattern used under the reserved
// staging directory.
const workspacePrefix = "call-"

// StagedFile is the writable end of one role's staged output. Writes go to a
// private file in this call's workspace. [StagedFile.Close] flushes and
// closes the handle; [Plan.Commit] reopens the path securely before rename.
type StagedFile struct {
	// mu guards file.
	mu sync.Mutex
	// path is the staged file path inside the per-call workspace.
	path string
	// file is the write handle, nil after Close.
	file *os.File
}

// Stage opens a writer for role's staged file. The caller must Close it
// before [Plan.Commit]. A second Stage for the same role replaces the
// previous staged file so a retry can overwrite.
//
// The per-call workspace is created lazily on the first Stage for each
// distinct final parent via [os.MkdirTemp] under parent/.imgoci-stage
// (mode 0700). Workspaces are unique by construction; concurrent Plans in
// the same parent do not share one and need no locking.
func (p *Plan) Stage(role string) (*StagedFile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rs, ok := p.roles[role]
	if !ok {
		return nil, fmt.Errorf("file: unknown role %q", role)
	}
	if rs.staged != "" {
		if err := os.Remove(rs.staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("file: replace staged file for role %q: %w", role, err)
		}
		rs.staged = ""
		rs.info = nil
	}

	ws, err := p.workspaceFor(rs.parent)
	if err != nil {
		return nil, err
	}

	staged := filepath.Join(ws, rs.role)
	f, err := createSecure(staged)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		_ = os.Remove(staged)

		return nil, err
	}
	rs.staged = staged
	rs.info = info

	return &StagedFile{path: staged, file: f}, nil
}

// workspaceFor returns the per-call workspace for parent, creating the
// reserved staging directory and a unique [os.MkdirTemp] child on first use.
func (p *Plan) workspaceFor(parent string) (string, error) {
	if ws, ok := p.workspaces[parent]; ok {
		return ws, nil
	}
	if err := os.MkdirAll(parent, destDirPerm); err != nil {
		return "", err
	}

	stageRoot := filepath.Join(parent, stageEntryName)
	if err := mkdirStaging(stageRoot); err != nil {
		return "", err
	}

	ws, err := os.MkdirTemp(stageRoot, workspacePrefix)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(ws, stageDirPerm); err != nil {
		_ = os.RemoveAll(ws)

		return "", err
	}
	p.workspaces[parent] = ws

	return ws, nil
}

// mkdirStaging creates the reserved staging directory at path with
// [stageDirPerm], or reuses an existing real directory. A non-directory
// (including a symlink) is refused. An existing directory whose owner is
// not the effective user, or that grants group/other write access, is
// unusable and wraps [ErrInvalidPlan] naming the reserved entry.
func mkdirStaging(path string) error {
	err := os.Mkdir(path, stageDirPerm)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("file: reserved staging path %s exists and is not a directory", path)
	}
	if err := validateStagingDir(info); err != nil {
		return fmt.Errorf(
			"file: reserved staging entry %s is not usable: %w: %w",
			path, err, ErrInvalidPlan,
		)
	}

	return nil
}

// Write appends p to the staged file. It fails with [os.ErrClosed] after
// [StagedFile.Close].
func (sf *StagedFile) Write(p []byte) (int, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if sf.file == nil {
		return 0, os.ErrClosed
	}

	return sf.file.Write(p)
}

// Close fsyncs and closes the write handle. It is idempotent. The staged
// bytes remain on disk for [Plan.Commit] to reopen.
func (sf *StagedFile) Close() error {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if sf.file == nil {
		return nil
	}
	err := sf.file.Sync()
	closeErr := sf.file.Close()
	sf.file = nil
	if err != nil {
		return err
	}

	return closeErr
}

// Cleanup removes every per-call workspace created by [Plan.Stage]. It is
// idempotent: a second call is a no-op that returns nil. A nil receiver is
// a no-op. The reserved .imgoci-stage directory is removed only when empty,
// so a concurrent Plan in the same parent is left alone.
func (p *Plan) Cleanup() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var first error
	for _, rs := range p.roles {
		if rs.staged == "" {
			continue
		}
		if err := os.Remove(rs.staged); err != nil && !errors.Is(err, fs.ErrNotExist) && first == nil {
			first = err
		}
		rs.staged = ""
		rs.info = nil
	}
	for parent, ws := range p.workspaces {
		if err := os.RemoveAll(ws); err != nil && first == nil {
			first = err
		}
		_ = os.Remove(filepath.Join(parent, stageEntryName))
		delete(p.workspaces, parent)
	}

	return first
}
