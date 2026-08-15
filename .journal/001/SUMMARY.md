---
id: 001
title: Repository bootstrap and onboarding
date: 2026-08-15
status: complete
repos_touched: [imgoci/go (journal branch only), imgoci/bigoci (temp doc, consumed)]
related_sessions: []
---

## Goal
Bootstrap the new `imgoci/go` repository (created public from `meigma/template-go`), understand the imgoci ecosystem, and produce a reviewed architecture plus an executable implementation plan for the canonical Go implementation of the imgoci release format spec.

## Outcome
Goal met. The repo exists and is cloned (template still unrenamed — deliberate; that is implementation slice 0). The session produced three durable artifacts in this folder: `ARCHITECTURE.md` (adversarially reviewed, 3 rounds, updated for bigoci v0.2.0), `PLAN.md` (20 PR-sized work items across 7 slices), and the ecosystem survey in `NOTES.md`. A five-item upstream request to bigoci was filed and **all five shipped same-day in bigoci v0.2.0**, eliminating every upstream dependency from the implementation plan. No implementation code was written; no PRs were opened (all session output lives on `journal/jmgilman`).

## Key Decisions
- Root `package imgoci` at the module root of `github.com/imgoci/go` → import identifier comes from the package clause, no alias needed; CLI becomes a private `cli/` submodule (bigoci pattern) so Cobra/Viper never enter the library go.mod.
- Hand-written index validator; CUE schema + conformance corpus are test oracles only → CUE cannot check canonical bytes (rule 10) or duplicate keys, so a Go layer exists regardless.
- Canonical-bytes verification = `utf8.Valid` gate → decoded-dup-key scan → full-domain JCS transform (`gowebpki/jcs` v1.0.1) → byte-compare. Empirically pre-audited: the transform round-trips invalid UTF-8 (gate is load-bearing), already errors on duplicate keys, silently accepts invalid surrogate *pairs* and precision loss (byte-compare catches both). Audit property: "errors OR output ≠ input", not "errors on every violation".
- `Resolved` bound to `Release` by canonical index digest (`ErrSelectionMismatch`) → closes the spec's fetch→select→retrieve chain across serialization boundaries.
- BigOCI support gated on bigoci ≥ v0.2.0 + imgoci interop fixtures; producer uses `PushByDigest` (no tag writes for file manifests). The pre-v0.2.0 local producer fallback and identity marker-predicate designs are retired (kept in ARCHITECTURE.md review history).
- Two non-nesting retry domains (ours for registry/go-oci-blob with zero inner policies; bigoci self-retrying) → bigoci exposes no public retry control and its zero policy = 4 attempts.
- Stage-then-commit consumer writes: per-call `MkdirTemp` staging + content-addressed flock'd stored cache keyed by full digest; per-file-atomic commit with documented committed-prefix semantics.

## Changes
- `.journal/001/ARCHITECTURE.md` — final reviewed architecture (3 adversarial rounds: 15 blockers found and fixed; appendix has review history + v0.2.0 update note).
- `.journal/001/PLAN.md` — e2e implementation plan: slices 0-6, 20 PRs, dependency arrival schedule, CI/docs/release tracks, §9 decision points scheduled.
- `.journal/001/NOTES.md` — ecosystem survey (spec/go-oci-blob/bigoci), JCS verification probe results, upstream-request log.
- `bigoci/IMGOCI_UPSTREAM_REQUESTS.md` — temp request doc (5 asks with source anchors); consumed and deleted by the bigoci-side agent; shipped as bigoci v0.2.0 (#55-#59).
- `.journal/TECH_NOTES.md` — durable context promoted at close (see that file).

## Open Threads
- Implementation not started: next session begins at PLAN.md slice 0 (rename pass, one PR: `chore: adopt imgoci/go module identity and library shape`).
- ARCHITECTURE.md §9 open questions remain scheduled, not resolved: JCS pin audit (slice 1), zstd/xz strictness spike (slice 4), Windows staging semantics (implementation time), stored-cache retention policy (post-usage).
- Spec is still a draft (2026-08-11); no v1.0.0 before it promotes.

## References
- `.journal/001/ARCHITECTURE.md`, `.journal/001/PLAN.md` (both in this folder).
- bigoci v0.2.0 release: https://github.com/imgoci/bigoci/releases/tag/v0.2.0 (PRs #55 casefold, #56 seam docs, #57 PushByDigest, #58 identity coding, #59 wire re-hash).
- Normative spec: `~/code/imgoci/spec/spec.md`; conformance fixtures `~/code/imgoci/spec/conformance/v1/`.
- Siblings: `github.com/imgoci/go-oci-blob` v1.1.1, `github.com/imgoci/bigoci` v0.2.0.

## Lessons
- Adversarial architecture review earned its cost: round 1 found the JCS verifier would reject valid conformance fixtures and that two sibling-API integration flows were unimplementable as drawn; rounds 2-3 found security-relevant transport and staging-collision flaws. Verify sibling API claims against source, not memory.
- Same-owner upstream asks are cheap: filing five precise, source-anchored requests against bigoci got all five shipped in a day, deleting an entire fallback subsystem from the plan.
