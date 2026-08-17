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

## 2026-08-16 21:55 — Step 1 merged-ready: PR #21
Worktree `.wt/feat-usage-value-layer` on `feat/usage-value-layer`, based on
`origin/master` `140d3f4` (created with `wt switch --create`, then
`git reset --hard origin/master` per the stale-tip note). Commit `a9dfe11`,
PR https://github.com/imgoci/go/pull/21, CI green (ci, CodeQL, Analyze go +
actions, Pages). Local `moon run root:check`: 17 tasks green.

Scope landed: plan steps 1 AND 2 together, not step 1 alone. Step 1 in isolation
would have left `CanonicalizeUsage`/`ValidateUsage`/`ValidateUsageRelationship`
with zero callers, and this repo's golangci config would either flag the dead
exported helpers or force a suppression. Wiring rules 3-7 and the §9 comparator in
the same PR keeps every symbol consumed and every rule tested.

Agent shape: 1 `programmer` (implementation, 18m) -> 3 read-only reviewers in
parallel (`reviewer`, `conformance`, `qa`) -> 1 `programmer` (test gaps, 6m). The
`programmer` type did NOT crash this time, contradicting the 0/5 record from
sessions 002 and 006; two clean runs at 18m and 6m. Worth updating TECH_NOTES at
close: the guidance should be "prefer `task`, but `programmer` is no longer
known-broken", and the explicit absolute-worktree-path instruction did its job
(both agents edited only the target worktree; main checkout stayed clean).

Review value, in order of usefulness:
- `reviewer` found a real blocker I had caused with my own brief: I told it not to
  touch `producer.go`, but making `io.imgoci.usage` a DEFINED annotation means
  §5.2 placement applies, so it had to join `isDescriptorOnlyAnnotation` or a
  producer could emit it on the release-index root. Reproduced by the agent from
  the emitted bytes. Fixed, with a paired test in `TestProducerOnlyViolations`
  asserting Build rejects while Validate accepts. Mutation-checked: removing the
  one line turns the test red.
- `reviewer` also caught `usageTokenAt` reimplementing `strings.SplitSeq` (R3),
  with benchmarks showing no perf cost (12.5 vs 12.2 ns/op, 0 allocs both).
  Deleted ~19 lines of sentinel-offset cleverness.
- `conformance` independently ran the five new upstream fixtures against the
  branch through a /tmp harness with a replace directive: pass accepted, three
  syntax fails at rule 3, `install-offline-without-install` at rule 4. That is the
  only independent oracle on this PR, since the SPEC_COMMIT pin is a later step.
- `qa` ran 29 mutants; 23 defended, 6 survived. All six were real gaps: Decode
  dropping the annotation (the projection test built a Descriptor literal and
  never called Decode), 129-byte tokens through either helper, the exact 4096-byte
  canonicalization bound, trailing comma and unknown/private acceptance end to end
  through Validate, and `incus-vm` roles split across usage sets. Closed and each
  mutant re-run red.

Lesson to promote at close: a brief that fences an agent out of a file can create
the defect. "Do not touch producer.go" was right about the §5.4 registry and wrong
about the §5.2 annotation-location table; the fence should name the RULE to skip,
not the FILE.

Also fixed by me during integration: 9 golangci findings the agent's targeted
verification never saw (3x perfsprint fmt.Errorf -> errors.New, nonamedreturns,
golines/goimports, modernize mapsloop) and a `<empty>`/comma ambiguity in
`formatUsage` error text, now `usage=<empty>` / `usage="install,live"`.

Next: step 3 of `UPDATE_PLAN.md` (public `Usage` domain type, `ListQuery`
containment, `ResolveQuery` exact-set equality, `Deliverable.Usage`), still
gated behind PR #21 review.

## 2026-08-17 08:35 — Step 1 merged; step 3 shipped as PR #22
PR #21 squash-merged as `41719ff`. Worktree removed. Step 3 lives on
`feat/usage-public-api` in `.wt/feat-usage-public-api`, commit `373b6e9`,
PR https://github.com/imgoci/go/pull/22. All checks green after three GitHub
codeload outages (429/502/503 downloading `mise-action`, `configure-pages`,
`codeql-action`) forced two reruns and one empty retrigger commit; none of the
failures were ours. Local `moon run root:check`: 17 tasks green.

Scope: plan step 3 PLUS step 5 (the CLI) and the docs, 34 files. Pulling the CLI
forward was not gold-plating; see the blocker below.

