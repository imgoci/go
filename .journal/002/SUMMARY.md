---
id: 002
title: Implementation from architecture/plan
date: 2026-08-15
status: complete
repos_touched: [imgoci/go]
related_sessions: [001]
---

## Goal
Execute the session-001 implementation plan (`.journal/001/PLAN.md`) end to end: turn the unrenamed template into `github.com/imgoci/go` and build the canonical Go implementation of the imgoci release format through slices 0–5, one reviewed PR per slice, via orchestrated parallel programmer agents plus one adversarial reviewer per slice.

## Outcome
Goal met. Six PRs merged in one session (all squash, all CI-green, each user-reviewed): #7 (slice 0 rename/library shape), #8 (slice 1 offline core), #10 (slice 2 consumer vertical), #11 (slice 3 producer — round trip self-hosting), #12 (slice 4 strict xz/zstd), #13 (slice 5 BigOCI, capabilities gated on the passing §6.4 interop suite). Master at `df469e7` with the full gate green: format, lint, build, build-windows, `-race` tests, docs, conformance-drift, conformance-cue, test-e2e (four-compression + BigOCI e2e against real zot/distribution containers, bidirectional bigoci CLI interop). Slice 6 (polish/release) is the only plan slice remaining. Release Please is provisioned and working (IMGOCI_* credentials, rulesets applied) — but see Open Threads for a blocking flag on its first release PR.

