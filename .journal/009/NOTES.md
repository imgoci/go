---
id: 009
title: New session
started: 2026-08-17
---

## 2026-08-17 14:54 — Kickoff
Goal for the session: not yet stated. The user asked to start a new session and
has not given a task; capture the goal here as soon as it arrives.

Current state of the world:
- `master` is `885feee` (PR #23, the producer usage registry plus the bumped
  conformance pin). Main checkout is clean but 3 behind `origin/master`.
- All PLAN slices 0-6, the manual-release follow-up, the spec-conformance
  remediation, and the full `io.imgoci.usage` selector are merged. Nothing from
  the implementation plan is outstanding.
- Spec pin is `46d18b74cc407ac7d61ded7692fc42b644f4d1e2` (draft, 2026-08-16).
- Open threads carried in from session 007: Release Please PR #9
  (`chore(master): release 0.1.0`) is still open under the draft-spec guard, and
  still needs session 005's `SECURITY.md` fix plus a passing `REL-04` rerun; two
  Dependabot alerts (1 high, 1 moderate) on the default branch are
  uninvestigated; session 005 remains `in-progress` with no `SUMMARY.md`.
- Journal worktree `.wt/journal-jmgilman` is clean at `97679cb`, in sync with
  `origin/journal/jmgilman`.

Plan: wait for the user's actual request, then scope it, branch with
`wt switch --create --base master` (followed by
`git reset --hard origin/master`), and integrate through a GitHub PR with a
squash merge.

## 2026-08-17 15:22 — Goal set: shrink the root package, starting with e2e
Goal: focused refactor to reduce the root package surface. First step is moving
the e2e suite to `internal/e2e`. The user's stated reason for keeping the root
package small: a large root package makes agents lazy about boundaries, so any
forced helper refactor is a feature, not a cost.

Survey facts (measured, `master` at `8d02009` after ff from `885feee`):
- 44 `.go` files in the root package: 2759 prod lines, 3754 non-e2e test lines,
  3537 e2e lines across 12 `//go:build e2e` files, all `package imgoci`.
- Trial move into `/tmp/e2e-trial` (`internal/e2e`, `package e2e`, dot-import of
  the root package as a stand-in for qualification): compiles green after
  removing one unused dot-import. Only 4 unexported symbols were missing, all in
  `e2e_publish_test.go`: `clientWithTransferPorts`, `validReleaseSpec`,
  `gzipTwoMemberSpec` (publish_test.go helpers) and `toIndexSelector`
  (publish.go).
- Deleting `e2e_publish_test.go` from the trial left the other 11 files compiling
  clean, which proves they are public-API-only.
- `clientWithTransferPorts` injects `Client.newAdapter` with `clientSettings` /
  `adapterPorts`, so two tests in `e2e_publish_test.go` (TwoMemberGzip,
  IndexPutFailureLeavesNoTag) are white-box fake-port tests that never touch a
  registry. They are mislabeled as e2e.
- `moon.yml` `test-e2e` is `go test -race -tags e2e ./...`, so a new package is
  picked up with no task change. golangci-lint sets no build tags, so e2e files
  are unlinted today and stay unlinted after the move.
- A directory whose only files are all tag-excluded is silently skipped by
  `go build ./...` / `go vet ./...` (verified in the trial).
- One relative path to fix: `bigOCIFixtureDir = "testdata/bigoci/v1"`.
- `fixtures_e2e_test.go` is 1006 lines, already over the AGENTS R2 1000-line cap.
- No `.md`, `.yml`, or `.sh` outside `.wt/` references any `TestE2E*` name (34
  funcs), so renaming is free. `cli/registry_test.go` is the cli module's own
  e2e suite and is out of scope.

Next: propose the migration plan, then implement on approval.

## 2026-08-17 16:05 — e2e migration implemented, PR #27 open
Branch `refactor/move-e2e-to-internal-package`, two commits, PR
https://github.com/imgoci/go/pull/27.

