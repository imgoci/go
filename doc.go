// Package imgoci is the canonical Go implementation of the imgoci release
// format. It validates, resolves, fetches, and publishes OS-image releases
// stored in OCI registries.
//
// The implemented specification is imgoci v1 draft, 2026-08-16
// (imgoci/spec commit 46d18b74cc407ac7d61ded7692fc42b644f4d1e2, the same
// pin recorded in testdata/conformance/SPEC_COMMIT).
//
// This library is under active development. The API is not yet stable
// (pre-v1).
package imgoci