THE BLOCKER, and the real lesson of this session: `cli/go.mod` carries
`replace github.com/imgoci/go => ../`, so the private CLI compiles against the
working tree. Step 3 gave `ResolveQuery.Usage` the meaning "nil = the empty set,
matched exactly". `cli/query.go` never sets `Usage` and had no `-usage` flag, so
`imgoci resolve` and `imgoci fetch` could no longer reach ANY deliverable
carrying `io.imgoci.usage`, and `imgoci list` printed byte-identical rows for
deliverables that differ only in usage. `root:check` was GREEN through all of it,
because no CLI test publishes a usage-bearing release. The `reviewer` agent found
it by running one fixture against `origin/master` (`err=<nil>`) and against the
branch (`no deliverable ... usage=<empty>`).

Generalization worth promoting to TECH_NOTES: a semantic change to the DEFAULT of
a public field is not additive, and a plan that sequences callers into a later
step is only safe for genuinely additive changes. The replace directive makes
`cli/` a same-PR caller, not a downstream consumer.

Agent shape: 3 parallel `programmer` agents (list / resolve / publish) after I
built the shared prerequisite inline (`usage.go`, `Selector.Usage`, the internal
`UsageValues`/`UsageContainsAll`), then 2 parallel reviewers (`reviewer`, `qa`),
then 3 more parallel agents (`CliUsage`, `LibraryGaps`, `UsageDocs`). Nine agents
total this step, all clean; `programmer` is now 5/5 in this session against 0/5
in sessions 002 and 006.

Two things the parallel split cost me, both worth remembering:
- `PublicResolve` wrote its first edits into the MAIN checkout despite an
  absolute-path brief. `PublicList` noticed and reported it; I messaged the agent
  and it moved the work and restored the cwd. Sibling agents cross-checking each
  other's blast radius is a real benefit of fanning out.
- Nobody owned `usage_test.go` for the public type: I wrote `usage.go` inline as
  the prerequisite and never assigned its tests. Caught it at integration and
  wrote them myself, including `TestParseIndexCarriesUsage`, which feeds
  hand-written canonical bytes through `ParseIndex` (so rule 10 vets the fixture
  too). Fan-out needs an explicit owner for anything the integrator writes.

`qa` found one surviving mutant out of 19: nil-usage resolve was order-masked
because `firstAccepted` keys by compression and the LAST candidate wins, and the
fixture put the empty-usage entry last, which is exactly what a nil=match-any
implementation returns. A comment in the test even claimed the ordering prevented
that. Now covered by a non-empty-only index plus an order-sensitive positive case.
`reviewer` separately found that the `usage=<empty>` error marker survived
deletion.

Integration fixes I made: `publish_test.go`'s new `body(ref)` helper tripped
`unparam` (all three callers passed "v1") -> parameterless with a
`publishTestTag` constant; testifylint rejected `assert.True(t, a == b)` in the
comparability test, so the comparison is assigned to a variable first, since
`assert.Equal` would prove reflection equality rather than `==`.

Ops note: `moon run root:lint` reported findings in `../feat-usage-value-layer`
for two runs after that worktree was removed. It was a stale golangci-lint cache;
`golangci-lint cache clean` fixed it. Worth checking before believing lint output
that names a deleted worktree.

Next: the remaining plan steps collapse to one PR — the §5.4 producer usage
registry, the `SPEC_COMMIT` bump to `46d18b7`, the five vendored upstream
fixtures with counts 13/25, the repo-owned `testdata/canonical` usage fixtures,
and `cue_crosscheck.sh` minima.

## 2026-08-17 12:20 — Step 3 merged; final step shipped as PR #23
PR #22 squash-merged as `46a2efb`. Final step on `feat/usage-conformance-pin` in
`.wt/feat-usage-conformance-pin`, commit `97c9c6e` plus one empty retrigger
commit, PR https://github.com/imgoci/go/pull/23. All checks green. Local
`moon run root:check`: 17 tasks green.

Content: the §5.4 producer usage registry in `producer.go` reached only from
`Build`; `SPEC_COMMIT` at `46d18b74cc407ac7d61ded7692fc42b644f4d1e2` with the five
vendored upstream fixtures (counts 13/25); `pinnedUsages()` added to the registry
review gate; eight hand-written `testdata/canonical` fixtures (2 pass, 6 fail
covering rules 3, 4, 5, 9); `cue_crosscheck.sh` minima 13/15/25; docs.

The implementation is now caught up with spec `46d18b7`. The independent upstream
oracle runs in CI instead of only in a reviewer's scratch harness, which was the
one real weakness of PR #21.

