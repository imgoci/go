package imgoci

// Source is a path-backed stored file used as Publish input.
//
// It is a concrete opaque struct built only by [FromFile]; there is no source
// interface.
//
// Source stability is a caller precondition: a Source must not change during
// [Client.Publish]. Defense-in-depth detects most violations but is not a
// guarantee under concurrent mutation. Pass 1 captures size and mtime and
// re-checks them before upload. On the standard path the upload reader
// cryptographically re-hashes the bytes actually streamed to the registry and
// fails the push at EOF on mismatch with pass 1, so wrong bytes cannot be
// committed under the declared digest even by a registry that skips commit
// checks.
type Source struct {
	// path is the filesystem path [FromFile] recorded.
	path string
}

// FromFile names a stored file at path. Compression is declared on the
// surrounding [FileSpec].Selector, not inferred from the path.
func FromFile(path string) Source {
	return Source{path: path}
}
