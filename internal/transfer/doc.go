// Package transfer orchestrates imgoci fetch and publish of release indexes
// and file manifests.
//
// It declares the [Manifests], [Blobs], and [Multipart] ports adapters
// implement, bundled as [Ports] for [Publish]. Multipart is the BigOCI surface:
// path-typed, tag-free, and wired by the root client. A nil Multipart is still
// valid when every entry takes the standard path (including a <2-part
// fallback). [Progress] unifies standard-path and BigOCI WireBytes and Retries
// into one serialized absolute stream.
//
// Destination planning is [github.com/imgoci/go/internal/file.NewPlan]:
// preflight of ByRole paths happens before any registry Get or Pull. A
// [*github.com/imgoci/go/internal/file.CommitError] from
// [github.com/imgoci/go/internal/file.Plan.Commit] is returned unwrapped so
// callers can read the committed prefix.
package transfer
