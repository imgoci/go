// Package auth authenticates registry HTTP requests.
//
// It is a clone of bigoci's auth stack — anonymous bearer token exchange,
// static username/password credentials, token caching, and off-origin
// credential stripping — reshaped for this repository. Duplication with
// bigoci is an accepted v1 decision (ARCHITECTURE.md §9.8): extraction waits
// for a third consumer. The Docker credential store is not in this package;
// it lands in slice 6 as WithDockerCredentials.
//
// The surface is a [Transport] RoundTripper decorator the registry adapter
// wraps around a base transport. A 401 with a WWW-Authenticate challenge
// becomes a token GET against the named realm (service and scope as query
// parameters), optionally with static Basic credentials, and the resulting
// bearer token is cached until expiry.
//
// # Realm traffic isolation
//
// Token-realm requests must never pass through the registry adapter's
// identity-encoding enforcement (ARCHITECTURE.md §6.6.1). That invariant is
// structural: [Transport.RealmClient] is an explicit injectable HTTP client
// used only for realm GETs, defaulting to a plain [http.Client] that is
// deliberately outside identity enforcement. [Transport.Base] is the
// registry-facing RoundTripper the adapter may wrap with identityTransport.
// Realm requests are issued with RealmClient.Do and never through
// Base.RoundTrip, so a compressing token realm keeps working while identity
// is enforced on manifest and blob GETs.
//
// # Off-origin stripping
//
// Credentials apply to [Transport.Host]. A request whose host differs —
// typically a blob redirect onto object storage — has Authorization stripped
// and receives no credential.
package auth
