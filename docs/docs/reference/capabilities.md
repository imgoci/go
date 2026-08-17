---
title: Capabilities reference
description: Consumer capability sets, their defaults, validation rules, and comparison semantics.
---

# Capabilities reference

A `Capabilities` value is a validated set of file-manifest types a consumer
can retrieve. It contains only file-manifest types; it does not describe
deliverable usage. The value is shared by offline `Index.Resolve` and the
network client. This page
describes the implemented spec revision: imgoci v1, spec release v0.1.0
(`imgoci/spec` commit `8083159daebe15dc1d78da3e8a03b6b80526d427`).

## Media types

| Type | Meaning |
|---|---|
| `application/vnd.imgoci.file.v1` | The imgoci v1 standard file-manifest type. Every capability set must include it. |
| `application/vnd.bigoci.file.v1` | The BigOCI file-manifest type, for multipart-stored files. |

## Zero value and defaults

A zero `Capabilities` is not one meaning everywhere:

- `Index.Resolve` treats it as `StandardCapabilities()`.
- `Client.Resolve` treats it as `Client.Capabilities()` — standard plus
  `application/vnd.bigoci.file.v1` — unless the caller passes
  `StandardCapabilities()` explicitly.

BigOCI is never assumed on the offline path. `Client.Resolve` uses
standard+BigOCI unless passed `StandardCapabilities`.

```go
func StandardCapabilities() Capabilities
```

Returns the set containing only `application/vnd.imgoci.file.v1`. This is the
zero-value default for `Index.Resolve`.

`Client.Capabilities()` reports what a built client can retrieve conformingly:
the standard type plus `application/vnd.bigoci.file.v1`. BigOCI is advertised
because the pinned bigoci version (v0.2.0) passes the interop suite; see
[About the architecture](../explanation/architecture.md#standard-and-bigoci-forms).

`Index.List` never filters by capabilities: a listing shows every stored
transport alternative, including types the consumer cannot retrieve.

## Usage selectors

Consumer capabilities do not filter `io.imgoci.usage`. A syntactically valid
unknown or private usage value remains listable. It is also resolvable when the
resolve query supplies the deliverable's complete usage set and the other
selectors match. This follows spec section 7.3: a consumer can select and
verify a deliverable without understanding what a usage value means.

Usage values are producer assertions, not file-manifest capabilities.
Validation and retrieval do not prove that a deliverable is bootable,
installable, or offline-installable.

## NewCapabilities validation

```go
func NewCapabilities(types ...string) (Capabilities, error)
```

Validates `types` as a consumer capability set. All of the following must
hold, or the zero value and a descriptive error are returned:

1. No value contains parameters (no `;`).
2. Every value is an RFC 6838 `type/subtype`: exactly one slash separating two
   restricted names. A restricted name is 1 to 127 characters, starts with an
   ASCII letter or digit, and continues with ASCII letters, digits, or
   `!#$&^_.+-`.
3. No duplicates after ASCII case folding.
4. The set includes `application/vnd.imgoci.file.v1`, compared
   case-insensitively.

The stored set is normalized to ASCII lowercase. Duplicates after ASCII
folding are rejected, not removed.

## Comparison semantics

Media types in this format are ASCII, and spec section 4 comparison is ASCII
case-insensitive. `EqualMediaType` implements it:

- Two strings of different lengths are never equal.
- Only ASCII `A`–`Z` fold to lowercase. Non-ASCII bytes are compared as-is, so
  Unicode case pairs such as U+017F (long s) and U+212A (Kelvin sign) do not
  fold — `Kelvin/K` tricks cannot smuggle a type past the filter.
- Parameters are never compared; a value carrying them fails validation
  before comparison. HTTP `Content-Type` headers, the one place parameters
  legally appear, are stripped by the registry adapter before comparison.

Capability filtering during `Resolve` uses the same comparison against each
entry's `ArtifactType`. When filtering leaves a selected role with no
remaining transport alternative, `Resolve` fails with `ErrUnsupportedType`
(see the [error reference](errors.md)). `Client.FetchFiles` re-checks that
every selected entry's `ArtifactType` is in `Client.Capabilities()` before any
network I/O.

## CLI

The private CLI's `-capability` flag builds a set through `NewCapabilities`,
so a set given on the command line must include the standard type. Unset, the
client's own capability set applies. See the
[CLI reference](cli.md#imgoci-resolve).