What landed:
- `57b4ac1` moved the two fake-port publish tests (`newAdapter` injection, no
  registry) into root `publish_ports_test.go` UNTAGGED, so they now run in the
  default suite. `e2e_publish_test.go` kept the 3 public-API tests plus local
  `simpleReleaseSpec` and `indexSelectorOf` helpers.
- `30d70d5` moved all 12 e2e files to `internal/e2e` as `package e2e`, split the
  1006-line fixture file into registry/release/client fixture files, split the
  external bigoci CLI helpers into `fixture_bigocicli_test.go`, added `doc.go`,
  stripped the `E2E` infix from 32 test names, fixed `bigOCIFixtureDir` to
  `../../testdata/bigoci/v1`, and documented `root:test-e2e` in CONTRIBUTING.
- Zero new exports. Root package: 44 files/6296 lines -> 34 files/2977 lines.

Method worth reusing: qualification was done type-aware, not by regex. Move the
files with a dot-import of the root package (compiles unchanged, and any missing
symbol is exactly an unexported dependency), then a throwaway
`x/tools/go/packages` program rewrites every ident whose `TypesInfo.Uses` object
belongs to the root package into `imgoci.X`, skipping `SelectorExpr.Sel` and
composite-literal keys, then swap the dot-import for the repo's
`imgoci "github.com/imgoci/go"` alias. 174 insertions across 11 files, zero
manual qualification errors. A naive text rewrite would have corrupted
composite-literal keys such as `Selector:` inside `index.ModelEntry`.

Verification: `go test -race -tags e2e ./...` green; all 32 e2e tests pass with
no skips, including `TestBigOCICLIInterop`; count parity with master (34 -> 32 +
2 relocated). golangci-lint run and fmt --diff clean. cli module built and
tested. CI on #27 was still running at this checkpoint.

Next: confirm CI green on #27, then squash-merge. Follow-up slice the user
flagged: the remaining ~3754 lines of root unit tests.

## 2026-08-17 16:20 — PR #27 merged; one number corrected
Squash-merged as `686d4f3`; CI green on every gate (`ci` = `root:check`, which
includes `root:test-e2e`, plus CodeQL and Pages). `master` fast-forwarded and the
implementation worktree removed. `internal/e2e/` holds 16 files; `go test -race .`
on the root package passes on the merged tree.

Correction to the earlier PR body: I wrote that the root package dropped to
"34 files / 2977 lines". That was wrong — 2977 corresponded to nothing measured.
Accurate: 44 files / 10050 lines -> 33 files / 6712 lines (2759 production, 3953
test, the increase being the 199-line relocated `publish_ports_test.go`). The PR
body on GitHub has been corrected in place with the error called out.

Durable context worth promoting at close:
- e2e suite now lives in `internal/e2e` (`package e2e`, `//go:build e2e`,
  testcontainers/Docker). It imports the library as a consumer, so a test needing
  an unexported seam belongs in the root package instead. `bigOCIFixtureDir` is
  relative to the package dir.
- For any future cross-package test move: do the qualification type-aware via a
  dot-import + `x/tools/go/packages` rewrite (skip `SelectorExpr.Sel` and
  composite-literal keys), never a text regex.
- `moon run root:test-e2e` needs no change when adding packages; it is
  `go test -race -tags e2e ./...`. golangci-lint sets no build tags, so
  e2e-tagged files are still unlinted — gofmt and compile are the only automatic
  gates on them.

## 2026-08-17 17:05 — Root package boundary audit (4 parallel agents)
Fanned out 4 read-only `conformance` agents over the 19 root production files
(2759 lines) with a shared verdict vocabulary (public-api / thin-wiring /
borderline / extract / extract-heavy-body) and a hard constraint: no proposal may
change the public API. Reports: agent://AuditPublishIndex, agent://AuditQueryPaths,
agent://AuditClientWiring, agent://AuditFetchPaths.

