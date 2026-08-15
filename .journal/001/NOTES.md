---
id: 001
title: Repository bootstrap and onboarding
started: 2026-08-14
---

## 2026-08-14 19:59 — Kickoff
Goal for the session: bootstrap the new `imgoci/go` repository created from `meigma/template-go` and get it into a working state.
Current state of the world: public repo `imgoci/go` created from the template and cloned to `~/code/imgoci/go`; session-setup complete — journal branch `journal/jmgilman` created from `origin/master`, scaffold journal files committed and pushed; worktree at `.wt/journal-jmgilman`. Template placeholders (module path, binary name, Moon metadata, goreleaser, ghd.toml, release-please, docs) not yet renamed; `DELETE_ME.md` still present.
Plan: awaiting the user's actual request; likely next steps are the template rename pass and first-setup checklist from `DELETE_ME.md`.

## 2026-08-14 21:42 — Ecosystem survey of spec, go-oci-blob, bigoci
Mapped the three sibling repos with parallel researchers. Key findings:

**imgoci/spec** (`~/code/imgoci/spec`): draft, language-neutral wire format for publishing one OS-image release (all its files) in a single OCI repository. `spec.md` is sole normative authority; `schema/release-index-v1.cue` (`#ReleaseIndex`, CUE module `github.com/imgoci/spec/schema@v0`) is canonical machine-readable validation; JSON Schema is generated best-effort; `conformance/v1/{pass,fail}` are informative fixtures checked via `cue vet -c -d '#ReleaseIndex'`. Release = OCI image index (`schemaVersion` 2, `artifactType: application/vnd.imgoci.release.v1`), root annotations `io.imgoci.name` + `org.opencontainers.image.version`. Identity model: deliverable = (architecture, target, representation); file = + role; file entry = one transport alternative (per compression/manifest type). Required descriptor annotations: `io.imgoci.{architecture,target,representation,role,compression,content.digest,content.size,filename}` (content.size is a decimal string). Standard file manifest: `artifactType application/vnd.imgoci.file.v1`, empty config, exactly one `application/octet-stream` layer. Multipart form: bigoci manifest (`application/vnd.bigoci.file.v1`, parts `application/vnd.bigoci.file.part.v1`), minimum two parts in imgoci profile. Compressions: none/gzip/xz/zstd, strict single-unit decoding, no trailing bytes. Deterministic wire identity: descriptors sorted by (arch,target,repr,role,compression) UTF-8 byte order; RFC 8785 (JCS) canonical encoding; consumers reject noncanonical bytes and validate whole index before selection; no alternative fallback after selection. Consumers compare media types case-insensitively; producers write exact lowercase. Non-goals: signatures/trust, tag discovery/version ordering, deltas, boot/conversion. README: "canonical Go implementation under development, not yet public" — that is this repo (`imgoci/go`).

**go-oci-blob** (`github.com/imgoci/go-oci-blob`, v1.1.1, package `blob`): blob subset of OCI distribution only — Exists/Push/Pull/PullRange/Mount. Stdlib + go-digest only; auth is caller-injected `http.RoundTripper` (`WithTransport`), separate credential-stripped `WithStorageTransport` for off-origin redirects. Defaults conservative (monolithic push, serial pull); `WithChunkedUpload`/`WithParallelPull` opt-in. Pull streams with digest verification (verified only at EOF); PullRange intentionally unverified. Retry: `RetryPolicy` (zero = single attempt, `DefaultRetryPolicy()` = 4 attempts full-jitter); `Retryable(err)`/`StatusCode(err)` expose classification for outer orchestrators. Sentinels: ErrNotFound/ErrUnauthorized/ErrTooLarge/ErrDigestMismatch. `WithProgress` (committed bytes) + `WithWireProgress` (wire deltas incl. failed attempts). Nine-registry compatibility campaign documented.

