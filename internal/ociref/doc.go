// Package ociref parses a release reference into host, repository, optional
// tag, and optional digest.
//
// The grammar is github.com/distribution/reference. A digest must be sha256.
// [Parsed.ManifestRef] selects the digest over the tag when both are present.
// The package is pure and performs no I/O. It does not decide whether a
// missing tag or digest is an error: that rule differs between fetch and
// publish.
package ociref
