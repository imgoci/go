// Package adapters is the per-repository adapter cache and the construction
// of the registry and multipart adapters behind the
// [github.com/imgoci/go/internal/transfer] ports.
//
// A [Pool] holds one mutex across construction so a repository is opened
// once. Failures are not cached. Error classification into public sentinels
// is deliberately the caller's job.
package adapters