**bigoci** (`github.com/imgoci/bigoci`, Go 1.26.5, prepared 0.1.0): stores one large file (5GB+) as fixed-size parts (default 512 MiB, 8 workers, max 4096 parts) as layers of a standard OCI manifest, `artifactType application/vnd.bigoci.file.v1`, annotations `io.bigoci.file.{digest,size}` + `io.bigoci.part.size`, deterministic compact-JSON manifest encoding. Public surface: `New`, `Client.Push/Pull`, `Reference`, `FromFile/ToFile`, sealed options (`WithPartSize/WithWorkers/WithTitle/WithProgress/WithDockerCredentials/WithCredentials/WithHTTPClient/WithPlainHTTP/WithUnverifiedExternalTransport`), `Progress` snapshots, sentinels ErrNotFound/ErrUnauthorized/ErrNotBigociArtifact/ErrPartTooLarge/ErrDigestMismatch. Hexagonal: internal/{transfer(core+ports),plan,manifest,oci,file,auth,retry}. Uses go-oci-blob for blob Put only (inner retries disabled — outer core owns retry budget). Resumable pull via `.bigoci-partial` sibling + atomic rename; warm push skips existing blobs; manifest written last. CLI (`cli/`, private, unpublished) = push/pull with strict stdout/exit-code contract; bench/ = private benchmark harness that set the defaults.

**Integration constraints for imgoci/go** (from cross-repo analysis):
1. Spec producers must use standard one-blob file manifest unless multipart is needed; imgoci-profile bigoci manifests need >=2 parts (bigoci itself allows 1) — selection logic lives above bigoci.
2. bigoci Pull verifies parts; imgoci additionally requires assembled stored-file digest/size verification, then strict decompress + decoded-content digest/size verification.
3. Spec requires case-insensitive media-type comparison; bigoci's decoder compares exactly — open design decision (fix in bigoci vs adapt here).
4. Likely consumption: go-oci-blob directly for standard one-blob manifests; bigoci for multipart; implement release-index types/validation/JCS encoding/List/Resolve/verified retrieval here. Use spec conformance fixtures as validation oracle.

Full researcher reports: agent://SpecMapper, agent://BlobMapper, agent://BigociMapper (ephemeral).
Next: awaiting user direction; presumably scaffold rename + starting the canonical Go implementation.

## 2026-08-14 22:45 — Architecture designed and adversarially reviewed (3 rounds)
Ran a software-architect agent against an architecture-reviewer agent, hard-capped at 3 review rounds. Both agents were primed with an architecture brief plus the three ecosystem research reports; the reviewer verified claims against spec.md and sibling source directly.

Round outcomes: R1 revise (7 blockers — restricted JCS verifier would reject valid extension-bearing indexes; Release/Resolved unbound; bigoci.Push unusable tag-free; bigoci nonconformance can't be "documented around"; missing stored-layer size check + identity-encoding enforcement; single-retry-loop claim impossible; capabilities API inconsistent). R2 revise (5 blockers — identity wrapper opaque to bigoci's inspectable-transport seam; gowebpki/jcs lacks UTF-8 enforcement; local BigOCI producer fallback incomplete; staging-path suffix collisions; Publish reference forms undefined). R3 revise-but-close (3 localized blockers — bigoci external client carries token-realm traffic so identity enforcement needs a provenance predicate; staging key not unique/concurrency-safe; multipart mutable-source guarantee overstated). Architect produced a final document folding in R3 fixes: marker-predicate identity enforcement with an empirical preservation test, per-call MkdirTemp staging + content-addressed locked stored cache keyed by full digest, honest source-immutability precondition.

Final document: .journal/001/ARCHITECTURE.md. Key decisions: root `package imgoci` at module root; CLI as private cli/ submodule; hand-written validator (CUE = test oracle only); proven JCS transform behind utf8.Valid gate + audit; digest-bound Release/Resolved; BigOCI support conformance-gated on upstream bigoci fixes (5 upstream asks catalogued: case-insensitive decode, identity handling, digest publication, seam docs, wire re-hash); two non-nesting retry domains; stage-then-commit with per-file atomicity; delivery in 7 slices starting with rename pass + offline core.

Next: user review of ARCHITECTURE.md; then slice 0 (rename pass).

