// Package ociref parses a release reference into host, repository, optional
// tag, and optional digest.
//
// The grammar is github.com/distribution/reference. A digest must be sha256.
// [Parsed.ManifestRef] selects the digest over the tag when both are present.
// The package is pure and performs no I/O. Fetch and publish disagree about
// which reference forms they accept: [RequireTagOnly] states the publish rule,
// and fetch imposes no such requirement, so [Parse] itself accepts a
// name-only reference.
package ociref
