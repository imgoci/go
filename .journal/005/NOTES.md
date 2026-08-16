---
id: 005
title: Release-readiness functional test plan
started: 2026-08-16
---

## 2026-08-16 12:58 — Kickoff

Goal for the session: spawn a planner agent to compose a manual, real-world
functional testing plan for the public surfaces of `imgoci/go`. The plan must
demonstrate that the project is fully ready to be released and that it delivers
on every promise it makes. The final plan document lands in this session folder
and is presented to the user for review.

Current state of the world:

- `master` is at `b4b5921` (PR #15 merged); PLAN slices 0–6 and the
  manual-release documentation follow-up are all on `master`.
- No release or tag exists. Release Please PR #9 proposes `release 0.1.0` with
  `initial-version: 0.1.0` durably configured. The v1 guard stands while the
  spec is draft.
- Session 004 already ran a manual release rehearsal: 101 scenarios through 24
  external probe programs against zot v2.1.20, no release-blocking defect, two
  documentation gaps fixed in PR #15.
- Known coverage gaps carried forward from 004: TLS/custom CAs, cross-host blob
  redirects, external Docker credential helpers, Bearer/OAuth token exchange,
  publish-side retry injection, multi-GiB BigOCI payloads, concurrent same-tag
  publication, and forced commit-phase partial filesystem failure.
- Public surfaces to cover: root `package imgoci` (module
  `github.com/imgoci/go`), the private `cli/` submodule, and the user-facing
  `docs/` set.

Plan: prime this session, then dispatch a planner agent with full repo and
prior-session context to produce the functional test plan document. Store it as
`.journal/005/FUNCTIONAL_TEST_PLAN.md` and present it for review.

## 2026-08-16 13:25 — Plan delivered

Correction to the kickoff entry: `master` is at `0b4be41` ("docs: remove plan
references and over-dense comments (#16)"), not `b4b5921`. PR #16 landed after
session 004's follow-up. The plan is written against `0b4be41`.

Planner agent `ReleaseTestPlanner` produced
`.journal/005/FUNCTIONAL_TEST_PLAN.md` (998 lines): 24 promises, 28 scenarios,
8 phases, verdict criteria, residual risk, execution notes.

Groundedness spot-checks I ran against the working tree before accepting it:

- Exported root surface in the plan matches `go doc -short .` exactly (types,
  functions, options, `Client`/`Index`/`Release`/`Resolved` methods, nine
  sentinels).
- CLI exit mapping 0-11/130/143 matches the `exit*` constants and
  `sentinelExits()` in `cli/run.go`.
- Per-command flag sets match `commonFlags.register`, `registerWorkers`,
  `registerProgress`, `queryFlags.registerList`, and `registerResolve`
  (`resolve` has no workers/progress; `fetch` and `publish` do).
- `imgoci version` line matches `versionLine` in `cli/run.go:100`.
- Progress line format matches `cli/progress.go:148`.
- Empty-command stderr text matches `cli/run.go:143`.

All eight coverage gaps from `TECH_NOTES.md` have concrete scenarios: NET-01
(TLS/custom CA), NET-02 (cross-host redirect), AUTH-01 (external credential
helper), AUTH-02/AUTH-03 (Bearer/OAuth, bare 401), FAIL-01 (publish-side retry
injection), BIG-02 (multi-GiB BigOCI, with a stated 15 GiB budget exception),
RACE-01 (concurrent same-tag publication), FAIL-02 (forced commit-phase partial
filesystem failure). The plan declares CLI exit `10` unreachable through the
shipped grammar and records it as residual risk rather than faking it.

Next: user review of the plan, then execute it.