My own independent measurement before reading them: 1560 of 2759 root production
lines (57%) sit in unexported declarations. Worst offenders: publish.go 327/421,
resolve.go 251/364, list.go 250/375, fetchfiles.go 188/260, client.go 150/364,
capabilities.go 99/153.

Verdict: the root package is NOT a public-API facade. ~1050 lines are flagged for
extraction. The strongest single finding, which I verified myself rather than
taking on faith, is that root carries SECOND IMPLEMENTATIONS of rules
`internal/index` already owns (~190 lines):
- `EqualMediaType` + `asciiFold` (root mediatype.go) vs internal/index/validate.go:421,442
- basic-token grammar `validBasicToken`/`isBasicTokenAlnum`/`isBasicTokenSep`/
  `validateArchitecture`/`maxBasicTokenBytes` (root list.go) vs
  internal/index/validate.go:463,491 and decode.go:57
- RFC 6838 restricted-name chain + `asciiToLower` (root capabilities.go) vs
  internal/index/validate.go:585-607
- `requireUTF8` and the UTF-8 walk (root publish.go) vs internal/index/build.go:242
- `deliverableKey` (root list.go) vs internal/index/validate.go:366
- `annotationName`/`annotationVersion` (root index.go) vs internal/index/decode.go:32-34
That duplication is exactly the boundary erosion the user predicted.

Clean, no findings: entry.go, parse.go, errors.go, doc.go, fetch.go, progress.go,
release.go, source.go. Near-clean: index.go (2 duplicate constants), usage.go (2
query helpers).

Three places I overrode an agent's recommendation:
1. Rejected a new `internal/clientconfig` package for `New`'s body: one default
   plus one zero-check is not a package. Keep `New` at root; only the Docker
   credential discovery I/O moves (to internal/auth).
2. `internal/ociref` for reference parsing: agree it must leave root, but prefer a
   home in `internal/registry` (already owns distribution concerns) unless that
   creates a cycle. Open decision, cheap to settle at implementation time.
3. `mapFetchError`/`mapPublishError`: keep the internal-category -> public-sentinel
   wrapping at root (an internal package cannot import root sentinels); move only
   the deep chain knowledge of file/decomp/transfer errors.

Also found: D1 gap at publish.go:70 (`PublishOption.applyPublish` has no doc
comment).

Biggest risk noted for the list/resolve slice: `Index` currently stores public
`FileEntry` values, so moving the §7.2/§7.3 engines into `internal/index` implies
`Index` retains internal descriptors instead — a private representation change
that root tests are coupled to (`list_test.go:37` builds `Index.entries` directly,
`fetchfiles_test.go:26` builds `Resolved.entries`, `resolve_test.go:226,357` test
the private validator). Sequence that slice last.

## 2026-08-17 18:05 — Slice 1 implemented, reviewed, PR #28 open (awaiting user)
Orchestration pattern for the slice campaign, working as intended: two
`programmer` agents with disjoint file ownership and a contract I fixed up front
(exact export names/signatures), then one `reviewer` agent, then the PR.

Slice 1 = delete root reimplementations of internal/index spec grammar.
- internal/index/validate.go: +3 exported wrappers (`IsBasicToken`, `IsMediaType`,
  `ASCIILower` — the last is the verbatim moved body of root `asciiToLower`).
- Deleted 13 root declarations across list.go, capabilities.go, mediatype.go,
  index.go; 4 call expressions rerouted. Root production -122 lines,
  internal +25.
- Deliberately deferred: `validateArchitecture` keeps composing
  `validateBasicToken` (its 3 distinct error messages cannot come from a boolean
  `IsArchitecture`); publish.go `requireUTF8`, root `deliverableKey`, and
  `supportsType` move with their callers in slices 5/6.

