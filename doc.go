// Package imgoci is the canonical Go implementation of the imgoci release
// format. It validates, resolves, fetches, and publishes OS-image releases
// stored in OCI registries.
//
// The implemented specification is the imgoci release format v1, spec
// release v0.1.0 (imgoci/spec commit
// 8083159daebe15dc1d78da3e8a03b6b80526d427, the same pin recorded in
// testdata/conformance/SPEC_COMMIT).
//
// This library is under active development. The API is not yet stable
// (pre-v1).
package imgoci
