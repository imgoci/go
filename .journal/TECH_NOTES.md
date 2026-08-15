# Technical Notes

- Use hexagonal architecture at all times. Keep business logic isolated from CLI, filesystem, network, storage, and other external adapters.
- Prefer functional testing before calling any feature complete. Unit tests are useful, but they do not prove the tool works the way the design intends.
- Take an agile approach to development. Avoid waterfall: underspecify when useful, prototype early, learn from the result, and refine from working behavior.
- This repo is the canonical Go implementation of the imgoci release format. Authoritative design: `.journal/001/ARCHITECTURE.md`; implementation map: `.journal/001/PLAN.md`. The normative spec is `~/code/imgoci/spec/spec.md` (draft; no v1.0.0 here before it promotes); its `conformance/v1/{pass,fail}` fixtures are the validator test oracle.
- Target shape: root `package imgoci` at module root `github.com/imgoci/go`; CLI is a private `cli/` submodule (bigoci pattern). Library deps stay minimal; each dep enters at the PR that first imports it (schedule in PLAN.md §1).
- Pins: `github.com/imgoci/bigoci` ≥ v0.2.0 (PushByDigest, casefold decode, identity coding, stable transport seam, upload wire re-hash — all required); `github.com/imgoci/go-oci-blob` v1.1.1 (inject transports, `RetryPolicy{}` = one attempt, own the outer retry budget).
- Canonical-bytes rule: never re-encode for identity; verify = `utf8.Valid` → decoded-dup-key scan → JCS transform (`gowebpki/jcs` v1.0.1, pre-audited 2026-08-15) → byte-compare. The transform is NOT a grammar validator and does not error on all violations — Decode must run first; the audit property is "errors OR output ≠ input". Tracked successor: stdlib `encoding/json/jsontext` once json/v2 leaves GOEXPERIMENT.
- Retry: exactly two non-nesting domains — this repo's loop for its own adapters, bigoci's internal budget for multipart calls (never wrap bigoci in retries; no public retry control exists there).
