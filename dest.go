package imgoci

import (
	"fmt"

	"github.com/imgoci/go/internal/file"
)

// Dest is a path-backed fetch destination. It is a concrete opaque struct built
// only by [ToDir] and [ToFiles]; there is no destination interface.
//
// The zero value is invalid and [Client.FetchFiles] reports [ErrInvalidDest]
// before constructing a registry adapter.
type Dest struct {
	// dest is the resolved directory or explicit per-role path map.
	dest file.Destination
}

// ToDir names each selected file by joining path with that entry's
// io.imgoci.filename. Filenames are already validated by the index rules;
// this constructor does not re-validate them.
func ToDir(path string) Dest {
	return Dest{dest: file.NewDir(path)}
}

// ToFiles names each selected file from byRole, keyed by io.imgoci.role.
// The map is cloned at construction so later mutation cannot race preflight.
//
// [Client.FetchFiles] requires every selected role to be present and rejects
// extra roles, wrapping [ErrInvalidDest] before any network I/O.
func ToFiles(byRole map[string]string) Dest {
	return Dest{dest: file.NewFiles(byRole)}
}

// mapByRole builds the role-to-path map destination preflight consumes.
//
// [ToDir] joins the directory with each entry's Filename. [ToFiles] requires
// every selected role to be present and no extras. The zero value is
// [ErrInvalidDest].
func (d Dest) mapByRole(entries []FileEntry) (map[string]string, error) {
	roles := make([]file.RoleFile, len(entries))
	for i, entry := range entries {
		roles[i] = file.RoleFile{
			Role:     entry.Selector.Role,
			Filename: entry.Filename,
		}
	}
	out, err := d.dest.Map(roles)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, ErrInvalidDest)
	}

	return out, nil
}
