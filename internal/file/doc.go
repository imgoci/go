// Package file plans destination paths, stages verified bytes, and commits them
// with per-file atomic rename. It also provides a content-addressed stored
// cache used by BigOCI fetch.
//
// # Preflight
//
// [NewPlan] is the I/O that callers must run before any network transfer. It
// resolves each role's final path by calling [filepath.EvalSymlinks] on the
// deepest existing parent (the final file itself may be missing) and rejects
// the plan wrapping [ErrInvalidPlan] when two roles collapse onto one file,
// when a final path is an existing directory, or when a final path is the
// reserved staging entry `.imgoci-stage` in its parent. Only that exact
// entry name is reserved: a caller tree such as `/srv/.imgoci-cache/output`
// is legal. Cross-filesystem role maps are allowed; each role stages beside
// its own final parent so every rename stays on one filesystem.
//
// # Staging
//
// [Plan.Stage] returns a [*StagedFile] writer into a per-call workspace
// created with [os.MkdirTemp] under `<parent>/.imgoci-stage/` at mode 0700.
// Workspaces are unique by construction, so concurrent plans in one parent
// never share staging and need no locking. One workspace is created per
// distinct final parent, lazily on the first Stage into it. The caller must
// [StagedFile.Close] the writer before [Plan.Commit].
//
// # Secure open
//
// Staging files are created and reopened with no-follow semantics
// ([syscall.O_NOFOLLOW] on unix) and checked for regular type, ownership, and
// mode. A mismatch is treated as absent so a planted symlink is never written
// or published. Other platforms use conservative fallbacks so GOOS=windows
// compiles. Windows is compile-checked only, via the moon `build-windows` task.
//
// # Stored cache
//
// [NewStoredCache] prepares `<parent>/.imgoci-stage/stored/` using the same
// reserved-directory ownership and mode checks as staging. Entries are
// `sha256-<full 64-hex>` of the stored digest; the key is the identity of the
// bytes. [StoredCache.With] takes a per-key lock, reopens securely, re-verifies
// the full digest, and only then calls use — a miss or a poisoned entry calls
// fetch and re-verifies before use. A failed re-verification wraps
// [ErrCacheVerify]. [StoredCache.Remove] deletes an entry and its sibling lock
// file after a successful commit; waiting for the lock is bounded by the
// supplied context. Verified entries are retained on failure or when removal
// cannot complete. Lock files are `<entry>.lock`; unix uses flock, other
// platforms use exclusive create bounded by context. Windows is compile-checked
// only.
//
// # Commit
//
// [Plan.Commit] publishes staged files in the caller-supplied role order:
// fsync the file, close the handle (required before rename on Windows-like
// platforms), rename staged onto final, then fsync the parent directory where
// that is durable. Each rename is atomic; the set of renames is not a
// transaction. A failure at role N returns a [*CommitError] whose Committed
// field is the prefix of roles whose rename already succeeded (1..N−1) and
// whose Role is the failing role. Retry overwrites every selected role; it
// does not skip the prefix. After the last successful rename Commit returns
// nil; leftover staging is the caller's job via [Plan.Cleanup].
//
// # Cleanup
//
// [Plan.Cleanup] removes every per-call workspace. It is idempotent and is
// safe to defer whether Commit succeeded, failed after a prefix, or never
// ran. A cleanup error is not a commit-phase failure.
package file
