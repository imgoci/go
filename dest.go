package imgoci

import (
	"fmt"
	"maps"
	"path/filepath"
)

// destKind selects how a [Dest] names output paths.
type destKind int

const (
	// destUnset is the zero [Dest], which is not a valid destination.
	destUnset destKind = iota
	// destDir names each output by joining a directory with the entry filename.
	destDir
	// destFiles names each output from an explicit per-role path map.
	destFiles
)

// Dest is a path-backed fetch destination. It is a concrete opaque struct
// built only by [ToDir] and [ToFiles]: there is no destination interface and
// no substitution point v1 does not offer.
//
// The zero value is invalid and [Client.FetchFiles] reports [ErrInvalidDest]
// before constructing a registry adapter.
type Dest struct {
	// kind selects directory-join or explicit per-role paths.
	kind destKind
	// dir is the destination directory for [ToDir].
	dir string
	// files is the cloned per-role path map for [ToFiles].
	files map[string]string
}

// ToDir names each selected file by joining path with that entry's
// io.imgoci.filename. Filenames are already validated by the index rules;
// this constructor does not re-validate them.
func ToDir(path string) Dest {
	return Dest{kind: destDir, dir: path}
}

// ToFiles names each selected file from byRole, keyed by io.imgoci.role.
// The map is cloned at construction so later mutation cannot race preflight.
//
// [Client.FetchFiles] requires every selected role to be present and rejects
// extra roles, wrapping [ErrInvalidDest] before any network I/O.
func ToFiles(byRole map[string]string) Dest {
	cloned := maps.Clone(byRole)
	if cloned == nil {
		cloned = map[string]string{}
	}

	return Dest{kind: destFiles, files: cloned}
}

// mapByRole builds the role-to-path map destination preflight consumes.
//
// [ToDir] joins the directory with each entry's Filename. [ToFiles] requires
// every selected role to be present and no extras. The zero value is
// [ErrInvalidDest].
func (d Dest) mapByRole(entries []FileEntry) (map[string]string, error) {
	switch d.kind {
	case destDir:
		out := make(map[string]string, len(entries))
		for _, entry := range entries {
			out[entry.Selector.Role] = filepath.Join(d.dir, entry.Filename)
		}

		return out, nil
	case destFiles:
		return mapToFiles(d.files, entries)
	case destUnset:
		return nil, fmt.Errorf("destination is unset: %w", ErrInvalidDest)
	default:
		return nil, fmt.Errorf("destination is unset: %w", ErrInvalidDest)
	}
}

// mapToFiles checks that files names exactly the selected roles and returns a
// clone so later mutation of Dest cannot race the transfer.
func mapToFiles(files map[string]string, entries []FileEntry) (map[string]string, error) {
	selected := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		role := entry.Selector.Role
		selected[role] = struct{}{}
		if _, ok := files[role]; !ok {
			return nil, fmt.Errorf("destination missing role %q: %w", role, ErrInvalidDest)
		}
	}
	for role := range files {
		if _, ok := selected[role]; !ok {
			return nil, fmt.Errorf("destination extra role %q: %w", role, ErrInvalidDest)
		}
	}

	return maps.Clone(files), nil
}