## 2026-08-15 09:20 — Independently verified the JCS/UTF-8 claims; audit criteria corrected
Verified §6.2's load-bearing claims with source review (gowebpki/jcs v1.0.1 jcs.go from GitHub) and an executable probe (/tmp/jcs-probe, go1.26.4):
- CONFIRMED: jcs.Transform round-trips invalid UTF-8 byte-identically (values, keys, overlong encodings) — parseQuotedString copies bytes >=0x20 unvalidated. The utf8.Valid pre-gate is genuinely load-bearing.
- CONFIRMED: encoding/json Unmarshal/Valid/Decoder.Token/Marshal all silently U+FFFD-substitute invalid UTF-8; Unmarshal keeps last duplicate key silently.
- CONFIRMED: RFC 8785 §3.1 requires I-JSON (no dup names, Unicode strings, IEEE 754 doubles); §3.2.2.2 requires erroring on lone surrogates.
- NEW: jcs.Transform ALREADY errors on duplicate keys, incl. decoded-equal ("\u0061" vs "a") — our dup-scan is defense-in-depth, not sole line.
- NEW: invalid surrogate PAIRS (\ud800\ud800) accepted silently as U+FFFD (RFC violation); lone surrogates error. Safe only via byte-compare.
- NEW: 2^53+1 silently rounds, -0 → "0", both caught by byte-compare not by transform errors; 1e400/NaN error properly.
- NEW: Transform is not a grammar validator ([1 2] → [12]); Decode-before-VerifyCanonical is an ordering REQUIREMENT.
- NEW: Go 1.26 GOEXPERIMENT=jsonv2 ships encoding/json/jsontext with Value.Canonicalize (RFC 8785) + I-JSON-strict defaults — unusable for a library now (experiment flag propagates to downstream builders) but the tracked successor.
ARCHITECTURE.md updated: §6.2 audit bullet replaced with verified probe results and corrected audit framing ("transform errors OR output != input", not "errors on every violation" — v1.0.1 fails the stricter framing and doesn't need it); §9.3 rewritten with pre-audit result + jsontext successor.
Next: user review; then slice 0 (rename pass).

## 2026-08-15 09:30 — Upstream request doc placed in bigoci
Wrote ~/code/imgoci/bigoci/IMGOCI_UPSTREAM_REQUESTS.md (untracked, temporary — another agent will process it, apply the changes, and cut a bigoci release). Contains the 5 asks from ARCHITECTURE.md §6.4/§6.6 with source anchors re-verified against the current bigoci tree: (1) ASCII case-insensitive media-type decoding in internal/manifest/decode.go checkKind/config/layer comparisons [hard blocker]; (2) Accept-Encoding: identity + Content-Encoding rejection on manifest/blob GETs incl. redirect re-issues, token realms exempt [hard blocker]; (3) digest-publication push mode (PushByDigest, no tag write) [unblocks producer, local fallback exists]; (4) docs blessing BigociExternalBase/BigociWrapExternal seam (endpoint.go:82-87) as stable [docs]; (5) optional wire re-hash of part uploads (push.go SectionReader re-read gap) [hardening]. Doc notes: canonical encoder bytes must not change (imgoci digest-identity oracle); ship 1+2 together; suggested minor release.
Next: bigoci-side agent processes the doc; after release, bump pin in imgoci/go and run interop fixtures.

## 2026-08-15 12:34 — bigoci v0.2.0 shipped all five asks; architecture updated
bigoci v0.2.0 released 2026-08-15 (v0.1.1 interim also exists). Verified all five requests in source after fetch+pull of the local checkout:
- #55 casefold decode: strings.EqualFold at all four decode.go comparisons; encoder untouched.
- #58 identity coding: internal/oci/encoding.go (RFC 9110 token-list parse, refusal before body read), redirect.go copyAllowed carries Accept-Encoding across hops; tests prove marker survives hop, coded store response rejected, token realm exempt. Changelog carries the compatibility note.
- #57 PushByDigest(ctx, repo, src, opts) with repository-only Reference — exactly the recommended shape; no tag write; same transfer guarantees.
- #56 seam docs: BigociExternalBase/BigociWrapExternal documented in options.go WithHTTPClient doc + docs/docs/reference/api.md as stable contract.
- #59 wire re-hash of part bytes during upload (push.go + hardening tests + bench update).
IMGOCI_UPSTREAM_REQUESTS.md was consumed and deleted by the processing agent.
ARCHITECTURE.md updated (targets bigoci v0.2.0): §3.2 multipart source-stability upgraded to wire-verified; §6.4 rewritten — prerequisites satisfied, local producer fallback RETIRED, port maps to PushByDigest; §6.6 bigoci case now "enforced natively upstream", marker-predicate mechanism retired; §6.7/§7/slice 5 reduced to pin-v0.2.0 + interop fixtures; §9.4/9.5 resolved and renumbered; appendix post-review update added.
Net effect: slice 5 no longer depends on upstream; all delivery slices are now purely our work.
Next: slice 0 (rename pass) whenever the user says go.
