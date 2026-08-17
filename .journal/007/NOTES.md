---
id: 007
title: Session opened, goal pending
started: 2026-08-16
---

## 2026-08-16 20:03 — Kickoff
Goal for the session: not yet stated. The developer asked only to create a new
session; the actual request follows.

Current state of the world:
- `master` is `9b144ae` (PR #20 squash, session 006 conformance remediation). The
  local `master` worktree is clean but 3 commits behind `origin/master`.
- All PLAN slices 0-6, the manual-release follow-up, and the spec-conformance
  remediation are merged. Root and private `cli/` modules pass `root:check`
  (format, lint, build, Windows build, race tests, real-registry e2e, strict
  docs, conformance).
- Release Please PR #9 (`chore(master): release 0.1.0`) is still open. No release
  or tag exists. The draft-spec guard from TECH_NOTES.md still applies: never
  merge a v1 proposal while `~/code/imgoci/spec/spec.md` is draft.
- Session 005 is still `in-progress` in `INDEX.md` (release-readiness functional
  test plan, no `SUMMARY.md`). Its one release blocker was the distributed
  `SECURITY.md` carrying author-facing template directions; `0.1.0` sign-off
  needs that fix plus a passing `REL-04` rerun.
- Journal branch `journal/jmgilman` is at `1b03267` and in sync with origin.

Plan: wait for the developer's actual request, then load task-relevant skills
and any matching prior-session history before starting work.

## 2026-08-16 20:15 — Goal: absorb the spec usage selector
Actual request: review the newest spec PR in `~/code/imgoci/spec` and update this
implementation to match; first spawn a planning agent for the update plan.

Spec delta reviewed. New spec tip is `46d18b74cc407ac7d61ded7692fc42b644f4d1e2`
(PR #17, `feat(spec): add deliverable usage selector`); we pin
`5b957102eeda16498fdcb80a738431b83abd4197`. The local spec checkout was behind
one commit; I fetched but did not move its branch. Snapshot for agents:
`/tmp/imgoci-spec-46d18b7/` plus `/tmp/imgoci-spec-46d18b7-usage.diff`.

Substance of the change: a new optional file-entry annotation `io.imgoci.usage`
carrying a canonical, comma-separated, strictly ascending, unique basic-token set
(<= 4096 bytes; absent = empty set; present empty string invalid). It joins the
deliverable key `(architecture, target, representation, usage)` and the file key,
the §6 rule 5 uniqueness tuple, the §9 producer sort tuple, and the §5.4
registry (`live`, `install`, `install-offline`, with `install-offline` requiring
`install` as a CONSUMER rule). §7.2 list filters by containment and must report
each deliverable's exact usage set; §7.3 resolve requires a complete usage set
matched for exact equality. Five new conformance fixtures (1 pass, 4 fail).

Touch points confirmed by reading: `internal/index/{decode,validate,sort,build,
producer}.go`, root `entry.go`/`list.go`/`resolve.go`/`index.go`/`publish.go`,
`internal/transfer/publish.go`, the whole `cli/` query and output surface, the
vendored conformance corpus plus counts in `internal/index/decode_test.go`
(12/21 -> 13/25), the repo-owned `testdata/canonical` corpus and its
phase-pinned fail map, and seven docs pages.

Binding decisions I made before delegating: keep both `Selector` structs
comparable by storing the canonical serialized usage string (rule 5 uses
`map[Selector]int`; the §9 sort must stay allocation-free), give the public API a
`Usage` domain type whose constructor sorts/dedupes/validates so producers never
hand-sort, make `ListQuery.Usage` a containment filter and `ResolveQuery.Usage`
an exact set where nil and empty both mean the empty set, put the §5.4 usage
registry in producer-only code but the `install-offline` -> `install`
relationship in `Validate`, and bump the pin through
`.github/scripts/sync_conformance.sh --pin`.

Next: read the planner's proposal, then decide scope and sequencing.

## 2026-08-16 20:40 — Planner proposal landed
`UsagePlanner` (planner agent, 18m) returned a 400-line plan; stored verbatim at
`.journal/007/UPDATE_PLAN.md`. Shape: 7 ordered steps, each leaving the tree
compiling — internal value layer (`internal/index/usage.go` as the single
canonicalization site) -> §6 rules and §9 order -> public `Usage` type plus list
and resolve -> producer registry and publish -> CLI -> pin/fixtures -> docs.

It accepted every binding decision and corrected two things in my brief:
- There is no §7.4 in this spec revision; the unsupported-value permission is the
  last paragraph of §7.3 (lines 760-763). Verified.
- I missed three normative points: §5.3:391-392 (never infer usage from a
  filename), §5.4:502-513 (producers must declare every applicable standard
  usage; usage is representation-independent), §5.4:513-514 (usage is a producer
  assertion that validation and retrieval cannot prove). Only syntax,
  relationship, and registry membership are mechanically enforceable.

Design refinements worth keeping: the public type is `struct{canonical string}`
rather than `type Usage string`, so `imgoci.Usage("bad,,value")` cannot bypass the
constructor while the value stays comparable; `Descriptor.Selector().Usage` may
hold raw noncanonical bytes between `Decode` and `Validate`, so nothing may key,
sort, or select on it before validation; `formatSelector` renders the empty set as
`<empty>`; CLI TSV gains the usage column right after representation and renders
the empty set as an empty field (fixed column count, lossless).

Claims I verified independently: `cue_crosscheck.sh` has the `require_json_count`
minima (12/12/21) the plan wants raised, and it vets `conformance/v1/fail` through
`cue vet` expecting rejection — all four new spec fail fixtures are rejected by
the updated CUE (`#UsageConstraint` minRunes for present-empty, the order loop for
duplicate/descending, the relationship check for bare `install-offline`);
`failFixtureRule` in `internal/index/validate_test.go` and `canonicalFailPhases`
in `parse_test.go` exist as described; `internal/transfer/publish.go` has the
`fileID` identity key at line 275.

Gap found in the plan: `cli/registry_test.go:68,85` asserts full TSV rows against
a real registry and is not in its step-5 file list. Adding the usage column breaks
those two assertions, so that file must be edited with `output_test.go`.

Open behavioral consequence to keep in view: resolve's usage list is an exact set
with nil meaning the empty set, so an existing caller resolving a deliverable that
the producer later republishes with non-empty usage stops matching. That is the
spec's requirement (§7.3 exact usage-set equality), not a regression, but it is
the sharpest break for consumers.

Next: implement, starting with step 1.
