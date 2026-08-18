---
id: 008
title: Release v0.1.0 against spec v0.1.0
date: 2026-08-17
status: complete
repos_touched: [imgoci/go]
related_sessions: [005, 007, 009]
---

## Goal

Prepare and ship the first release of imgoci/go now that the spec promoted to
stable v0.1.0: settle the spec-vs-package versioning question, absorb the spec
delta, clear the remaining release blockers, refresh the README, and merge the
Release Please proposal.

## Outcome

Goal met. `v0.1.0` is released: tag on squash commit `2cfe76b`, GitHub release
published 2026-08-18T05:11:04Z, and verified as an external consumer — the
module resolves from proxy.golang.org, the README example compiles against it
verbatim, and the shipped zip carries the corrected `SECURITY.md` with no
`cli/` entries. Four PRs merged this session: #24 (spec pin v0.1.0 +
versioning policy), #25 (Dependabot remediation), #26 (README refresh),
#9 (the release).

## Key Decisions

- Decouple package semver from spec releases -> the spec itself carries format
  compatibility in the media-type suffix (`.v1`; §4 "a breaking change
  requires a new type identifier"), compatibility is a set not a point, and Go
  module majors force `/v2` path churn that spec-number mirroring would
  misalign. Policy recorded in README (compatibility table), CONTRIBUTING.md,
  and `architecture.md`.
- Treat "nothing changed in the spec" as unverified -> the diff between pins
  found one normative change (`a0e61fd`, usage added to the shared-digest
  reuse rules). Rule 8 already permitted it; the new upstream pass fixture now
  proves it in CI instead of by inspection.
- Map the four new upstream fail fixtures to §6 rule 1 after reading
  `validateRule1` rather than guessing from names.
- Fix the Dependabot alerts (docs-only `pymdown-extensions` 10.21.3 -> 11.0.1)
  before release even though the Go module was unaffected -> zero open alerts
  on the released tip.
- Verify release readiness after the session-009 refactors with `apidiff`
  (zero exported-surface differences, `8d02009`..`7a3c419`) instead of
  re-running the functional campaign -> the READY verdict binds to the public
  surface, which was proven unchanged.
- Publish the draft release as part of "verify the release is successful" ->
  `draft: true` in the Release Please config means an unpublished draft is not
  a release.

## Changes

- `testdata/conformance/` — pin `8083159` (spec v0.1.0 tag); fixtures
  13/25 -> 14/29 including `pass/shared-manifest-across-usage-sets.json`.
- `internal/index/{decode_test,producer_test,validate_test}.go` — counts, pin
  constant, four rule-1 fixture mappings, rule-8 comment gains usage.
- `.github/scripts/cue_crosscheck.sh` — minima 14/15/29.
- `README.md` — rewritten per readme-writer/language-style: features,
  install, compiled usage example, Diátaxis docs links, spec compatibility
  table, related projects, contributing/security pointers.
- `CONTRIBUTING.md` — draft-spec release guard replaced by the versioning
  policy.
- `doc.go`, `docs/docs/**` — every draft-era spec reference updated to spec
  release v0.1.0 / commit `8083159`.
- `docs/uv.lock` — `pymdown-extensions` 11.0.1 (GHSA-gm37-52c6-37mw high,
  GHSA-9xwg-3r6f-jcx2 medium).
- `.release-please-manifest.json`, `CHANGELOG.md` — 0.1.0 via PR #9.

## Open Threads

- Session 005 is still `in-progress` in INDEX despite its work being complete
  (blocker fixed in PR #17, REL-04 rerun PASS at `e4b0d53`, verdict READY);
  it needs its own closeout with a SUMMARY.md.
- Future releases: Release Please creates drafts (`draft: true`); each release
  needs a manual publish step after the release PR merges.
- pkg.go.dev indexing of v0.1.0 was not awaited; proxy.golang.org serving was
  verified directly.

## References

- Release: https://github.com/imgoci/go/releases/tag/v0.1.0 (`2cfe76b`)
- Merged: https://github.com/imgoci/go/pull/24, /pull/25, /pull/26, /pull/9
- Spec release: https://github.com/imgoci/spec/releases/tag/v0.1.0 (`8083159`)
- Campaign evidence: `.journal/005/NOTES.md` (2026-08-17 00:45 sign-off entry)

## Lessons

- Read the bound predecessor sessions before repeating their work: the
  "outstanding" SECURITY.md blocker and REL-04 rerun had been done and signed
  off inside session 005's NOTES; only the unclosed session hid it. Stale
  TECH_NOTES bullets compound the misdirection — update them at close, and
  distrust any "X is still required" claim whose source session never closed.
- A release-readiness verdict survives refactors only if the public surface is
  provably unchanged; `apidiff` against the verdict's commit is cheap and
  decisive (and this session's zero-diff result made seven refactor PRs
  release-neutral without re-testing).
- The spec's own versioning answer was already in the spec: format majors live
  in media types, so the package version is free to track the Go API. Look for
  the upstream contract before inventing a reconciliation policy.
