---
title: Error reference
description: Every public sentinel error, what produces it, and how to respond.
---

# Error reference

The public error surface is nine sentinel values in `errors.go`. Failures wrap
a sentinel, so match with `errors.Is`; the message keeps the underlying
detail. This page describes the implemented spec revision: imgoci v1 draft,
2026-08-11 (`imgoci/spec` commit `5b957102eeda16498fdcb80a738431b83abd4197`).

The private reference CLI maps each sentinel onto a fixed exit code; see the
[CLI reference](cli.md#exit-codes).

## Sentinels

| Sentinel | Produced by | Meaning | Caller response |
|---|---|---|---|
| `ErrNotFound` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | The registry does not hold the requested release, manifest, or blob. | Check the reference and repository; nothing local to fix. |
| `ErrUnauthorized` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | The registry refused a request for lack of credentials or insufficient permission. | Supply credentials (`WithCredentials` or `WithDockerCredentials`) or fix registry permissions. |
| `ErrInvalidIndex` | `ParseIndex`, `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | A retrieved imgoci document is invalid: the release index failed a spec section 6 rule (decode, structure, canonical bytes, identity), the index response `Content-Type` did not identify the index type, a retrieved file manifest failed its rules — including a BigOCI profile violation — or `Client.Publish` read back an invalid BigOCI file manifest or profile after multipart push. | Do not retry with the same bytes; the producer published a non-conforming document. The wrapped error names the failed check. |
| `ErrInvalidSpec` | `Client.Publish` | A producer-side specification violation: an illegal publish reference form (digest-only, tag+digest, or name-only), a producer rule 1–8 failure, invalid UTF-8 in a caller string, a reserved `io.imgoci.*` annotation key, a negative `MultipartSpec.PartSize`, or inconsistent shared sources. | Fix the `ReleaseSpec` or the reference; nothing was written. |
| `ErrInvalidDest` | `Client.FetchFiles` | The fetch destination plan failed preflight: a zero `Dest`, a `ToFiles` map missing a selected role or naming an extra one, duplicate resolved paths, a path that is an existing directory, or a shadowed staging reservation. | Fix the destination; preflight fails before any network I/O. |
| `ErrDigestMismatch` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | Retrieved or published bytes did not match a declared digest or size: a fetched index that fails the reference's digest pin, a manifest or blob that fails verification, a decoded stream that exceeds its declared size, or a `Source` that changed between pass 1 and upload. | On fetch: retry may help against a transient corruption, but a stable mismatch means the published content is wrong. On publish: stop mutating the `Source` during `Publish`. |
| `ErrUnsupportedType` | `Index.Resolve`, `Client.Resolve`, `Client.FetchFiles` | A selected file-manifest type is outside the consumer capability set: capability filtering left a selected role with no remaining transport alternative, or a selected entry's `ArtifactType` is outside `Client.Capabilities`. | Widen the capability set with `NewCapabilities` (see the [capabilities reference](capabilities.md)) or select a different deliverable. |
| `ErrSelectionMismatch` | `Client.FetchFiles` | The `Resolved` value was not derived from the release being retrieved. Binding is by canonical index digest, not pointer identity. | Re-run `Client.Resolve` against the `Release` you are fetching from. |
| `ErrDecode` | `Client.FetchFiles`, `Client.Publish` | Strict decompression of a stored file failed — for example a two-member gzip stream, a zstd frame whose window exceeds 8 MiB, or an xz stream whose LZMA2 dictionary exceeds 8 MiB. The producer path is as strict as the consumer: `Publish` fails such a file before any upload. | Re-encode the stored file as one compression unit. For zstd and xz, configure a window or dictionary no larger than 8 MiB. |

`ErrNotFound`, `ErrUnauthorized`, and `ErrDigestMismatch` on the fetch path
wrap the transfer orchestrator's detail; `ErrDigestMismatch` also covers a
size bound exceeded during decode, because a size bound is digest discipline.

## Decode working-set limits

The decoder rejects a zstd frame whose declared window exceeds 8 MiB and an
xz stream whose LZMA2 dictionary exceeds 8 MiB. It checks the declared value
before allocating the decoder buffer. This working-set bound is independent of
the file's decoded content size.

The zstd command-line encoder's default window through compression level 19 is
within the limit. Higher levels and `--long` can exceed it. The xz command-line
encoder's `-9` preset uses a 64 MiB dictionary and exceeds the limit. Configure
either encoder with an explicit window or dictionary of at most 8 MiB when
producing imgoci files.

## Offline Resolve failures

`Index.Resolve` returns a descriptive error **without a matchable sentinel**
for the offline selection failures of spec section 7.3:

- no deliverable matches the query,
- a selected role is absent from the matching deliverable,
- no accepted compression remains for a role.

Only the capability filter wraps `ErrUnsupportedType`. Invalid queries (an
empty required selector, a non-nil empty `Roles` slice, a duplicate or
unknown compression token, a malformed basic token) and nil-receiver errors
also have no distinguishing sentinel. Those three classes — selection found
nothing, invalid query, and nil receiver — cannot be told apart with
`errors.Is`. Do not parse the error text.

`Index.List` likewise returns descriptive errors without a sentinel for
invalid queries and a nil receiver.

## Unclassified errors

Some failures match no sentinel and come back unchanged. Treat them as
ordinary errors; the CLI exits `1` for them.

- A malformed `Reference` is a caller error: it is neither `ErrInvalidIndex`
  (reserved for retrieved documents) nor `ErrInvalidSpec` (producer-only,
  including the tag-only publish contract). A name-only reference passed to
  `Client.Fetch` is the same kind of error.
- Nil arguments: a nil `Client`, `Release`, or `Index` reaching a method.
- `WithWorkers` with a non-positive count, rejected before any I/O.
- `New` failing to read an existing but unreadable Docker configuration file.
- Network and transport errors that match nothing public.
- On publish, an index self-oracle failure (the library's own output failing
  its own validator) stays unclassified on purpose: it is a bug report, not a
  caller mistake.
