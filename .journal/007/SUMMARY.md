---
id: 007
title: Absorb the spec deliverable usage selector
date: 2026-08-17
status: complete
repos_touched: [imgoci/go]
related_sessions: [001, 006]
---

## Goal

Bring the Go implementation up to imgoci spec revision `46d18b7`
(`feat(spec): add deliverable usage selector`, spec PR #17), which adds the
optional `io.imgoci.usage` annotation and threads it through the deliverable
key, the consumer validation rules, the canonical order, and both query
operations.

## Outcome

Goal met. Three PRs merged, each green: #21 (`41719ff`) the internal value
layer and §6/§9 rules, #22 (`46a2efb`) the public API and CLI, #23 (`885feee`)
the producer registry, the conformance pin, and the fixtures. `master` is
`885feee` and the spec pin is `46d18b7`. Nothing from the plan is outstanding.

The implementation now matches the spec on every usage requirement, and the
five upstream conformance fixtures run in CI rather than only in a reviewer's
scratch harness, which was the one real weakness of the first PR.

## Key Decisions

- Carry usage as one canonical serialized string rather than a token slice ->
  `validateRule5` keys `map[Selector]int` and the §9 comparator runs per fetched
  release, so both `Selector` structs must stay comparable and allocation-free.
- Make the public type `struct{canonical string}` rather than `type Usage
  string` -> a named string type is comparable but lets a caller write
  `imgoci.Usage("bad,,value")` and bypass the constructor.
- Split query semantics by operation: `ListQuery.Usage` nil means no filter with
  containment matching, `ResolveQuery.Usage` nil and empty both mean the empty
  set with exact equality -> that is what §7.2 and §7.3 require, and it removes
  any need for a "missing usage list" error in Go.
- Put registry membership in `Build` and the `install-offline` requires
  `install` relationship in `Validate` rule 4 -> the first is producer-only and
  §6/§12 require consumers to accept producer-only violations; the second is
  explicitly a consumer rejection in §5.4.
- Ship plan steps 1+2 as one PR -> step 1 alone would have left three exported
  helpers with no callers, which this repo's lint would flag or force a
  suppression for.
- Pull plan step 5 (the CLI) forward into the step 3 PR -> `cli/go.mod` has
  `replace github.com/imgoci/go => ../`, so the CLI is a same-PR caller;
  deferring it would have merged a CLI that could not reach any usage-bearing
  deliverable.
- Keep the CLI TSV empty set as an empty field rather than `-`, `[]`, or
  `<empty>` -> fixed column count, lossless, and no value a basic token could
  collide with.
- Do not apply the `install-offline` relationship to a query -> a filter or an
  exact-set request is not a published set, and rejecting it would prevent
  asking for a deliverable the index itself would be rejected for.

## Changes

- `internal/index/usage.go` (new) - the single parse, canonicalize, relate and
  containment site: `CanonicalizeUsage`, `ValidateUsage`,
  `ValidateUsageRelationship`, `UsageValues`, `UsageContainsAll`.
- `internal/index/{decode,validate,sort,build,producer}.go` - `AnnotationUsage`
  and `Selector.Usage`; rule 3 usage syntax; rule 4 relationship plus per-usage
  required roles and forbidden targets; rules 5, 6 and 7 rekeyed; rule 9 and the
  §9 comparator; `Build` emits the annotation only for a non-empty set; the
  §5.4 public usage registry with `FormatUsage` exported for error text.
- `usage.go` (new, root) - the comparable public `Usage` with `NewUsage`,
  `String`, `Values`, and the unexported query canonicalizer.
- `entry.go`, `index.go`, `list.go`, `resolve.go` - `Selector.Usage`;
  `ListQuery.Usage` containment with the exact set reported on `Deliverable`;
  `ResolveQuery.Usage` exact equality; four-field deliverable key, which also
  cut `List` from 416 to 17 allocs/op at 400 entries.
- `publish.go`, `internal/transfer/publish.go` - usage copied into the built
  index and included in the placeholder identity key and the pass-1 `fileID`.
- `cli/` - repeatable `-usage` on list, resolve and fetch; the usage column
  after representation in both TSV layouts; optional `files[].usage` in the
  publish spec.
- `testdata/conformance/` - pin `46d18b7` and the five upstream fixtures via
  `sync_conformance.sh --pin`; counts 12/21 -> 13/25.
- `testdata/canonical/` - eight hand-authored byte fixtures (2 pass, 6 fail
  covering rules 3, 4, 5 and 9) plus README rows and `canonicalFailPhases`.
- `.github/scripts/cue_crosscheck.sh` - minima 13/15/25.
- `docs/docs/**`, `doc.go`, `README.md` - usage documented for both roles, and
  every stale `5b95710` / 2026-08-11 reference corrected.

## Open Threads

- Release Please PR #9 (`chore(master): release 0.1.0`) is still open. The
  draft-spec guard still applies, and session 005's `SECURITY.md` blocker plus a
  passing `REL-04` rerun are still the conditions for `0.1.0`.
- Session 005 remains `in-progress` with no `SUMMARY.md`.
- Two Dependabot alerts (1 high, 1 moderate) are reported on the default branch
  and were not investigated in this session.
- The spec says a producer must declare every applicable standard usage value
  and that usage values are assertions validation cannot prove. Only syntax,
  the relationship, and registry membership are mechanically enforceable; the
  docs assign truthfulness to the producer.

## References

- Merged: https://github.com/imgoci/go/pull/21, /pull/22, /pull/23
- Update plan: `.journal/007/UPDATE_PLAN.md`
- Spec change: https://github.com/imgoci/spec/pull/17 (`46d18b7`)
- Prior conformance audit: `.journal/006/SUMMARY.md`

## Lessons

- A change to the MEANING of an existing default is not additive, and a plan may
  sequence its callers too late. `ResolveQuery.Usage` nil newly meant "the empty
  set", which silently made `imgoci resolve` and `imgoci fetch` unable to reach
  any usage-bearing deliverable. The full gate stayed green because no CLI test
  published usage. The `replace` directive makes `cli/` a same-PR caller.
- Fencing an agent out of a file can create the defect. "Do not touch
  producer.go" was correct about the §5.4 registry and wrong about the §5.2
  annotation-location table, so `io.imgoci.usage` could be emitted on the
  release-index root. Fence by rule, not by file.
- Mutation testing keeps earning its cost. Of 19 mutants on the public API, one
  survived: the nil-usage resolve test was order-masked because `firstAccepted`
  keys by compression and the last candidate wins, and the fixture put the
  empty-usage entry last, which is exactly what the broken implementation
  returns. A comment in the test claimed the ordering prevented that.
- Give every file an owner during fan-out, including files the integrator
  writes. `usage.go` was built inline as the shared prerequisite and its tests
  were never assigned; the gap only surfaced at integration.
- A doc line that claims agreement with a pinned file is a landmine when the pin
  moves. `doc.go` asserted it matched `SPEC_COMMIT` and went stale inside the
  very PR that bumped it. Grep the old pin value as the last act of a bump.
- Agents asking before widening scope is worth encouraging: the docs agent
  noticed five out-of-scope pages quoting the old pin and stopped rather than
  silently expanding or leaving the repo inconsistent.
