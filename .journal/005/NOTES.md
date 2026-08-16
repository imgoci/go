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

## 2026-08-16 14:05 — Phase 1 executed

Plan approved by the user; Phase 1 run with three `functional-tester` agents
(cap of 4 respected): `CM01Tester`, `DOC01Tester`, `DOC02Tester`. I bootstrapped
the shared environment inline first (evidence root, tool pins, image digests,
`$FT/consumer-local`, `$FT/bin/imgoci`, shared `registry:2` on 127.0.0.1:5100).
Evidence root: `/tmp/imgoci-functional-0b4be41/evidence`.

Verdicts: **no release blockers. Phase 1 stop rule not triggered.**

- `CM-01` PASS. External module `ft.local/imgoci-consumer` compiled and ran:
  exit 0, stdout byte-exact `consumer-smoke ok\n` (18 bytes), stderr 0 bytes.
  Every symbol the plan lists exists under import identifier `imgoci`; the
  plan's surface inventory matched reality with zero delta (22 types, 15
  functions, 9 sentinels). Build graph is 250 packages / 10 non-stdlib modules,
  0 `imgoci/go/cli` packages, 0 test-only deps. `go list -m all` = 64 modules.
- `DOC-01` NON-BLOCKING FINDING. Full tutorial walkthrough verified
  byte-for-byte on the substituted port: version line exact, `/v2/` 200,
  publish stdout one 72-byte digest line with all diagnostics on stderr, list
  stdout tab-exact, resolve 9 fields with content digest == `shasum disk.img`
  and size 1048576, fetch stdout empty, `cmp` identical, digest-pinned list
  immediate, `--rm` container gone after stop.
- `DOC-02` NON-BLOCKING FINDING. Strict docs build exit 0; all 10 nav targets
  200 with expected titles; 777 anchors checked, 0 failures, 0 dangling;
  copied `resolve-deliverables` and `verify-a-release` examples ran from an
  external module (`verified against sha256:61e6c55f…`); contract audit matched
  source on decode ceilings, per-command CLI flags, exit codes 0-11/130/143,
  capability defaults, retry attempts (4), 4096-part cap, 512 MiB part size,
  and the "CLI is never released" claim.

Findings raised (both non-blocking under the plan's stated rules):

1. `DOC-01-F1` — the tutorial's port 5000 is owned by macOS ControlCenter
   (AirPlay Receiver). Sharper than the plan predicted: `docker run -p 5000:5000`
   *succeeds* (no "already allocated"), so the failure surfaces one step later as
   the documented `curl` returning `403` with `Server: AirTunes/940.23.1` and
   exit 22. Walkthrough passes with 5000→5500 substituted consistently.
   Suggested fix: publish the tutorial on a less contested port, or warn that
   the bind can silently succeed while curl answers from AirTunes.
2. `DOC-02-F1` — `docs/docs/index.md` names `spec v1 draft, 2026-08-11` but
   omits spec commit `5b95710…`. Verified independently: 9 of 10 pages carry the
   commit; only the landing page does not.
   `DOC-02-F2` is the already-classified sparse `ReleaseSpec` name/version
   grammar, deferred to `ADV-04`.

Observations below the finding threshold: the tutorial's "What you need" list
omits `cmp`, which its final step uses.

Process notes:

- I dirtied `$REPO/go.mod` during bootstrap by running `mise exec -C "$REPO" --
  go mod edit`, which executes in the repo directory. Reverted immediately with
  `git checkout -- go.mod`; tree verified clean before the agents started and
  after they finished. Agents were told to use the pinned Go binary by absolute
  path (`/Users/josh/.local/share/mise/installs/go/1.26.5/bin/go`) and never
  `mise exec -C $REPO`. No agent mutated the repository.
- `DOC-02` used its own module `$FT/consumer-docs` and published its own release
  to `127.0.0.1:5100/docs/example:v1` instead of sharing `$FT/consumer-local`
  and DOC-01's torn-down registry. Both deviations were recorded; they avoid a
  `go.mod` race and a cross-agent teardown dependency.
- DOC-01's literal `go build` used the reader's plain `go1.26.4` (goenv shim),
  which `GOTOOLCHAIN=auto` upgraded to 1.26.5 for the build; buildinfo confirms
  `go1.26.5`. No pinned-toolchain substitution was needed.
- Host artifact: global git config rewrites `https://github.com/` to SSH, so the
  literal HTTPS clone went over SSH. Anonymous HTTPS reachability was proven
  separately with a neutralized git config.
- Shared `imgoci-ft-dist` registry left running on 127.0.0.1:5100 for later
  phases. Port-8000 docs server stopped; no stray `imgoci-zot` container.

Next: Phase 2 (core library contracts, `LIB-01`..`LIB-04`) on the user's go.
