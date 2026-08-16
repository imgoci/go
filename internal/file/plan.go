package file

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// stageEntryName is the single reserved directory name in each destination
// parent. The reservation is not a prefix: only this exact entry is reserved.
const stageEntryName = ".imgoci-stage"

// destDirPerm is the mode used when creating missing destination parents.
const destDirPerm os.FileMode = 0o750

// ErrInvalidPlan reports that a destination plan failed preflight. Detail
// names the role, path, and check that failed. Callers match with
// [errors.Is].
var ErrInvalidPlan = errors.New("invalid plan")

// Plan is a preflighted mapping of roles to final paths, plus the per-call
// staging workspaces used to write them.
//
// The zero value is not usable; [NewPlan] returns a ready one. Methods are
// safe for concurrent Stage of distinct roles; Commit and Cleanup take the
// same lock.
type Plan struct {
	// mu guards roles' handles, workspaces, and the rename hook.
	mu sync.Mutex
	// roles is keyed by role name and populated by [NewPlan].
	roles map[string]*roleState
	// workspaces maps resolved final parent → [os.MkdirTemp] directory.
	workspaces map[string]string
	// rename replaces [os.Rename] in tests that observe commit order or
	// inject a rename failure. Nil means [os.Rename].
	rename func(oldpath, newpath string) error
}

// roleState is one role's resolved destination and, after [Plan.Stage], its
// staged file.
type roleState struct {
	// role is the map key that named this destination.
	role string
	// final is the resolved destination path.
	final string
	// parent is the resolved directory that holds final.
	parent string
	// staged is the path inside the per-call workspace, empty until Stage.
	staged string
	// info is the inode identity of the file Stage created, used by reopen.
	info os.FileInfo
}

// NewPlan preflights byRole before any caller I/O.
//
// Each path is made absolute and resolved by [filepath.EvalSymlinks] on the
// deepest existing parent so two lexical paths that meet through a symlinked
// parent are reported as the same file. The final file may not exist yet.
// Failures wrap [ErrInvalidPlan].
func NewPlan(byRole map[string]string) (*Plan, error) {
	roles := make([]string, 0, len(byRole))
	for role := range byRole {
		roles = append(roles, role)
	}
	slices.Sort(roles)

	plan := &Plan{
		roles:      make(map[string]*roleState, len(byRole)),
		workspaces: make(map[string]string),
	}
	seen := make(map[string]string, len(byRole))

	for _, role := range roles {
		if err := checkRoleName(role); err != nil {
			return nil, err
		}
		state, err := preflightRole(role, byRole[role], seen)
		if err != nil {
			return nil, err
		}
		seen[state.final] = role
		plan.roles[role] = state
	}

	return plan, nil
}

// Parent returns the resolved destination parent directory for role.
//
// That directory is the argument [NewStoredCache] needs for this role's
// BigOCI stored cache (`<parent>/.imgoci-stage/stored/`). Unknown roles
// fail. Parent is safe to call before [Plan.Stage].
func (p *Plan) Parent(role string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rs, ok := p.roles[role]
	if !ok {
		return "", fmt.Errorf("file: unknown role %q", role)
	}

	return rs.parent, nil
}

// checkRoleName rejects empty names and names that would escape a staging
// workspace as a path element.
func checkRoleName(role string) error {
	if role == "" || role == "." || role == ".." || role != filepath.Base(role) {
		return fmt.Errorf("file: invalid role name %q: %w", role, ErrInvalidPlan)
	}

	return nil
}

// preflightRole resolves path for role, rejects duplicates against seen,
// existing directories, and reserved staging-entry shadowing.
func preflightRole(role, path string, seen map[string]string) (*roleState, error) {
	if path == "" {
		return nil, fmt.Errorf("file: role %q has an empty path: %w", role, ErrInvalidPlan)
	}

	final, err := resolveFinal(path)
	if err != nil {
		return nil, fmt.Errorf("file: resolve role %q path %s: %w: %w", role, path, err, ErrInvalidPlan)
	}

	if other, ok := seen[final]; ok {
		return nil, fmt.Errorf(
			"file: roles %q and %q both resolve to %s: %w",
			other, role, final, ErrInvalidPlan,
		)
	}

	if err := rejectExistingDirectory(role, final); err != nil {
		return nil, err
	}

	if filepath.Base(final) == stageEntryName {
		return nil, fmt.Errorf(
			"file: destination %s for role %q shadows reserved %s entry: %w",
			final, role, stageEntryName, ErrInvalidPlan,
		)
	}

	return &roleState{
		role:   role,
		final:  final,
		parent: filepath.Dir(final),
	}, nil
}

// rejectExistingDirectory wraps [ErrInvalidPlan] when final exists as a
// directory, including a symlink whose target is a directory.
func rejectExistingDirectory(role, final string) error {
	info, err := os.Lstat(final)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("file: stat role %q path %s: %w: %w", role, final, err, ErrInvalidPlan)
	case info.IsDir():
		return fmt.Errorf("file: destination %s for role %q is a directory: %w", final, role, ErrInvalidPlan)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Stat(final)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("file: stat role %q path %s: %w: %w", role, final, err, ErrInvalidPlan)
	case target.IsDir():
		return fmt.Errorf("file: destination %s for role %q is a directory: %w", final, role, ErrInvalidPlan)
	}

	return nil
}

// resolveFinal returns the absolute destination, with [filepath.EvalSymlinks]
// applied to the deepest existing parent so the final file need not exist.
func resolveFinal(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	parent, err := evalDeepestExisting(filepath.Dir(abs))
	if err != nil {
		return "", err
	}

	return filepath.Join(parent, filepath.Base(abs)), nil
}

// evalDeepestExisting walks up from path until a component exists, then
// returns [filepath.EvalSymlinks] of that ancestor joined with the missing
// suffix.
func evalDeepestExisting(path string) (string, error) {
	cur := path
	var missing []string
	for {
		_, err := os.Lstat(cur)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(cur)
			if evalErr != nil {
				return "", evalErr
			}
			for _, name := range slices.Backward(missing) {
				resolved = filepath.Join(resolved, name)
			}

			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no existing parent for %s", path)
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}
