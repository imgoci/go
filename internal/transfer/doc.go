// Package transfer orchestrates imgoci fetch of release indexes and standard
// file manifests.
//
// It declares the [Manifests] and [Blobs] ports the registry adapter
// implements. PLAN 2.5(b) defers the Multipart port to slice 5 so this
// package compiles without opencontainers/image-spec; BigOCI pull/push lands
// with that port.
//
// Destination planning is [github.com/imgoci/go/internal/file.NewPlan]:
// preflight of ByRole paths happens before any registry Get or Pull. A Plan
// offers Stage(role) (*file.StagedFile, error), Commit(order []string) error,
// and Cleanup() error. A [*github.com/imgoci/go/internal/file.CommitError]
// from Commit is returned unwrapped so callers can read the committed prefix.
package transfer
