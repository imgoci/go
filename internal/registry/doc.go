// Package registry is the OCI Distribution adapter for one repository.
//
// It implements the transfer package's [github.com/imgoci/go/internal/transfer.Manifests]
// and [github.com/imgoci/go/internal/transfer.Blobs] ports. A [Client] is bound
// to one registry host and repository at construction. Manifest GET and PUT
// address a tag or "sha256:…" digest within that repository, not a
// registry/name string.
//
// # Identity-encoding invariant
//
// Spec §8 and ARCHITECTURE.md §6.6 require Accept-Encoding: identity and an
// identity-only Content-Encoding on every manifest and blob GET. Enforcement
// is an [http.RoundTripper] decorator whose scope follows provenance:
//
//   - The registry transport wraps only /v2/…/manifests/… and /v2/…/blobs/…
//     GET and HEAD. Token-realm traffic is never this transport:
//     [github.com/imgoci/go/internal/auth.Transport] issues realm GETs through
//     RealmClient, which [New] points at the unwrapped base, so a compressing
//     token issuer keeps working.
//   - The go-oci-blob storage transport is wrapped unconditionally. That
//     client carries only redirected blob traffic, so "external means blob"
//     is actually true there (ARCHITECTURE.md §6.6.2).
//
// # Retry
//
// internal/retry.Do with a zero Policy is the only retry domain in this
// package. go-oci-blob is constructed with RetryPolicy{} (one attempt) so
// the two loops never nest. Manifest PUT is a documented slice-3 stub;
// blob Push is wired and is not retried here, because its reader is consumed
// once.
//
// # Docker-Content-Digest
//
// The header is ignored (ARCHITECTURE.md §6.8). Identity of retrieved
// manifests is the returned bytes, hashed by the caller.
package registry
