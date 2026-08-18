package file

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
)

// destKind selects how a [Destination] names output paths.
type destKind int

const (
	// destUnset is the zero [Destination], which is not a valid destination.
	destUnset destKind = iota
	// destDir names each output by joining a directory with the entry filename.
	destDir
	// destFiles names each output from an explicit per-role path map.
	destFiles
)

// ErrUnsetDestination reports a zero Destination.
var ErrUnsetDestination = errors.New("destination is unset")

// Destination is a path-backed fetch destination. It is a concrete opaque struct built
// only by [NewDir] and [NewFiles]; there is no destination interface.
//
// The zero value is invalid: [Destination.Map] reports [ErrUnsetDestination]
// for it, before any transfer is planned.
type Destination struct {
	// kind selects directory-join or explicit per-role paths.
	kind destKind
	// dir is the destination directory for [NewDir].
	dir string
	// files is the cloned per-role path map for [NewFiles].
	files map[string]string
}

// NewDir names each selected file by joining path with that entry's
// io.imgoci.filename. Filenames are already validated by the index rules;
// this constructor does not re-validate them.
func NewDir(path string) Destination {
	return Destination{kind: destDir, dir: path}
}

// NewFiles names each selected file from byRole, keyed by io.imgoci.role.
// The map is cloned at construction so later mutation cannot race preflight.
//
// Every selected role must be present and extra roles are rejected before any
// network I/O.
func NewFiles(byRole map[string]string) Destination {
	cloned := maps.Clone(byRole)
	if cloned == nil {
		cloned = map[string]string{}
	}

	return Destination{kind: destFiles, files: cloned}
}

// RoleFile is one selected role and the filename its entry declares.
type RoleFile struct {
	// Role is the selected entry's role.
	Role string
	// Filename is the filename the selected entry declares.
	Filename string
}

// Map returns the role-to-path map a fetch plan consumes.
//
// [NewDir] joins the directory with each entry's Filename. [NewFiles] requires
// every selected role to be present and no extras. The zero value is not a
// valid destination. A clone is returned so later mutation cannot race the
// transfer.
func (d Destination) Map(roles []RoleFile) (map[string]string, error) {
	switch d.kind {
	case destDir:
		out := make(map[string]string, len(roles))
		for _, entry := range roles {
			out[entry.Role] = filepath.Join(d.dir, entry.Filename)
		}

		return out, nil
	case destFiles:
		selected := make(map[string]struct{}, len(roles))
		for _, entry := range roles {
			role := entry.Role
			selected[role] = struct{}{}
			if _, ok := d.files[role]; !ok {
				return nil, fmt.Errorf("destination missing role %q", role)
			}
		}
		for role := range d.files {
			if _, ok := selected[role]; !ok {
				return nil, fmt.Errorf("destination extra role %q", role)
			}
		}

		return maps.Clone(d.files), nil
	case destUnset:
		return nil, ErrUnsetDestination
	default:
		return nil, ErrUnsetDestination
	}
}
