---
id: 009
title: Reduce the root package to a public API facade
date: 2026-08-17
status: complete
repos_touched: [imgoci/go]
related_sessions: [001, 006, 007]
---

## Goal

Shrink the root `package imgoci` so it contains only public-API-facing code.
The owner's reason: a large root package "makes agents lazy about keeping
boundaries". The session started with one concrete ask — move the e2e suite out
of the root — then audited what remained and moved the rest in reviewed slices.

## Outcome

Goal met. Seven PRs, all merged, all CI-green, all reviewer-approved:

| PR | what | root Δ |
|---|---|---|
| #27 `686d4f3` | e2e suite → `internal/e2e` | −3337 |
| #28 `0ed6081` | duplicate spec grammar deleted | −122 |
| #29 `7cc1737` | adapter cache + construction → `internal/adapters`, `internal/auth` | −109 |
| #30 `cb4d5a7` | reference parsing → `internal/ociref` | −55 |
| #31 `873ab4f` | destination mapping → `internal/file`, fault classification → `internal/transfer` | −53 |
| #32 `ea55aff` | producer validation → `internal/index`, `internal/transfer`, `internal/ociref` | −152 |
| #33 `7a3c419` | §7.2/§7.3 query engines → `internal/index` | −450 |

Root package: **44 files / 10050 lines → 19 files / 1794 lines**. `go doc -all .`
on the root package is byte-identical to where the session started: zero public
API change across all seven PRs. `moon run root:check` passes on the merged
`master` (17 tasks, including the Docker-gated e2e suite).

`internal/` went from 10 packages to 13 (`e2e`, `adapters`, `ociref` are new).

## Key Decisions

- Audit before moving, with four parallel read-only agents over the 19 root
  production files -> the audit found 57% of root production lines sat in
  unexported declarations and, more usefully, that root held second
  implementations of six spec rules `internal/index` already owned. That framed
  the whole campaign and gave slice 1 an unarguable case.
- Six slices, one PR each, each paused for user review -> a 1050-line
  behavior-preserving refactor is unreviewable as one diff, and each slice's
  differential evidence would have been diluted.
- Every slice got a differential probe (`master` vs branch, byte-compared
  output) rather than relying on the existing suite -> the suite proves the
  contracts it knows about; a move can change error text, wrap chains, ordering,
  or allocation profile without failing a single test.
- Two different error-wrapping shapes, chosen per case, because internal
  packages cannot import root sentinels: where the sentinel is a SUFFIX, the
  internal package returns a detail-only error whose text is exactly today's
  prefix and the root appends the sentinel with `%w: %w`; where the sentinel is a
  PREFIX and which sentinel varies, the internal package returns a CATEGORY
  (`transfer.Fault`, `*index.CapabilityError`) and the root owns the choice and
  the wording.
- `internal/index` exposes `ValidateProducerFields` and `ValidateProducerRules`
  as two functions rather than one -> the transfer source check runs between them
  in the original order, and fusing them would silently change which error a
  caller sees when a spec breaks several rules at once.
- `internal/ociref` is a new package rather than a home inside
  `internal/registry` -> that package is an I/O adapter bound to one repository
  and parses nothing; a pure grammar there would violate A2. Named `ociref`, not
  `ref`, because `publish.go` has parameters named `ref`.
- Two fake-port publish tests moved OUT of the e2e suite into the default suite
  -> they injected `Client.newAdapter` and never contacted a registry, so the
  Docker gate was costing time for nothing.
- Kept at the root deliberately: `clientSettings` and `Option` (moving them
  would change the public API, since `Option` is `func(*clientSettings)`), the
  nil-receiver and call-shape checks, `ErrSelectionMismatch`'s digest comparison,
  option plumbing, and every DTO mapper.
- `index.Entry`/`EntriesOf` materialize the selector once at parse time ->
  `Descriptor.Selector()` re-derives six annotation-map lookups per call, and
  reading it per query cost Resolve 590%.
- Took the reviewer's arena-backed public-tree mapping in slice 6 -> it refuted
  my "irreducible" claim with measurements (1019 -> 821 allocs/op), and honoring
  the alloc criterion I set beat hand-waving it.

## Changes

- `internal/e2e/` (new, 16 files) — the whole e2e suite, `package e2e` behind the
  `e2e` build tag, importing the library as a consumer does.
- `internal/adapters/` (new) — `Pool`, `NewPool`, `PortsFor`, `Open`, `Config`,
  `Ports`, `Factory`: the per-repository adapter cache and the registry/multipart
  construction that `client.go` used to own.
- `internal/ociref/` (new) — `Parse`, `Parsed`, `ManifestRef`, `RequireTagOnly`:
  the reference grammar, the sha256-only rule, digest-over-tag selection, and the
  tag-only publish contract.
- `internal/index/` — gained `IsMediaType`, `ASCIILower`, `SupportsMediaType`,
  `ValidateProducerFields`, `ValidateProducerRules`, `ProducerInput`,
  `ProducerFile`, `Entry`, `EntriesOf`, `List`/`Listed`/`ListedRole`/
  `Alternative`/`ListQuery`, `Resolve`/`ResolveQuery`/`ValidateResolveQuery`/
  `CapabilityError`, plus `query.go`'s shared §7.1 validators. Lost
  `IsBasicToken` once its only caller moved.
