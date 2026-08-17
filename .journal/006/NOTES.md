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