Pre-flight evidence I generated myself before writing the agent briefs: a
differential test over 371,165 inputs proving root `validBasicToken` ≡
`isBasicToken`, `validRFC6838TypeSubtype` ≡ `isMediaType`, `validRestrictedName`
≡ `isRestrictedName`. Reviewer independently swept 1,780,132 inputs (0
mismatches), diffed `go doc -all` before/after, ran a master-vs-branch probe
program (130/130 output lines byte-identical), and measured allocs to confirm
`EqualMediaType` still allocates nothing. Verdict: approve, zero findings.
One subtlety settled by review: the deleted root `sub == ""` guard in
`validRFC6838TypeSubtype` was redundant because `isRestrictedName` rejects the
empty string on its length check.

Full gates green locally (build, vet ±e2e tag, race tests, golangci-lint run and
fmt --diff, cli module). PR: https://github.com/imgoci/go/pull/28. Paused for the
user's review per instruction; slices 2-6 follow on approval.

OPERATIONAL LESSON (again, worse this time): BOTH programmer agents resolved
relative edit paths against the session cwd instead of their assigned worktree.
`RootCapsAndDupes` caught and reverted it itself; `IndexExportsAndList` did not
notice until I checked `git -C <main> status --porcelain` mid-flight and steered
it via `hub send`. Next brief MUST say: use absolute paths for every edit, and
verify the main checkout is clean before yielding. Checking the main checkout
after each fan-out is now mandatory for me.

