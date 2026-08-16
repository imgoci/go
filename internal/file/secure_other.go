//go:build !unix

package file

import "os"

// noFollow is zero where the platform has no O_NOFOLLOW. [createSecure]'s
// exclusive create cannot follow an existing link, and [reopenSecure]'s
// pre-open regular-file check still refuses a statically planted symbolic
// link.
const noFollow = 0

// validateAccess adds no ownership rule on platforms without Unix UID
// metadata. [reopenSecure] still preserves static symlink refusal and checks
// [os.SameFile] where the platform can substantiate it, without claiming an
// ownership or race-free pathname boundary this adapter cannot establish.
func validateAccess(_ os.FileInfo) error {
	return nil
}

// validateStagingDir adds no ownership or mode rule on platforms without
// Unix UID metadata, matching [validateAccess].
func validateStagingDir(_ os.FileInfo) error {
	return nil
}

// isNoFollowErr is always false where the platform has no O_NOFOLLOW.
func isNoFollowErr(error) bool {
	return false
}

// syncDir is a no-op where directories cannot be opened and flushed the unix
// way; the rename in [Plan.Commit] is still atomic, its durability is just
// left to the filesystem.
func syncDir(string) error {
	return nil
}