Agent shape: 4 parallel agents off a base I prepared inline by running
`sync_conformance.sh --pin` first, so all four saw a consistent fixture set.
`RegistryDocs` asked before widening scope: it noticed that `index.md`,
`explanation/architecture.md`, `capabilities.md` and two how-tos still quoted the
old pin `5b95710` / 2026-08-11, outside its assigned files. Authorized the
docs-wide pin bump; that is a question worth an agent asking rather than either
silently expanding or silently leaving the repo inconsistent.

Verified evidence rather than claims: each of the six new canonical fail fixtures
was reported with its observed error, and every one names the rule it is named
for (3/3/3/4/5/9). Registry mutation kills
`TestProducerOnlyViolations/{bare_unknown_usage,unknown_token_after_public_usage}`;
deleting a vendored fixture fails the count assertion; a wrong rule mapping fails
its subtest with a rule mismatch.

Ops findings worth keeping:
- The stale golangci-lint cache bit again, this time naming the removed
  `feat-usage-public-api` worktree. `golangci-lint cache clean` after every
  `wt remove` is the cheap habit.
- `cue` on PATH is 0.16.1 and REFUSES the spec module (language `v0.17.1`),
  rejecting even valid fixtures with a closed-field error. `mise exec` supplies
  0.17.1 and the cross-check exits 0. Anyone debugging a CUE "failure" locally
  should check which binary they got before believing it.
- GitHub had a multi-hour GraphQL/codeload outage during this step: 503s on
  `gh pr create` (GraphQL AND REST), 429/502/503 on codeload for `mise-action`,
  `configure-pages` and `codeql-action`, and a CodeQL job that failed on
  "determine feature enablement". Runs that fail in `Set up job` on a codeload
  download are never ours. Some runs report `cannot be rerun`; an empty commit is
  the reliable retrigger.

## 2026-08-17 13:05 — Completeness audit found two stragglers
Asked whether #23 was the last step, I audited rather than answered from memory,
and plan step 7 ("documentation and stale source language") was NOT fully done.
Two files still claimed the old pin:
- `doc.go:5-7` said "imgoci v1 draft, 2026-08-11 (commit 5b957102..., the same pin
  recorded in testdata/conformance/SPEC_COMMIT)". That sentence asserts agreement
  with a file THIS PR changes, so #23 would have shipped a self-contradicting
  package doc.
- `README.md:7` tracked "imgoci spec v1 draft (2026-08-11)".
Both corrected in `1164ed4`. A repo-wide grep for `5b957102` and `2026-08-11`
outside `.git`, `docs/build` and `docs/.venv` is now empty.

The one other hit, `validate_test.go:469` "one of the three fields", is correct:
it means content digest, size and filename, not a selector tuple width.

Lesson: a doc line that claims consistency with a pinned file is a landmine when
the pin moves. Grep for the OLD pin value as the last act of any pin bump; the
seven-step plan listed the docs pages by name but not this cross-reference.

Usage work is now complete: PR #21 (value layer), #22 (public API and CLI), #23
(producer registry, pin, fixtures, docs), each green.

Next: at close, promote to TECH_NOTES: the usage design (canonical string,
comparable Selector, producer/consumer split), the `cli/` replace-directive trap,
the `programmer`-agent rehabilitation (9/9 this session, against 0/5 in sessions
002 and 006), the stale golangci-lint cache after `wt remove`, the PATH-vs-mise
`cue` version trap, and the old-pin grep habit above.

## 2026-08-17 14:10 — Close
PR #23 squash-merged as `885feee`. All three usage PRs are in:
- #21 `41719ff` — internal value layer, §6 rules 3-7, §9 order
- #22 `46a2efb` — public `Usage`, list containment, resolve exact equality,
  publish/transfer plumbing, CLI `-usage` and TSV column, docs
- #23 `885feee` — §5.4 producer registry, `SPEC_COMMIT` `46d18b7`, five upstream
  fixtures (13/25), eight repo-owned canonical fixtures, cue minima, docs

Handoff state: `master` at `885feee`, local default fast-forwarded, all three
implementation worktrees removed, only `journal/jmgilman` remains under `.wt/`.
The implementation matches spec `46d18b7` on every usage requirement and the
upstream oracle runs in CI.

`TECH_NOTES.md` updated with the usage design, the producer/consumer asymmetry,
the `cli/` replace-directive trap, the corrected `programmer`-agent record, the
fence-by-rule fan-out lesson, and the golangci-cache / `cue`-version / old-pin
traps. `SUMMARY.md` written; `INDEX.md` row 007 set to complete.

Carried forward, untouched by this session: Release Please PR #9 and the `0.1.0`
guard, session 005 still `in-progress` without a summary, and two Dependabot
alerts (1 high, 1 moderate) on the default branch.
