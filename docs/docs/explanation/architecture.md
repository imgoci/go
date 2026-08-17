---
title: About the architecture
description: Why imgoci/go is shaped the way it is - identity, binding, retries, and boundaries.
---

# About the architecture

imgoci/go implements both sides of the imgoci release format: a producer that
publishes releases and a consumer that fetches, validates, selects, and
verifies them. The implemented spec revision is imgoci v1, spec release
v0.1.0 (`imgoci/spec` commit `8083159daebe15dc1d78da3e8a03b6b80526d427`). This page
explains the design; for exact signatures and contracts, see the
[API reference](../reference/api.md).

## One public package, hexagonal internals

Everything public is `package imgoci` at the module root. Internally the
library follows hexagonal architecture: `internal/transfer` is the
orchestration core, and it declares the ports — `Manifests`, `Blobs`,
`Multipart` — that adapters satisfy. Pure packages (`internal/index`,
`internal/jcs`, `internal/filemanifest`, `internal/decomp`) import nothing but
the standard library and codec dependencies, so all validation and selection
logic runs and tests without a network. Adapters (`internal/registry`,
`internal/auth`, `internal/multipart`, `internal/file`) implement the ports;
the root package wires everything.

The boundary also decides what is delegated. Blob transfer goes through
`go-oci-blob`, a proven wire kernel with streaming digest verification.
Multipart (BigOCI) transfer goes through `bigoci`, whose reason to exist this
is. Everything the spec layers on top — manifest endpoints, bounded stored
reads, strict decoding, content verification, index canonicalization — lives
here, because no delegate provides it. The tradeoff is two external transports
with their own behavior; the sections on retries and content coding below are
consequences of that choice.

## Canonical bytes are identity

An imgoci release index is identified by the SHA-256 of its canonical (RFC
8785) bytes. The library takes this literally: `ParseIndex` validates that the
input bytes *are* canonical — decode with duplicate-key rejection first, then
the structural rules, then a full canonical transform byte-compared against
the input — and records the digest of those original bytes. It never
re-encodes to compute identity, because a re-encode could silently "fix" a
non-canonical document and produce a digest of bytes that were never
published.

The producer uses the same canonicalization path to encode, so producer bytes
and consumer verification cannot disagree. The digest is exposed for external
signers; signatures and trust are deliberately outside the library.

## Binding selection to retrieval

The consumer path is three steps — fetch, resolve, retrieve — and the digest
chains them together. `Fetch` validates the index and pins its digest.
`Resolved` carries the digest of the index it was selected from. `FetchFiles`
refuses to run unless that digest equals the fetched release's digest
(`ErrSelectionMismatch`), and then retrieves file manifests by digest, not by
tag.

Binding by digest rather than pointer identity means independently parsed
copies of the same canonical index interoperate and the binding survives
serialization. It also closes a race: a tag mutated between `Fetch` and
`FetchFiles` cannot redirect retrieval, because the tag is only consulted
once.

## Two retry domains, never nested

Exactly two retry domains exist, and they never nest.
`internal/retry.Policy{}` is the four-attempt outer domain for manifest
GET/PUT and blob Exists/Pull. go-oci-blob gets `blob.RetryPolicy{}` for one
delegate attempt. Standard blob Push is not adapter-retried. BigOCI owns a
separate four-attempt domain and is never wrapped.

The unified `Progress.Retries` field merges both domains without nesting:
standard-path attempts after the first that actually begin are counted
directly — cancellation during backoff does not count — and each BigOCI
transfer contributes the **latest absolute** `Retries` value from its own
snapshots. Repeated snapshots from one transfer replace that transfer's
contribution; they are never summed. Standard retry updates emit
immediately; standard `WireBytes` updates are folded into later snapshots
rather than emitted per chunk. If bigoci later exposes retry control,
collapsing to one domain is a small follow-up.

## Identity content coding

The spec requires `Accept-Encoding: identity` and identity-only
`Content-Encoding` on every manifest and blob GET: a transparent proxy that
gzips a response would otherwise change the bytes being hashed. Enforcement
follows the provenance of each egress path. Our own manifest adapter enforces
it on manifest and blob paths while leaving token-exchange requests alone —
a token realm that compresses its responses keeps working. go-oci-blob's
transports get the same wrapper. bigoci enforces the invariant natively
upstream (since v0.2.0), including across its manual redirect hops, so no
wrapper is injected there.

## Stage-then-commit and the stored cache

`FetchFiles` never writes a final output until every selected role has been
downloaded and fully verified into private staging. Only then does the commit
phase run: fsync+rename per file, sequential, in canonical order. Any failure
before commit means zero committed outputs. A failure during commit leaves a
committed prefix and an error naming the committed roles — commit is per-file
atomic, not transactional across files. A retry re-stages everything and
re-commits all roles; it never trusts prior outputs.

The tradeoff is disk space and latency in exchange for a strong promise:
callers never observe a torn or unverified output under a final name.

BigOCI fetches add a content-addressed stored cache, keyed by the full stored
digest, so a failed decode does not force a re-pull of a large file. Reuse
never trusts the cache: an entry is re-hashed in full before use, so a
poisoned or corrupted entry can only cause a re-pull, never a wrong output.
Cache entries are removed on successful commit and retained on failure for
reuse.

After a successful BigOCI commit, the cache entries and their lock files are removed, but the empty `<parent>/.imgoci-stage/stored/` directory remains. The directory is reserved library working state, not a deliverable from the release.

## Standard and BigOCI forms

Standard form is the default: one blob per file, streamed with no stored
temporary, digest-verified at EOF. BigOCI is a per-file opt-in for large
files, stored as multiple parts and transferred by bigoci. The producer
enforces the ≥2-part profile: a file whose part plan comes to fewer than two
parts is published in standard form instead, and `Progress.Fallbacks` counts
those fallbacks. On the consumer side the client advertises the BigOCI
capability because the pinned bigoci version passes the interop fixtures.
`Index.Resolve` treats a zero capability set as standard only. `Client.Resolve`
uses standard+BigOCI unless passed `StandardCapabilities` (see the
[capabilities reference](../reference/capabilities.md)).

## Why the CLI and release boundaries are separate

The library is the release unit. Its version tracks the Go API and is
decoupled from spec releases: format compatibility is carried by the imgoci
media types (`.v1`), and a breaking format change requires new type
identifiers. A pure library publishes no binaries.

The CLI lives in `cli/` as a separate module pinned to the library by a local
`replace` directive. That boundary does two jobs. It keeps the CLI's
dependencies out of the library's `go.mod`, so importing the library never
drags in command-line machinery. And it makes the CLI deliberately
uninstallable from a module proxy: it is a private reference tool for watching
the library work against a real registry (see the
[CLI reference](../reference/cli.md)), thin by rule — every flag except
`-timeout` maps onto one public option or query field, `-timeout` applies a
CLI context deadline rather than a library option or query field, and no
transfer logic lives in it. Promoting it to a supported tool has a bar:
a stable spec and external demand.
