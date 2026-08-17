---
title: Error reference
description: Every public sentinel error, what produces it, and how to respond.
---

# Error reference

The public error surface is nine sentinel values in `errors.go`. Failures that
carry a sentinel wrap it, so match with `errors.Is`. Most messages keep the
underlying detail. On the standard blob path, `go-oci-blob` can redact a
transport cause from the top-level message while retaining it in the
`errors.Unwrap` chain. This page describes the implemented spec revision:
imgoci v1 draft, 2026-08-16 (`imgoci/spec` commit `46d18b74cc407ac7d61ded7692fc42b644f4d1e2`).

The private reference CLI maps each sentinel onto a fixed exit code; see the
[CLI reference](cli.md#exit-codes).

## Sentinels

| Sentinel | Produced by | Meaning | Caller response |
|---|---|---|---|
| `ErrNotFound` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | The registry does not hold the requested release, manifest, or blob. | Check the reference and repository; nothing local to fix. |
| `ErrUnauthorized` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | The registry refused a request for lack of credentials or insufficient permission. | Supply credentials (`WithCredentials` or `WithDockerCredentials`) or fix registry permissions. |
| `ErrInvalidIndex` | `ParseIndex`, `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | A retrieved imgoci document is invalid: the release index failed a spec section 6 rule (decode, structure, canonical bytes, identity), an `io.imgoci.usage` value has invalid syntax, a usage set contains `install-offline` without `install`, the index response `Content-Type` did not identify the index type, a retrieved file manifest failed its rules — including a BigOCI profile violation — or `Client.Publish` read back an invalid BigOCI file manifest or profile after multipart push. | Do not retry with the same bytes; the producer published a non-conforming document. The wrapped error names the failed check. |
| `ErrInvalidSpec` | `Client.Publish` | A producer-side specification violation: an illegal publish reference form (digest-only, tag+digest, or name-only), a producer rule 1–8 failure, an `io.imgoci.usage` value that is neither public (`live`, `install`, or `install-offline`) nor private-form (`x-<owner>-<name>`), invalid UTF-8 in a caller string, a reserved `io.imgoci.*` annotation key, a negative `MultipartSpec.PartSize`, or inconsistent shared sources. | Fix the `ReleaseSpec` or the reference; nothing was written. |
| `ErrInvalidDest` | `Client.FetchFiles` | The fetch destination plan failed preflight: a zero `Dest`, a `ToFiles` map missing a selected role or naming an extra one, duplicate resolved paths, a path that is an existing directory, or a shadowed staging reservation. | Fix the destination; preflight fails before any network I/O. |
| `ErrDigestMismatch` | `Client.Fetch`, `Client.FetchFiles`, `Client.Publish` | Retrieved or published bytes did not match a declared digest or size: a fetched index that fails the reference's digest pin, a manifest or blob that fails verification, a decoded stream that exceeds its declared size, or a `Source` that changed between pass 1 and upload. | On fetch: retry may help against a transient corruption, but a stable mismatch means the published content is wrong. On publish: stop mutating the `Source` during `Publish`. |
| `ErrUnsupportedType` | `Index.Resolve`, `Client.Resolve`, `Client.FetchFiles` | A selected file-manifest type is outside the consumer capability set: capability filtering left a selected role with no remaining transport alternative, or a selected entry's `ArtifactType` is outside `Client.Capabilities`. | Widen the capability set with `NewCapabilities` (see the [capabilities reference](capabilities.md)) or select a different deliverable. |
| `ErrSelectionMismatch` | `Client.FetchFiles` | The `Resolved` value was not derived from the release being retrieved. Binding is by canonical index digest, not pointer identity. | Re-run `Client.Resolve` against the `Release` you are fetching from. |
| `ErrDecode` | `Client.FetchFiles`, `Client.Publish` | Strict decompression of a stored file failed — for example a two-member gzip stream, or a zstd frame or xz stream that needs a decoder working set above the configured ceiling (128 MiB by default). The producer path is as strict as the consumer: `Publish` fails such a file before any upload. | Re-encode the stored file as one compression unit. For a working-set rejection, either re-encode with a smaller window or dictionary, or raise the ceiling with `WithDecoderMaxWindow`. |

Registry membership alone never causes `ErrInvalidIndex`. A retrieved index may
contain a syntactically valid unknown or private usage value; consumer
validation preserves it. Consumer validation still rejects usage syntax and
canonical-encoding violations and `install-offline` without `install`.

`ErrNotFound`, `ErrUnauthorized`, and `ErrDigestMismatch` on the fetch path
wrap the transfer orchestrator's detail; `ErrDigestMismatch` also covers a
declared size a stored file disagrees with in either direction, because a
size bound is digest discipline. See
[stored-file size verification](#stored-file-size-verification).

## Decode working-set limits

A zstd frame declares the window it must be decoded against, and an xz stream
declares the LZMA2 dictionary capacity it must be decoded against. The decoder
reads that declared value out of the frame or block header and refuses the file
with `ErrDecode` *before* allocating the buffer. This working-set bound is
independent of the file's decoded content size, which is bounded separately by
`io.imgoci.content.size`.

One ceiling covers both codecs. It defaults to **128 MiB** and is configured
with `WithDecoderMaxWindow`:

```go
client, err := imgoci.New(imgoci.WithDecoderMaxWindow(32 << 20))
```

Zero is rejected by `New`: it is not a way to turn the bound off.

The default is the ceiling mainstream producer output needs. It is the zstd
command-line tool's own default decode limit (`windowLog` 27), so a frame from
`zstd --long=27` is accepted, and it covers the 64 MiB dictionary of the xz
command-line tool's `-9` preset. Lowering the ceiling rejects such files;
raising it accepts more hostile ones.

The bound applies to **each active decoder**, not to a transfer as a whole, and
it is joint across the two codecs: one number, whichever codec a given file
uses. A transfer decoding several roles at once therefore holds one working set
per role in flight, so peak decoder memory is roughly the ceiling times the
worker count set by `WithWorkers`. Raising both at the same time multiplies
them.

The same ceiling applies on publish. Pass 1 decodes every source with it, so a
producer cannot write a release that a consumer running the same configuration
would refuse to read back.

## Stored-file size verification

Spec section 8 has the consumer verify a file layer's digest **and** size, so
`Client.FetchFiles` treats `layers[0].size` as an equality check rather than a
ceiling. A stored file that runs past the declared size fails, and so does one
that ends before it — a manifest declaring `N+1` bytes over a blob of exactly
the `N` bytes its layer digest names is rejected even though every digest in
the release is correct, because the digest alone cannot catch an overstated
length. Both directions are integrity failures and surface as
`ErrDigestMismatch`, never as `ErrDecode`, even when the disagreement is
noticed while a gzip, xz, or zstd stream is being decoded. Nothing is
committed: the transfer stages every role and only renames destinations into
place once all of them verify.

## Offline Resolve failures

`Index.Resolve` returns a descriptive error **without a matchable sentinel**
for the offline selection failures of spec section 7.3:

- no deliverable matches the query,
- a selected role is absent from the matching deliverable,
- no accepted compression remains for a role.

Only the capability filter wraps `ErrUnsupportedType`. Invalid queries (an
empty required selector, a non-nil empty `Roles` slice, a malformed or
duplicate usage value, a non-nil empty list-query `Usage` slice, a duplicate
or unknown compression token, or another malformed basic token) and
nil-receiver errors also have no distinguishing sentinel. Those three classes
— selection found nothing, invalid query, and nil receiver — cannot be told
apart with `errors.Is`. Do not parse the error text.

`Index.List` likewise returns descriptive errors without a sentinel for
invalid queries and a nil receiver. Invalid `ListQuery.Usage` and
`ResolveQuery.Usage` values are ordinary argument errors.

## Unclassified errors

Some failures match no sentinel and come back unchanged. Treat them as
ordinary errors; the CLI exits `1` for them.

- A malformed `Reference` is a caller error: it is neither `ErrInvalidIndex`
  (reserved for retrieved documents) nor `ErrInvalidSpec` (producer-only,
  including the tag-only publish contract). A name-only reference passed to
  `Client.Fetch` is the same kind of error.
- Nil arguments: a nil `Client`, `Release`, or `Index` reaching a method.
- `NewUsage` returns an ordinary argument error for a malformed token, a
  canonical set longer than 4096 bytes, or `install-offline` without
  `install`. These errors match no public sentinel.
- `WithWorkers` with a non-positive count, rejected before any I/O.
- `New` failing to read an existing but unreadable Docker configuration file.
- Network and transport errors that match nothing public.
- On publish, an index self-oracle failure (the library's own output failing
  its own validator) stays unclassified on purpose: it is a bug report, not a
  caller mistake.
- A `401 Unauthorized` response without `WWW-Authenticate` cannot start an
  authentication exchange. It matches no public sentinel and returns
  `the registry refused the request without saying how to authenticate`;
  the CLI exits `1`.
- `Client.Publish` rejects a syntactically valid but unsupported compression,
  such as `x-ft-brotli`, before registry I/O. The error matches no public
  sentinel.
- A standard blob transport failure may render as `registry request failed`.
  The underlying cause remains in the `errors.Unwrap` chain. For example, a
  proxy that applies a non-identity content coding retains
  `the response is not identity coded` in that chain. The BigOCI path
  reports that cause directly.
