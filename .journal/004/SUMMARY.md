---
id: 004
title: Manual release rehearsal
date: 2026-08-16
status: complete
repos_touched: [imgoci/go]
related_sessions: [003]
---

## Goal

Manually exercise the complete public imgoci Go library against one local registry before the first release, identify release blockers, and land the required follow-up changes.

## Outcome

Goal met. A functional-testing agent exercised 101 manual scenarios through 24 external probe programs against zot v2.1.20. Standard and BigOCI flows, all supported compression modes, selection, integrity, filesystem behavior, progress, retries, cancellation, authentication, and CLI interoperability passed without a release-blocking correctness or safety defect. PR #15 merged as squash commit `b4b5921`, correcting the two documentation issues required before release. No release or tag was created.

## Key Decisions

- Use one registry implementation with anonymous and Basic-auth configurations -> covered the relevant behavior without expanding into the deferred registry-specific matrix.
- Drive the public API from disposable external programs instead of running the repository suite -> the evidence reflects consumer-visible network and filesystem behavior rather than existing automated assertions.
- Treat registry-dependent digest retention and the undocumented 8 MiB decode working-set ceiling as pre-release documentation blockers -> both could mislead users even though the implementation behaved correctly.
- Defer non-blocking diagnostics and classification findings -> none accepted invalid bytes, wrote unverified output, exposed credentials, or required a breaking pre-release correction.

## Changes

- `docs/docs/tutorials/first-release.md` - qualified digest-pinned availability against registry retention and garbage collection.
- `docs/docs/reference/errors.md` - documented the 8 MiB zstd window and xz LZMA2 dictionary ceilings and updated `ErrDecode` guidance.
- PR #15 - merged the documentation follow-up after strict MkDocs and rendered-surface verification.

## Open Threads

- PR #9 still proposes the guarded first v0.1.0 release; it remains unmerged.
- Manual coverage still excludes TLS/custom CAs, cross-host blob redirects, external Docker credential helpers, Bearer/OAuth token exchange, publish-side retry injection, multi-GiB BigOCI payloads, concurrent same-tag publication, and forced commit-phase partial filesystem failure.
- Non-blocking findings remain: misleading zstd single-segment window diagnostics, unclassified unsupported publish compression and bare-401 errors, one misnamed canonical fixture, sparse `ReleaseSpec` name/version grammar comments, and the tutorial's port 5000 conflict with macOS AirPlay.

## References

- Merged follow-up: https://github.com/imgoci/go/pull/15
- Pending release proposal: https://github.com/imgoci/go/pull/9
- Prior implementation session: `.journal/003/SUMMARY.md`

## Lessons

- A digest pins identity, not retention. Registry garbage collection can remove an untagged manifest even when the client still holds its digest.
- Security-oriented decoder memory bounds are part of the interoperability contract and must be documented alongside compression support.
- External consumer probes found documentation and classification gaps that the green release gate did not expose.
