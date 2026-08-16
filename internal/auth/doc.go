// Package auth authenticates registry HTTP requests.
//
// It is a clone of bigoci's auth stack — anonymous bearer token exchange,
// static username/password credentials, an opt-in Docker configuration store,
// token caching, and off-origin credential stripping — reshaped for this
// repository. Duplication with bigoci is an accepted v1 decision
// (ARCHITECTURE.md §9.8): extraction waits for a third consumer.
//
// This is the only place oras-go enters the import graph, and it borrows
// exactly one thing from it: the credential store. The bearer exchange, the
// token cache, and the decision of what a refusal is worth all live in this
// package, because they are transfer behaviour rather than credential
// storage.
//
// Reading a credential can run a program on the machine. A Docker
// configuration file may name a credential helper, in its credsStore or
// credHelpers field, and a [Store] lookup then executes the program called
// docker-credential-<name> from the process PATH and reads the credential off
// what it prints. Which program that is, and where it comes from, is the
// user's configuration and not this package's choice. It is what a tool that
// honours `docker login` has to do, and it is why the public option that
// builds a Store is opt-in: a caller that never asks for it reads no file and
// runs no program. A lookup is given [credLookupTimeout] to finish, so a
// helper that never answers fails a transfer instead of hanging it.
//
// Nothing here can write to a user's configuration. The port has one read
// method, so the store's Put and Delete are unreachable through it and a
// credential-writing bug is unrepresentable rather than merely absent.
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
//
// # Docker credentials
//
// [Store] reads the Docker configuration file that `docker login` writes and
// answers with what it finds under the host a transfer dialed. [Static]
// answers every registry with one credential the caller already holds.
package auth
