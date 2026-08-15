// Package imgoci is the canonical Go implementation of the imgoci release
// format. It validates, resolves, fetches, and publishes OS-image releases
// stored in OCI registries.
//
// The implemented specification is imgoci v1 draft, 2026-08-11
// (imgoci/spec commit da153d8d11fdf0eb3b4bd3c67393fec190397764).
//
// This library is under active development. The API is not yet stable
// (pre-v1).
package imgoci
