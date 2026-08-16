---
id: 003
title: Continue phased implementation plan
date: 2026-08-16
status: complete
repos_touched: [imgoci/go]
related_sessions: [001, 002]
---

## Goal
Complete the remaining Slice 6 polish from the session-001 plan: Docker credential-store authentication, unified transfer progress, a private reference CLI, Diátaxis documentation, and a safe first v0.x release proposal. Stop before publishing a release.

## Outcome
Goal met. PR #14 merged as squash commit `cf70874`, completing Slice 6 on `master`. The full local gate and GitHub CI passed. Release Please updated PR #9 from the prohibited v1 proposal to `chore(master): release 0.1.0`; no release or tag was created.

## Key Decisions
- Keep Docker credential lookup opt-in through `WithDockerCredentials` -> anonymous behavior remains the default, and unusable local Docker configuration cannot break existing clients.
- Run credential helpers with the caller's context and a 10-second helper ceiling -> cancellation reaches subprocesses without allowing a stuck helper to hang a transfer.
- Report transfer progress as serialized absolute snapshots across standard and BigOCI paths -> consumers receive one monotonic contract instead of transport-specific callbacks or deltas.
- Observe standard-path retries through per-operation context state -> no global state, no public retry abstraction, and no retry loop around BigOCI's independent internal budget.
- Keep `cli/` private, standard-library based, and replace-wired to the root module -> it remains a reference and verification surface rather than a separately released compatibility promise.
- Set Release Please `initial-version` to `0.1.0` in durable configuration -> future regeneration stays pre-v1 while the imgoci specification remains draft.
- Use thin adversarial review passes before the gate -> they found cancellation, error-classification, stream-serialization, retry-accounting, and documentation-contract defects that package tests alone did not establish.

## Changes
- `client.go`, `internal/auth/` - added an ORAS-backed Docker credential store, platform-aware configuration lookup, credential-helper execution, redacted typed failures, and client context propagation.
- `progress.go`, `internal/transfer/`, `internal/multipart/` - unified standard and multipart progress fields, serialized callbacks, wire-byte and retry accounting, monotonic totals, and exactly-once terminal snapshots.
- `cli/` - added private `publish`, `list`, `resolve`, and `fetch` commands with deterministic machine output, guarded diagnostics, signal handling, and real-registry end-to-end coverage.
- `docs/docs/`, `docs/mkdocs.yml`, `CONTRIBUTING.md` - added the first-release tutorial, task-focused how-to guides, API/CLI/error/capability references, architecture explanation, and complete navigation.
- `release-please-config.json` - fixed the first proposal at v0.1.0 while preserving the draft release guard.
- `moon.yml`, `.moon/workspace.yml` - integrated the CLI module and its format, lint, build, test, and end-to-end gates into `root:check`.

## Open Threads
- PR #9 (`chore(master): release 0.1.0`) is open for a future reviewed release. Do not merge a v1 proposal while the specification remains draft.
- Post-v1 architecture items remain intentionally deferred: stored-cache retention tuning, native Windows behavior verification beyond compile coverage, shared auth extraction, and migration to stdlib `jsontext` after it leaves GOEXPERIMENT.
- The private CLI remains unversioned and unreleased by design.

## References
- Merged implementation: https://github.com/imgoci/go/pull/14
- Deferred release proposal: https://github.com/imgoci/go/pull/9
- Architecture: `.journal/001/ARCHITECTURE.md`
- Implementation plan: `.journal/001/PLAN.md`
- Prior implementation session: `.journal/002/SUMMARY.md`

## Lessons
- A progress API can be monotonic yet still wrong when retry updates are silent; every observable state transition must emit through the same serialized boundary.
- Subprocess-based credential adapters must inherit caller cancellation in addition to enforcing their own timeout. A detached background context turns cancellation into a misleading client-level guarantee.
- Real-registry CLI tests exposed output, exit-code, and serialization contracts that unit tests could not prove.
