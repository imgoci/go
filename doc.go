// Package imgoci is the canonical Go implementation of the imgoci release
// format. It validates, resolves, fetches, and publishes OS-image releases
// stored in OCI registries.
//
// The implemented specification is imgoci v1 draft, 2026-08-11
// (imgoci/spec commit 5b957102eeda16498fdcb80a738431b83abd4197, the same
// pin recorded in testdata/conformance/SPEC_COMMIT).
//
// This library is under active development. The API is not yet stable
// (pre-v1).
package imgoci