## 2026-08-17 19:05 — Slice 1 merged; slice 2 implemented, reviewed, PR #29 open
Slice 1 merged as `0ed6081` (#28) after the user's LGTM.

Slice 2 = adapter cache and construction out of client.go.
- New `internal/adapters` (doc.go, pool.go, open.go): `Config`, `Ports`,
  `Factory`, unexported `adapterKey`, `Pool`, `NewPool`, `PortsFor`, `Open`
  (moved `defaultAdapter`), unexported `multipartConfig` (moved).
- `internal/auth.NewDockerCredentials` = moved root `dockerCredentials`.
- client.go 364 -> 255 lines; no longer imports sync, internal/multipart,
  internal/registry, internal/transfer. Root keeps `clientSettings`/`Option`
  (public `Option` is `func(*clientSettings)`), one `adapterConfig` mapper, and
  the `auth.ErrAuth` -> `ErrUnauthorized` classification (internal cannot import
  root sentinels). Root `adapterPorts` DTO deleted; 3 call sites read
  `adapters.Ports` fields.
- PR: https://github.com/imgoci/go/pull/29, CI green, reviewer approve with one
  minor doc nit that I fixed before commit.

DESIGN BUG I INTRODUCED AND HAD TO CORRECT (worth remembering): my first contract
had `Pool` hold a `Config` snapshot taken at `New`. Two existing tests
(`TestFetchUnsupportedStoredTokenIsUnauthorized`,
`TestFetchCancelledContextReachesCredentialResolution`) failed because they
assign `client.settings.resolved` AFTER `New`, and master's `portsFor` passed
`c.settings` into the factory on EVERY construction. Config is call-time state,
not construction-time state. Fixed by `NewPool(factory)` +
`PortsFor(ctx, host, repo, cfg)`. Lesson: when writing a move contract, check
whether the old code read shared state per call or once, and let the existing
tests arbitrate — they encoded the invariant I had missed.

Reviewer method worth reusing: it extracted master's `defaultAdapter` /
`multipartConfig`, applied the authorized renames with sed, and diffed against
the new file to prove verbatim; then ran an 8-fact probe under -race in two
extracted trees (master vs branch) and got byte-identical output, including
`maxInFlight=1` with 16 concurrent callers to prove the mutex is still held
across construction.

Also confirmed: `internal/adapters` has no test file, but no coverage was lost —
only attribution. There is no per-package coverage gate in CI.

PATH BUG, THIRD OCCURRENCE: `RootClientRewire` edited all eight of its files in
the MAIN checkout despite an explicit absolute-path instruction in the brief.
Caught by my scheduled mid-flight `git -C <main> status --porcelain` check ~2
minutes in, steered via `hub send`, agent recovered by copying content across and
restoring the main checkout. The mid-flight check is now non-negotiable for every
fan-out.

## 2026-08-17 20:00 — Slice 2 merged; slice 3 implemented, reviewed, PR #30 open
Slice 2 merged as `7cc1737` (#29).

Slice 3 = reference parsing out of root.
- New pure `internal/ociref` (doc.go, ref.go): `Parsed` (exported fields),
  `Parse(raw string)`, `(Parsed).ManifestRef()`.
- reference.go 88 -> 33 lines: public `Reference` + unchanged doc comment + a
  one-line delegate `func (r Reference) parse() (ociref.Parsed, error)`. Keeping
  the delegate meant fetch.go/publish.go/reference_test.go only needed field and
  type renames instead of call-site rewrites.
- PR https://github.com/imgoci/go/pull/30, CI green.

Two decisions recorded in the PR: a NEW package rather than `internal/registry`
(that package is an I/O adapter bound to one repository and parses nothing, so
hosting a pure grammar there breaks A2); and the name `ociref` rather than `ref`
because publish.go has parameters named `ref` that would shadow it.

Only genuine risk in this slice was the `%q` operand changing static type from
`Reference` to `string`. Settled empirically by the reviewer: probe run against a
`git archive` of master and against the branch produced byte-identical error text
(same md5) for three malformed references and one sha512 digest.

Reviewer found one real defect I would have missed: `[ManifestRef]` in the new
package comment does not resolve as a doc link, because doc links address
package-level symbols and ManifestRef is a method on Parsed. It proved this by
walking the `go/doc` comment tree, then I fixed it to `[Parsed.ManifestRef]`.
Worth remembering for every new package comment in the remaining slices.

Two process notes: (1) single programmer agent this time (44 lines) instead of
fanning out — fan-out would have been padding; the mid-flight main-checkout check
came back clean, first slice where no agent leaked edits. (2) `gofmt -l` caught a
trailing blank line the agent left in reference.go, and the stale golangci-lint
cache again reported findings from the deleted slice-2 worktree until
`golangci-lint cache clean`. Both are now standing steps in my integration
routine.

Remaining: slice 4 (fetchfiles preflight + dest mapping + error classification),
slice 5 (publish producer validation), slice 6 (list/resolve engines).

## 2026-08-17 21:15 — Slice 3 merged; slice 4 implemented, reviewed, PR #31 open
Slice 3 merged as `cb4d5a7` (#30).

Slice 4 = three moves out of root:
- `internal/file/dest.go` (new): `Destination`, `NewDir`, `NewFiles`, `RoleFile`,
  `Map`, `ErrUnsetDestination` — the moved `Dest.mapByRole` + `mapToFiles`.
- `internal/transfer/classify.go` (new): `Fault` + constants, `Classify`,
  `CommitFault` — the moved decision tree of `mapFetchError`.
- `internal/index.SupportsMediaType` — the moved `supportsType` loop.
- Root: dest.go 97->54, capabilities.go 89->79, fetchfiles.go sheds the
  internal/file and internal/decomp imports entirely, resolve.go one line.
- PR https://github.com/imgoci/go/pull/31, CI green.

THE DESIGN INSIGHT WORTH KEEPING: root error messages interleave public sentinels
with detail text, and an internal package cannot import a root sentinel. Two
different resolutions, chosen per case:
1. Where the sentinel is the SUFFIX (`destination missing role %q: %w`), internal
   returns a DETAIL-ONLY error whose text is exactly today's prefix, and root
   appends with `fmt.Errorf("%w: %w", err, ErrInvalidDest)`. Byte-identical text
   AND both errors stay in the chain. No enum needed.
2. Where the sentinel is the PREFIX and the choice of sentinel varies
   (`mapFetchError`), internal returns a CATEGORY (`transfer.Fault`) and root
   owns the sentinel choice and wording.
That pattern generalizes to slices 5 and 6, which have the same problem with
`ErrInvalidSpec` and `ErrUnsupportedType`.

No test file needed to change, which was a design goal and held. Two independent
differential probes (mine 20 lines, reviewer's 818 lines) came back byte-identical
vs master, including the two load-bearing precedences proven with errors that
satisfy two sentinels at once: a `*file.CommitError` that also wraps another
sentinel (commit wins) and an error wrapping both `decomp.ErrDecode` and
`decomp.ErrSizeMismatch` (size wins, so a size verdict is never reported as a
codec verdict). `go doc -all .` byte-identical.

Two integration fixes I made myself: `CommitFault` had named returns
(`nonamedreturns` lint), and a moved doc sentence went tautological when the
unreachable `[Client.FetchFiles]`/`[ErrInvalidDest]` links were substituted out
("is invalid and is not a valid destination"). Reviewer caught the latter; that is
the second slice in a row where the only real finding was a doc comment damaged by
link adaptation. Standing instruction for slices 5-6: when a moved doc comment
loses a link, name a symbol that IS reachable rather than paraphrasing the
sentence into a tautology.

Main checkout stayed clean this slice (mid-flight check at ~110s).

## 2026-08-17 22:20 — Slice 4 merged; slice 5 implemented, reviewed, PR #32 open
Slice 4 merged as `873ab4f` (#31).

Slice 5 = producer validation out of publish.go (422 -> 270 lines):
- `internal/index/producer_input.go`: `ProducerInput`, `ProducerFile`,
  `ValidateProducerFields` (empty checks + UTF-8 walk + reserved annotations),
  `ValidateProducerRules` (placeholder `Build`), unexported `placeholderIdentityKey`.
- `internal/transfer/publishsources.go`: `PublishSource`, `ValidatePublishSources`.
- `internal/ociref.RequireTagOnly` (moved `checkPublishRef`).
- Nine root declarations deleted; `internal/index.requireUTF8` now reused, which
  retires the last duplicate from the boundary audit.
- One test moved: `TestPlaceholderIdentityKeyIncludesUsage` -> internal/index.
- PR https://github.com/imgoci/go/pull/32, CI green.

TWO INVARIANTS BEYOND TEXT that shaped the contract:
1. ORDER. Today's sequence is fields -> sources -> index rules, and the source
   check lives in a DIFFERENT package from the two index concerns. That is why
   internal/index exposes TWO functions instead of one: fusing them would change
   which error a caller sees when a spec breaks several rules at once. Reviewer
   proved order preservation with 21 multi-violation specs.
2. The placeholder construction byte for byte (`Size: 1`, `ContentSize: 0`,
   ContentDigest from the five-field identity key). Sharing content identity per
   selector key is what makes §6 rule 6 checkable before any bytes exist. Rule 6
   firing on filename disagreement is the test that proves it survived.

ERRORS.UNWRAP ARITY, worth remembering: switching a single-wrap
`fmt.Errorf("%w: name is empty", ErrInvalidSpec)` to the detail-only split
`fmt.Errorf("%w: %w", ErrInvalidSpec, err)` changes `errors.Unwrap(err)` from
returning the sentinel to returning nil (the error exposes `Unwrap() []error`
instead). The reviewer caught it; I checked whether the single-wrap alternative
`%w: %s` was possible and it is NOT — `errorlint` rejects a non-wrapping verb for
an error, so multi-wrap is the only lint-clean way to keep the text identical.
Accepted consciously: `errors.Is` is unaffected, master was already multi-wrap on
the index-rules path, and no consumer in repo/docs/cli walks `errors.Unwrap` on
these paths. Same tradeoff will appear in slice 6.

Reviewer's other finding was again doc drift, and again in a package doc I wrote:
`internal/ociref/doc.go` claimed the package "does not decide whether a missing
tag or digest is an error", which `RequireTagOnly` now does. Fixed. Third slice in
a row where the only real defects were doc comments describing a boundary that
moved — when adding an exported symbol to an existing internal package, re-read
that package's doc.go.

Two lint fixes during integration: `perfsprint` wanted `errors.New` for the two
constant messages (needed an `errors` import), and gofmt caught a trailing blank
line left by the test deletion.

Remaining: slice 6 (list/resolve engines, ~500 lines, the representation-change
risk noted in the audit).

## 2026-08-17 23:55 — Slice 5 merged; slice 6 implemented, reviewed, PR #33 open (campaign complete pending merge)
Slice 5 merged as `ea55aff` (#32).

Slice 6 = both spec §7 query engines out of the root.
- `internal/index/list.go` (§7.2 engine + `Listed`/`ListedRole`/`Alternative`),
  `internal/index/resolve.go` (§7.3 engine + `ValidateResolveQuery` +
  `CapabilityError`), `internal/index/query.go` (shared §7.1 validation +
  query usage canonicalization), `internal/index/entry.go` (`Entry`, `EntriesOf`).
- list.go 338->145, resolve.go 364->104, usage.go 92->65. Root's 12
  representation/role/compression constants and its `deliverableKey` deleted in
  favor of the internal ones; `IsBasicToken` (added slice 1) deleted as dead.
- ROOT PACKAGE NOW: 19 files, 1794 production lines (campaign start: 44 files,
  10050 lines).
- PR https://github.com/imgoci/go/pull/33, CI green.

THE PERFORMANCE STORY, the most valuable thing in this slice:
- `Index` had to stop storing `[]FileEntry` so an internal engine could read it.
  First cut stored `[]index.Descriptor` — but `Descriptor.Selector()` re-derives
  the six-field selector from the ANNOTATION MAP on every call. Master had
  materialized selectors once at parse time. Result at 400 entries: List
  818->1019 allocs and +42% ns; Resolve 3.07us -> 23.3us, **+590%**.
- Fix: `index.Entry` + `EntriesOf`, materializing selector and the three
  annotation-derived accessors once at parse time. Restored Resolve; List still
  carried +201 allocs from building an internal tree and then a public tree.
- I called that irreducible. The REVIEWER REFUTED IT with a working
  implementation: cut all role/alternative slices from one backing array each,
  sized by a non-allocating pre-pass, with full slice expressions so cap==len
  (safer than master's `slices.Clone`, which could return cap>len). It measured
  1019->821 allocs itself before recommending. Applied.
- Final: List 65.8us/821 allocs vs master 61.8us/818; Resolve 3.27us/19 vs
  3.07us/18. +6.5% ns, constant +3/+1 allocs = the cost of one type-mapping
  boundary that did not exist before.
LESSON: benchmark a representation change before believing it is free, and when
a reviewer says a perf claim is wrong, ask for numbers — this one brought them.

Reviewer also caught doc drift for the FOURTH consecutive slice:
`internal/index/doc.go` still scoped the package to the codec plus §6 rules while
it had just gained 11 exported query declarations. Fixed. (Watch the blank-line
trap when appending to a package comment: a bare blank line splits it into two
comment blocks and detaches the first from `package`.)

RECORDED FOLLOW-UP (not done, deliberately): `internal/index` now spans five
concerns — decode, §6 validation, canonical verification, producer Build, query
selection. Reviewer argues for a future `internal/query` package and notes the
coupling is nearly a file move: only `isBasicToken` and `deliverableKey` are
unexported among what the engines need. I agree it is the right next step, but it
is a boundary re-cut, not a move, so it does not belong in a slice whose contract
is move-not-rewrite.

Campaign summary (6 slices, 6 PRs, all reviewer-approved, all CI green):
#28 duplicate spec grammar (-122 root lines), #29 internal/adapters (-109),
#30 internal/ociref (-55), #31 dest+classify (-53 root, 2 internal packages
gained policy), #32 producer validation (-152), #33 query engines (-450).
