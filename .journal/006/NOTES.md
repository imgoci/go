---
id: 006
title: Spec conformance audit of the Go implementation
started: 2026-08-16
---

## 2026-08-16 16:17 — Kickoff

Goal for the session: run a systematic conformance review of `~/code/imgoci/spec`
against the Go implementation in this repo. Scope is strictly spec-vs-code
validation, not a general code review. Tests that exist to prove spec conformance
are in scope as oracles: a wrong harness means wrong code. Deliverable is a
compiled report.

Current state of the world: `master` is `0b4be41` ("docs: remove plan references
and over-dense comments (#16)"). Sessions 001–004 are closed; 005 (release-readiness
functional test plan) is still in-progress and separately bound. Implementation
covers plan slices 0–6: root `package imgoci` (parse/list/resolve/capabilities/
fetch/fetchfiles/publish), `internal/{jcs,index,filemanifest,file,decomp,transfer,
multipart,registry,retry,auth}`, private `cli/`, conformance fixtures under
`testdata/conformance` pinned to spec commit `5b95710`, plus canonical fixtures and
a CUE cross-check.

Plan: read the spec and map its normative sections to implementation surfaces,
then fan out up to five `conformance` agents over disjoint spec section groups,
each producing rule-by-rule verdicts with file/line evidence over both code and
the tests that claim to prove it. Reconcile findings into a single report,
deduplicate, and rank by severity.

## 2026-08-16 16:40 — Conformance audit complete

Ran five `conformance` agents over disjoint spec areas (index structure §4/§5;
cross-entry+canonical §6.4-10/§9; discovery/selection §7/§10; retrieval §8/§5.4
compression; producer §3/§9/§10). Compiled report at
`.journal/006/CONFORMANCE_REPORT.md`.

Reconciled severities myself rather than trusting agent labels — four agent
"blockers" were unreachable from the public API or rested on an ambiguous spec
location; two findings escalated after empirical reproduction.

Two real defects:

- F1 (blocker, interop): `internal/decomp` hard-caps xz LZMA2 dictionary and
  zstd window at 8 MiB (`maxDecodeWindow`, zstd.go:39-42). Proved with real
  tooling that `xz -9` (64 MiB dict) and `zstd --long=27` (128 MiB window) are
  rejected with ErrDecode, while `xz -6` and `zstd -19` pass. These are
  mainstream producer settings for OS images, and the spec defines no decoder
  resource profile. The zstd rejection also misreports a window failure as a
  decompressed-size limit. Needs an org decision: add a spec resource clause or
  make the ceiling a knob with a conforming default. Three tests currently
  assert the rejection as required and must change with it.
- F2 (major): §8 requires verifying layer digest AND size; `copyLayer`
  (internal/transfer/fetchfiles.go:465-478) only catches overrun via
  BoundedReader. A compressed layer whose blob is shorter than the declared size
  but matches the declared digest verifies successfully.
  `TestBoundedReaderShortStream` codifies this misreading.

Unreachable/ambiguous (downgraded): `filemanifest.BuildInput.ArtifactType` and
`.Annotations` allow non-conforming manifests but have no non-test caller;
mechanical producer rules (private `x-<owner>-<name>` values, root-only
annotation placement) unenforced; config/layer nested annotations unchecked;
query validation after the index fetch; resolve merges §7.3 step barriers.

Biggest systemic issue is proof rather than behavior: producer byte tests use
the producer's own encoder as the oracle (no independent goldens anywhere), and
three `testdata/canonical/fail` fixtures — `unsorted-keys.json`,
`exponent-1e0.json`, `exponent-1e2.json` — fail earlier than the rule they claim
to test, which `parse_test.go` cannot detect because it asserts only
`ErrInvalidIndex`.

Fixture pin verified: SPEC_COMMIT == spec HEAD 5b95710, 12 pass + 21 fail
byte-identical, fail harness maps every fixture to its specified §6 rule.

Next: user decision on F1, then F2 fix, then the test-oracle repairs.

## 2026-08-16 17:05 — Resolution plan received

Planner produced a nine-slice plan (one PR each), full text in agent output;
key decisions captured here.

F1: one shared `WithDecoderMaxWindow(uint64)` client option, 128 MiB default
(matches zstd CLI ZSTD_WINDOWLOG_LIMIT_DEFAULT=27; covers xz -9's 64 MiB dict),
plumbed Client -> transfer request -> `decomp.Decoder(name, maxWindow)` for both
fetch and publish pass-1. Zero rejected in New. Verified the wiring assumptions:
`Option func(*clientSettings)` with `New(...) (*Client, error)` supports
validation, and `checkProducerRules` (publish.go:302-312) already runs
`index.Build` before any network write, so producer-only rules land there
without duplicating tables in the root package.

F2: BoundedReader gains `ErrSizeMismatch` on early EOF, preserved through the
gzip/xz/zstd wrappers, mapped to public ErrDigestMismatch. Planner confirmed the
BigOCI path is already covered by decodeStored's post-adapter digest+count check.

Two amendments I intend to make before implementation:

1. Architecture registry (slice 3). Planner proposes table-or-`x-` enforcement
   for architecture using a GOARCH list. That risks rejecting a legitimately
   correct future OCI spelling — a false rejection we invent. imgoci owns the
   target/representation/role/compression registries and pins them to
   SPEC_COMMIT; OCI owns architecture and moves independently. Enforce
   table-or-`x-` for the four imgoci-owned fields only; keep architecture
   syntax-only.
2. Golden provenance (slice 4). Plan says "an independent RFC 8785
   implementation" without naming one. Must name the tool and record the exact
   command in testdata README, or the independence claim is unverifiable.

Sequencing note: slices 1 and 3 change public API/behavior and must land before
Release Please PR #9 finalizes 0.1.0.

## 2026-08-16 18:25 — Remediation merged into one PR, CI green

Orchestrated the nine-slice plan across three phases plus a lint phase.
PR #20 (`fix/spec-conformance`, rebased onto e4b0d53), CI pass in 2m19s.

Commits: `0d1f1a1` (F1-F7 behavior + producer rules + oracle repairs),
`a7739e7` (§8 integrity matrix + valid BigOCI fixtures).

Both amendments held: architecture stayed syntax-only (OCI owns that registry),
and the standard-manifest golden was produced with an out-of-repo CPython
canonicalizer whose command and ASCII/integer-subset caveat are recorded in
`internal/filemanifest/testdata/README.md`.

Two corrections I did not anticipate:

1. `xz.ReaderConfig.DictCap` — I told the agent the zero-value 8 MiB
   substitution in `lzma/reader2.go` was a cap that had to be overridden. The
   agent proved out of tree that `lzmafilter.go:70-77` raises DictCap to the
   Block Header's declared capacity whenever that is larger, so it is a FLOOR:
   the library already allocated the declared dictionary, and `inspectXZDictCap`
   was the only real bound. It kept the explicit DictCap (still worth doing) and
   refused to write a test claiming to prove wrong-dictionary bytes. Good catch
   against my instruction.
2. §6 rule 8 descriptor-mediaType disagreement is unreachable: rule 2 already
   forces every descriptor mediaType to identify MediaTypeManifest, and rule 8
   compares with equalMediaType, so anything surviving rule 2 compares equal.
   The agent documented the omission instead of writing a test that would assert
   a false rule.

Operational lessons for next time:

- The `programmer` agent type failed 5/5 at ~4m15s, all after reads and before
  edits. Session 002 recorded the same pattern. Switching to the general-purpose
  `task` type fixed it outright: 13/13 clean. Do not spend another session
  rediscovering this.
- Relative paths in subagent edit calls resolve against the SESSION cwd, not the
  agent's worktree. Three agents leaked edits into the main checkout. In every
  case the worktree copy was the superset and main held a superseded draft, so
  `git restore` was safe, but I had to diff every file to establish that. Always
  give subagents absolute worktree paths and make them check
  `git -C <main> status --porcelain` before yielding.
- moon's test tasks all set `cache: false`, so a missing `inputs` entry (raised
  for the new `testdata/bigoci`) cannot cause a stale-cache miss. No change made.
- Local golangci found 47 issues the per-package agent runs had not seen;
  running the real gate before commit remains non-negotiable.

Open: PR #20 awaits review. It should merge before Release Please PR #9
finalizes 0.1.0, or the release ships the obsolete 8 MiB decode contract and
omits WithDecoderMaxWindow.

## 2026-08-16 18:50 — PR #20 merged

Squash-merged as `9b144ae` after a comment-review pass (`48f4d20`). Master CI,
Release Please, and GitHub Pages all green on the merge commit. Worktree removed
and master fast-forwarded; `wt list` shows only master and the journal.

Comment review removed 111 of the 1062 added comment lines across six parallel
units. The only criterion-1 violations were four change-narrative comments in the
decoder work ("the 8 MiB this package used to hardcode"). The dominant
criterion-2 problem was the query-validation deviation repeated near-verbatim on
Fetch, List, and Resolve; it now reads in full on Fetch with pointers from the
other two. Two comments were factually wrong and fixed: index.Build claimed it
does not re-check Validate (it calls it at build.go:96 before encoding), and a
test doc described a ReleaseSpec constructor as publishing.

Citation style: the branch had introduced three spellings including
`spec.md:550-551` line ranges. Normalized to master's section-only form, which
does not rot when the spec is edited.

Release Please PR #9 (`chore(master): release 0.1.0`) is still open and now
carries this work. Note the version-bump nuance: the PR title was `fix:` but the
change adds public API (`WithDecoderMaxWindow`), which is conventionally `feat:`.
It does not matter here because `initial-version: 0.1.0` pins the first release
regardless, but a later minor-vs-patch decision should not rely on this commit's
type.

Remaining open threads for a future session: the release guard still forbids v1
while the spec is draft (§9.1); the manual coverage gaps recorded in
`.journal/004/SUMMARY.md` (TLS/custom CAs, cross-host redirects, external
credential helpers, Bearer/OAuth, multi-GiB BigOCI payloads, concurrent same-tag
publication) are untouched by this work; and the per-codec ceiling split
(64 MiB xz / 128 MiB zstd) was considered and deliberately not taken.