- `internal/transfer/` — gained `Fault`, `Classify`, `CommitFault`,
  `PublishSource`, `ValidatePublishSources`.
- `internal/file/` — gained `Destination`, `NewDir`, `NewFiles`, `RoleFile`,
  `Map`, `ErrUnsetDestination`.
- `internal/auth/` — gained `NewDockerCredentials`.
- Root: `list.go` 375→145, `resolve.go` 364→104, `publish.go` 421→270,
  `client.go` 364→255, `dest.go` 97→54, `capabilities.go` 153→79,
  `reference.go` 88→32, `mediatype.go` 26→11, `usage.go` 92→65; `fetchfiles.go`
  kept its size but shed `internal/file` and `internal/decomp` entirely.
- `publish_ports_test.go` (new, root) — the two relocated fake-port tests.
- `CONTRIBUTING.md` — documents `moon run root:test-e2e` and its Docker
  requirement, which it never mentioned.
- `testdata/bigoci/README.md` — points at `internal/e2e` instead of "the root
  e2e suite".

## Open Threads

- `internal/index` now spans five concerns: decode, §6 validation, canonical
  verification, producer `Build`, and query selection. The slice-6 reviewer and I
  both think the engines want their own `internal/query` package, and the
  coupling is narrow enough to be nearly a file move (only `isBasicToken` and
  `deliverableKey` are unexported among what they need). Deliberately not done:
  it is a boundary re-cut, not a move, and doing it inside a move-only slice
  would have invalidated the differential evidence.
- No benchmarks are committed. Every performance number in this session came
  from throwaway benchmarks under `/tmp`. If the `List`/`Resolve` allocation
  profile matters, it needs a committed benchmark CI can see.
- `internal/adapters`, `internal/ociref`, and the two `internal/index` query
  files have no package-local tests; coverage is attribution-only lost, since the
  root tests still exercise every rule through the public API. A package-local
  test for `Pool`'s "mutex held across construction" invariant is the one genuine
  gap (it never had a direct test in either tree).
- Cosmetic leftovers the move-only contract forbade touching:
  `TestDefaultAdapterWiresMultipart` and `TestParsedRefManifestRefPrefersDigest`
  still name symbols that have moved.
- Release Please PR #9 (`release 0.1.0`) is still open under the draft-spec
  guard, unchanged by this session. The two Dependabot alerts noted in session
  007 were not investigated.

## References

- Merged: https://github.com/imgoci/go/pull/27, /28, /29, /30, /31, /32, /33
- Audit reports: `agent://AuditPublishIndex`, `agent://AuditQueryPaths`,
  `agent://AuditClientWiring`, `agent://AuditFetchPaths` (transcripts under
  `history://`)
- Prior sessions: `.journal/007/SUMMARY.md` (usage selector),
  `.journal/006/SUMMARY.md` (conformance audit)

## Lessons

- A pure-move refactor fails in the documentation, not the logic. Four of six
  slices were reviewed with exactly one real finding, and every time it was a doc
  comment describing a boundary that had moved: an orphaned antecedent, a link
  paraphrased into a tautology, a package doc contradicted by a symbol just added
  to it, an unresolvable `[ManifestRef]` that needed `[Parsed.ManifestRef]`.
  Standing rule: when a moved comment loses a link, name a symbol that IS
  reachable, and re-read the destination package's `doc.go`.
- Benchmark a representation change before believing it is free. Swapping
  materialized selectors for on-demand annotation-map derivation was invisible to
  every test and cost Resolve 590%. No benchmark existed in the repo to catch it.
- Existing tests arbitrate invariants a contract flattens. My first
  `internal/adapters` contract snapshotted the client config at `New`; two tests
  failed because the old code read `c.settings` on every construction. The tests
  knew the invariant I had missed.
- When a reviewer says a performance claim is wrong, ask for numbers — this one
  brought them, having implemented the alternative and measured it.
- Prove equivalence BEFORE writing the brief when a slice swaps two
  implementations of the same rule. A 371k-input differential test made slice 1's
  swap a premise rather than a hope, and let the brief say "do not re-derive
  this".
- Type-aware beats regex for cross-package moves: a throwaway
  `x/tools/go/packages` pass rewrote 174 identifiers by resolving each to its
  object, skipping `SelectorExpr.Sel` and composite-literal keys. A text rewrite
  would have corrupted keys like `Selector:` inside `index.ModelEntry`.
- Lint config can force a behavior change. `errorlint` rejects a non-wrapping
  verb for an error, so keeping message text identical while splitting detail
  from sentinel necessarily turns single-wrap errors into two-element multi-wrap
  ones. Verified no consumer walks `errors.Unwrap` on those paths.
- Subagents resolve relative edit paths against the session cwd, not their
  worktree. It happened in three of six slices despite explicit absolute-path
  instructions. A scheduled mid-flight `git -C <main> status --porcelain` check
  caught each one; that check is now mandatory after every fan-out.
