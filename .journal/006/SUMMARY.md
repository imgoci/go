---
id: 006
title: Spec conformance audit of the Go implementation
date: 2026-08-16
status: complete
repos_touched: [imgoci/go]
related_sessions: [001, 004]
---

## Goal

Systematically validate the Go implementation against the normative spec at
`~/code/imgoci/spec/spec.md` (draft, commit `5b95710`), treating the tests that
claim to prove conformance as part of the audit surface: a wrong harness means
unproven code. Then resolve everything the audit found.

## Outcome

Goal met. Five read-only conformance agents produced rule-by-rule verdicts over
all 942 spec lines. The consumer validation core was substantively conformant,
but the audit found two real defects, four producer/consumer discipline gaps, and
a systemic proof problem: many rules were implemented correctly yet defended by
tests that would survive deleting the check. All of it is remediated and merged
as `9b144ae` (PR #20, squash), 70 files, +4866/-413, followed by a comment-review
pass. Master CI, Release Please, and Pages are green on the merge commit.

The most consequential finding was reproduced with real tooling before it was
believed: the hardcoded 8 MiB decoder ceiling rejected `xz -9` (64 MiB LZMA2
dictionary) and `zstd --long=27` (128 MiB window on payloads above ~64 MiB) with
`ErrDecode`. Those are the ordinary high-compression settings for OS disk images,
so the shipped client refused mainstream producer output for its own domain.

## Key Decisions

- Raise the decoder ceiling to a 128 MiB default and expose it as
  `WithDecoderMaxWindow` instead of amending the spec -> an implementation may
  set a resource default, but hardcoding one is not a spec matter. 128 MiB is the
  zstd CLI's own decode limit (windowLog 27) and covers `xz -9`'s 64 MiB
  dictionary; one shared value, since a single knob must be the max of the two
  codecs to admit both.
- Enforce only the mechanically checkable half of the producer value rules ->
  registry membership and the `x-<owner>-<name>` private form are decidable;
  "use the public value that matches your intended meaning" is not, because
  intent is not available to a library (spec §5.3 scopes the registry to producer
  conformance anyway).
- Keep architecture syntax-only while enforcing registries for target,
  representation, role, and compression -> imgoci owns those four and pins them
  to `SPEC_COMMIT`, but OCI owns architecture and evolves it independently, so a
  hardcoded list would falsely reject a correct future spelling. This overrode
  the planner's recommendation.
- Put producer-only rules in `internal/index/producer.go` reached from `Build`,
  never in `Validate` -> spec §6:559-561 and §12 require consumers to accept
  producer-only violations. Paired tests assert both halves.
- Treat §7.1's "validate the query before fetching" as a documented deviation
  rather than an API change -> `Fetch` takes only a `Reference`, so honouring the
  ordering would mean a combined fetch-and-query entry point. The cost is one
  wasted round trip on an invalid query, never a wrong result.
- Restructure the §7.3 barriers even though the observable outcome was already
  correct -> the spec mandates the phase ordering, and the rewrite is cheap. Only
  internal error attribution changes; the spec defines no error precedence.
- Verify boundaries by neutering each production check in an out-of-tree copy ->
  several agents proved that only the intended case failed, which is the evidence
  that a test actually defends its rule rather than passing incidentally.

## Changes

- `internal/decomp/` - deleted the hardcoded `maxDecodeWindow`; added
  `DefaultDecoderMaxWindow` (128 MiB) and `Decoder(name, maxWindow)`; xz now
  passes the declared LZMA2 dictionary through to `ReaderConfig.DictCap`; a zstd
  window rejection names the required window instead of surfacing klauspost's
  decompressed-size message; added `ErrSizeMismatch` for a stored file shorter
  than its declared layer size, preserved through every codec wrapper.
- `client.go`, `fetchfiles.go`, `publish.go`, `internal/transfer/` - added
  `WithDecoderMaxWindow` and threaded `DecoderMaxWindow` through both transfer
  requests into the fetch and publish decode paths; mapped `ErrSizeMismatch` to
  public `ErrDigestMismatch`.
- `internal/filemanifest/build.go` - removed the `BuildInput` fields that let a
  caller emit a sixth top-level member or a non-lowercase artifact type, so a
  standard manifest is again a function of layer digest and size alone (§3.1).
- `internal/index/producer.go` (new) - pinned §5.4 registries, the private-value
  form, and the annotation-location table, enforced from `Build`.
- `internal/filemanifest/standard.go` - validates `annotations` as a string map
  on the manifest root, `config`, and `layers[0]`.
- `resolve.go`, `list.go`, `fetch.go` - two-pass §7.3 barriers; the
  query-validation deviation documented on the public surface and in
  `docs/docs/reference/api.md`.
- `docs/docs/reference/errors.md` - rewrote the decode working-set section and
  added stored-file size verification.
- `testdata/canonical/fail/` - repaired `unsorted-keys.json` (duplicate
  `schemaVersion` tripped the duplicate-key scan) and both exponent fixtures (the
  exponent sat in the known `size` member, so integer decoding rejected them
  before rule 10); `parse_test.go` now asserts the failing phase and that every
  earlier phase accepts the fixture.
- `internal/filemanifest/testdata/standard-v1.json`, `internal/index/build_test.go`
  - independent byte goldens for both producers, replacing self-oracle
  round-trips.
- `testdata/bigoci/v1/` (new) - valid two-part and one-part BigOCI v1 artifacts;
  the old unit fixtures lacked `schemaVersion`, the empty config, and
  `io.bigoci.part.size` and mocked `PullTo`, bypassing the validating adapter.
- Roughly 60 previously undefended rules gained assertions across
  `internal/index`, `internal/filemanifest`, `internal/transfer`,
  `internal/registry`, and the root package.

## Open Threads

- Release Please PR #9 (`chore(master): release 0.1.0`) is open and now carries
  this work. The draft-spec guard still applies.
- The PR title was `fix:` but the change adds public API
  (`WithDecoderMaxWindow`), conventionally `feat:`. Inert today because
  `initial-version: 0.1.0` pins the first release, but a later minor-vs-patch
  decision must not lean on this commit's type.
- Per-codec ceilings (64 MiB xz / 128 MiB zstd) were considered and deliberately
  not taken: it would halve worst-case exposure in the xz path only, at the cost
  of a second public concept.
- `index.Build`'s doc previously claimed it does not re-check `Validate`, which it
  does. Corrected here; worth watching for similar drift in comments that
  describe control flow.
- The 128 MiB default is per active decoder, so peak decoder memory is the
  ceiling times the concurrent role count set by `WithWorkers`. Documented, but it
  is a real memory-profile change from 8 MiB.

## References

- Merged remediation: https://github.com/imgoci/go/pull/20 (squash `9b144ae`)
- Full audit report: `.journal/006/CONFORMANCE_REPORT.md`
- Pending release proposal: https://github.com/imgoci/go/pull/9
- Spec pin: `imgoci/spec` `5b957102eeda16498fdcb80a738431b83abd4197`
- Prior manual rehearsal: `.journal/004/SUMMARY.md`

## Lessons

- A conformance audit must include the harness. Two of this session's defects
  were held in place by tests asserting the wrong thing:
  `TestBoundedReaderShortStream` required a short stream to succeed, and the xz
  and zstd tests asserted the 8 MiB rejection as required behavior. Green tests
  were evidence of the misreading, not of conformance.
- Self-oracle tests prove nothing about spec conformance. Producer bytes were
  validated with the same package's encoder and validator, which is exactly how a
  non-conforming `BuildStandard` stayed green. Only an independently produced
  golden catches a mutually consistent producer/consumer mistake.
- Reproduce before believing a spec-vs-code argument. The 8 MiB finding read as
  theoretical hardening until `xz -9` output was actually rejected; that promoted
  it from a style disagreement to the session's top defect.
- Fixtures can fail for the wrong reason and look correct. Three canonical
  fixtures were rejected at an earlier gate than the rule they claimed, and the
  harness could not tell because it asserted only `ErrInvalidIndex`. Assert the
  phase, and assert that earlier phases accept.
- Instructing an agent from a wrong premise is recoverable if the brief demands
  evidence. The xz `DictCap` instruction was wrong (it is a floor, not a cap); the
  agent proved the upstream behavior out of tree, corrected the rationale, kept
  the useful part of the change, and refused to write a test that would have
  claimed to prove something impossible.
- Agents will also correctly refuse a requested test: §6 rule 8's
  descriptor-mediaType branch is unreachable because rule 2 already constrains
  it, so asserting a rule-8 failure there would encode a false rule.