## Key Decisions
- One PR per slice instead of PLAN's 20 sub-PRs → user reviews slice-grained; squash merge keeps main history clean. Sub-PR structure survived as agent task boundaries.
- JCS pin confirmed (closes §9.3): gowebpki/jcs v1.0.1 passes the RFC 8785 vectors + §6.2 audit suite behind the utf8.Valid pre-gate.
- ASCII-only media-type folding everywhere → `strings.EqualFold` does Unicode simple folding (U+017F≡s, U+212A≡k) and accepted look-alike types; custom fold in root + internal/index.
- `ErrInvalidIndex` widened to "invalid retrieved imgoci documents" (index §6 + file manifests §3.1) → spec §8 failures needed a public sentinel without adding one; recorded in godoc.
- Offline Resolve selection failures carry no matchable sentinel in v1 (godoc'd) → additive later if callers need it.
- `WithUnverifiedExternalTransport` = "allow an opaque storage RoundTripper" (never TLS-skipping) → reviewer caught InsecureSkipVerify smuggled under a provenance-relaxation doc.
- Identity enforcement unconditional on the dedicated manifest client → path-scoped wrapping let redirect hops escape (net/http transparently gunzips); every hop passes the RoundTripper.
- Multipart progress machinery deleted rather than wired → unified WireBytes/Retries reporting is explicitly PR6.2 scope; the half-wired merger double-counted across calls.
- Decoder memory bounded at 8 MiB (zstd window, xz LZMA2 dict) → sub-100-byte hostile blobs could force 512 MiB/4 GiB allocations before the content ceiling engaged.
- go pin bumped 1.26.4 → 1.26.5 → go-oci-blob v1.1.1 sets that floor; PLAN's "keep 1.26.4" overtaken by the dependency.
- Release app credentials renamed MEIGMA_* → IMGOCI_* on this repo (key at `op://Development/imgoci-release-please/key.pem`); repo settings/rulesets applied from the committed manifest (repo had none post-template).

## Changes
- Slice 0 (#7): module `github.com/imgoci/go`, doc-only root `package imgoci`, template CLI + release machinery deleted per DELETE_ME library path, moon.yml on the bigoci model, dual licenses, Release Please kept (component `go`, manifest 0.0.0).
- Slice 1 (#8): `internal/jcs` (Verify/Encode + audit suite + RFC vectors), `internal/index` (Decode/Validate rules 1–9/VerifyCanonical/Build + 33 conformance fixtures at SPEC_COMMIT 5b95710 + drift script), root ParseIndex/List/Resolve/Capabilities + byte-level canonical fixtures + CUE cross-check (cue 0.17.1 pinned).
- Slice 2 (#10): `internal/{retry,auth,decomp,file,transfer,filemanifest,registry}` + root Client/Fetch/FetchFiles surface + mockery mocks + e2e harness (testcontainers zot + registry:2, gzipping proxy, htpasswd).
- Slice 3 (#11): registry PUT, re-verifying upload reader (Seek-preserving), `filemanifest.BuildStandard`, `transfer.Publish` (§5.1 order, dedupe, empty-config ensure, index-by-tag-last), root Publish/Source; self-hosting e2e.
- Slice 4 (#12): strict xz/zstd decoders + spike outcomes; four-compression e2e matrix.
- Slice 5 (#13): `internal/multipart` (PushByDigest/PullTo adapter), BigOCI profile reader, `internal/file` StoredCache (per-key flock, always-re-verify), fetchfiles BigOCI branch, opt-in multipart publish with fallback reporting, capability flip; §6.4 interop e2e suite incl. bigoci CLI both directions.
- Repo ops: IMGOCI_* release credentials set; `configure_github_repo.py apply` converged live settings/rulesets; mise pins added (mockery 3.7.2, cue 0.17.1, go 1.26.5).

## Open Threads
- **BLOCKING FLAG: Release Please PR #9 proposes `release 1.0.0`** — violates §9.1 (no v1.0.0 before the spec promotes). Do NOT merge. Likely fix: give release-please an initial 0.x version (e.g. `Release-As: 0.1.0` footer commit or config `initial-version`) before merging any release PR.
- Slice 6 remains: PR6.1 Docker credential store, PR6.2 unified progress (BigOCI WireBytes/Retries latest-absolute merge deferred there), PR6.3 `cli/` submodule, PR6.4 Diátaxis docs, PR6.5 first v0.x release; full {standard,bigoci}×{none,gzip,xz,zstd} matrix now that both slices are on master.
- Dependabot PRs #2/#4/#5 open (routine bumps, unreviewed).
- bigoci still uses MEIGMA_*-prefixed release credentials — cross-repo naming drift.
- mise.toml still pins melange/apko/cosign with a stale comment referencing deleted release workflows — slice-6 cleanup candidate.
- Template skills `.agents/skills/{apko,melange,mise}` describe deleted machinery.
- ARCHITECTURE §9 items still open by design: stored-cache retention tuning (§9.6 v1 rule shipped), Windows behavior verification (§9.7 compile-guarded), auth extraction (§9.8), jsontext successor (§9.3 tracked).

## References
- PRs: imgoci/go #7, #8, #10, #11, #12, #13 (all merged, squash).
- Design: `.journal/001/ARCHITECTURE.md`, `.journal/001/PLAN.md`; spec pin `5b957102eeda16498fdcb80a738431b83abd4197`.
- Prior session: `.journal/001/SUMMARY.md`.

## Lessons
- Adversarial reviewers earned their cost every slice: 4 security-grade defects (InsecureSkipVerify smuggling, redirect-hop identity escape, zstd/xz decode bombs) and 2 releases-would-have-been-broken bugs (missing empty-config push, trusted desc.Size) — all caught pre-merge.
- The claude-backed programmer agents crashed mid-flight constantly (~8 crashes, 2 rogue worktrees, 1 zero-output 35-min run); salvage pattern: check the tree before assuming loss, respawn with "read what exists, finish in same style", wake idle agents via hub to re-apply from their own context. The grok-4.6 programmer swap fixed reliability outright (5/5 clean).
- `wt switch --create --base master` races a just-finished pull: 2-for-2 stale-base worktrees; always `git reset --hard origin/master` after create.
- Local/CI golangci drift produced CI-only findings twice (goconst, revive time-naming) with identical configs — trust CI as the arbiter.
- E2e gates catch what unit suites structurally cannot: the empty-config bug shipped through green unit tests and was caught only by real registries rejecting the manifest PUT.
