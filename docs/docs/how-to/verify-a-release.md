---
title: Verify a release
description: Pin a release by digest, fetch it, and inspect verification failures.
---

# Verify a release

This guide shows how to fetch a release so every file you write is bound to one digest you chose to trust, and how to tell which check failed when a fetch is rejected.

Prerequisites:

- `github.com/imgoci/go` in your module
- the canonical index digest of the release you want, obtained over a channel you trust — `Publish` returns it, and the CLI's `publish` prints it as its only standard output

The library verifies unconditionally. There is no option that skips a check and no fallback to an unverified alternative. Supply the digest pin and read failures with `errors.Is`.

## Pin the reference by digest

A reference may carry a tag, a digest, or both. For verification, include the digest. The retrieved index bytes are hashed and must match that pin, so a registry cannot substitute another document. A tag beside a digest is only a claim about where that tag pointed; the digest wins.

```go
rel, err := client.Fetch(ctx, "ghcr.io/example/os@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
```

After `Fetch`, `rel.Digest()` is the index digest. Later calls address file manifests by digest, so a tag mutation after `Fetch` cannot redirect retrieval.

`Fetch` requires the HTTP response's `Content-Type` to identify the
release-index media type, then runs the same validation as the offline
`ParseIndex`: JSON decoding rejects duplicate keys and invalid UTF-8; the ten
consumer rules of spec section 6 are applied, including canonical descriptor
order; the bytes must be RFC 8785-canonical. Index validation also checks
`io.imgoci.usage` syntax and rejects `install-offline` without `install`.
These usage checks validate the producer's assertion, but do not prove that
the deliverable behaves as asserted. Any index-validation failure wraps
`imgoci.ErrInvalidIndex`. Ten-rule validation failures name the violated rule.
Decode, content-type, and canonical-bytes failures describe the failed check.
The recorded digest is computed from the original input bytes — the library
never re-encodes for identity.

## Verify end to end in Go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	imgoci "github.com/imgoci/go"
)

// The digest recorded when the release was published, delivered over a
// channel you trust. Replace with your release's digest.
const pinned = "ghcr.io/example/os@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func main() {
	client, err := imgoci.New(imgoci.WithDockerCredentials())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	rel, err := client.Fetch(ctx, pinned)
	if err != nil {
		fail(err)
	}

	sel, err := client.Resolve(rel, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "qemu",
		Representation: "qcow2",
		Usage:          []string{"live"},
		Compressions:   []string{"zstd", "none"},
	})
	if err != nil {
		fail(err)
	}

	if err := client.FetchFiles(ctx, rel, sel, imgoci.ToDir("out")); err != nil {
		fail(err)
	}
	fmt.Println("verified against", rel.Digest())
}

func fail(err error) {
	switch {
	case errors.Is(err, imgoci.ErrInvalidIndex):
		fmt.Fprintln(os.Stderr, "retrieved document failed validation:", err)
	case errors.Is(err, imgoci.ErrDigestMismatch):
		fmt.Fprintln(os.Stderr, "bytes did not match a declared digest or size:", err)
	case errors.Is(err, imgoci.ErrDecode):
		fmt.Fprintln(os.Stderr, "stored file failed strict decompression:", err)
	case errors.Is(err, imgoci.ErrSelectionMismatch):
		fmt.Fprintln(os.Stderr, "selection was not derived from this release:", err)
	default:
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}
```

The example requests the exact usage set `live`. Replace it with the complete
set reported by `List`. Use nil or an empty slice only for a deliverable with
the empty usage set; omitting `Usage` does not match a deliverable that carries
usage.

`Resolve` returns a `Resolved` that carries the digest of the index it selected from (`sel.IndexDigest()`). `FetchFiles` refuses to run unless that digest equals `rel.Digest()`, failing with `imgoci.ErrSelectionMismatch` before any network I/O. For every selected entry it then fetches the file manifest by digest. A standard entry pulls one stored blob under an exact size bound. A BigOCI entry reconstructs the multipart stored file and re-verifies that file's stored digest and size. Both forms strictly decode a single unit and check the decoded stream against `io.imgoci.content.digest` and `io.imgoci.content.size`. A digest or size violation is `imgoci.ErrDigestMismatch`; a strict-decompression violation is `imgoci.ErrDecode`; an invalid retrieved manifest is `imgoci.ErrInvalidIndex`.

`FetchFiles` stages and verifies every selected role privately first. Commit runs only after all roles verify: fsync, close, rename, and parent-directory fsync per file, in the order of `sel.Entries()`. Destination preflight failures (an unset `Dest`, a `ToFiles` map missing a selected role or naming an extra one, conflicting paths) are `imgoci.ErrInvalidDest`, reported before any network I/O.

## Expected result

On success, every file `FetchFiles` wrote under `out/` is bound to the pinned digest; files already in that directory that were not selected are untouched. On failure before commit, nothing was committed. A commit-phase failure names the roles already committed. A retry after a partial commit re-stages and re-commits every selected role; it never trusts or skips files already present.

## If a check fails

Match with `errors.Is`; the message keeps the underlying detail. Use the cases in the example above for the verification sentinels. Destination-plan failures match `imgoci.ErrInvalidDest`. A selected file-manifest type outside the client's capabilities matches `imgoci.ErrUnsupportedType`. Registry answers match `imgoci.ErrNotFound` or `imgoci.ErrUnauthorized` and cannot bypass a content check.

The CLI maps the same sentinels one-to-one onto exit codes 3–11; see the [CLI reference](../reference/cli.md). The full catalog is in the [errors reference](../reference/errors.md).

## Related pages

- [Resolve deliverables](resolve-deliverables.md) — building the selection this guide verifies.
- [About the architecture](../explanation/architecture.md) — why verification is structured this way.
- [Error reference](../reference/errors.md) — every public sentinel and how to respond.

Implemented spec revision: imgoci v1 draft, 2026-08-11 ([imgoci/spec](https://github.com/imgoci/spec) commit `5b957102eeda16498fdcb80a738431b83abd4197`).
