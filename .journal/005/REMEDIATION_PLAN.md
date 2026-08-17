# Remediation Plan — Post-Campaign

The release-readiness campaign exercised `master` at `0b4be41` through 8 phases and 28 external-consumer scenarios, covering the root library, private CLI, documentation, registry/authentication boundaries, failure paths, scale, and release packaging. It found exactly one `0.1.0` release blocker: `SECURITY.md` distributes author-facing template directions instead of a security policy. The remaining findings are documentation gaps, one small CLI classification mismatch, dependency-owned diagnostics, cosmetic drift, or observations that should be closed without product work. The runtime implementation had no correctness, integrity, confidentiality, or machine-contract blocker.

## Decision Summary

| Finding ID | Issue | Disposition | Target PR | Blocks `0.1.0` |
|---|---|---|---|---|
| `REL-04-F1` | `SECURITY.md:7-8,24-25` publishes template directions instead of a supported-versions and disclosure policy; the file ships in the module zip. | **FIX NOW** | PR 1 | **Yes** |
| `REL-04-F2` | `SECURITY.md:12` says private reporting applies only “when it is enabled,” although the repository feature is enabled. | **FIX NOW** | PR 1 | No |
| `ADV-04-F1` | `ReleaseSpec.Name` and `Version` enforce spec §5.1/§5.3 grammar, but the public Godoc and API/CLI references do not state it. | **FIX SOON** | PR 2 | No |
| `ADV-04-F2` | Three `testdata/canonical/README.md` rows describe a rule-10 failure that their bytes do not reach. | **FIX SOON** | PR 2 | No |
| `DOC-02-F1` | `docs/docs/index.md:9` omits spec commit `5b957102eeda16498fdcb80a738431b83abd4197`. | **FIX SOON** | PR 2 | No |
| `DOC-01-F1` | Tutorial host port `5000` can silently route the verification request to macOS AirPlay instead of zot. | **FIX SOON** | PR 2 | No |
| `DOC-01-F2` | The tutorial uses `cmp` but omits it from “What you need.” | **FIX SOON** | PR 2 | No |
| `NET-01-F1` | A successful BigOCI `ToDir` fetch retains an empty `<dest>/.imgoci-stage/stored/` tree, but no user documentation mentions it. | **FIX SOON** | PR 2 | No |
| `CLI-01-F1` | Binary top-level help uses the valid generic placeholder `[command]` and gives four examples, while the references enumerate all five help topics. | **DECLINE** | — | No |
| `CLI-02-F1` | Omitting `files[].filename` bypasses the CLI required-member check and exits `6` instead of usage exit `2`. | **FIX SOON** | PR 3 | No |
| `NET-02-F1` | `go-oci-blob` flattens a standard-blob transport cause to `registry request failed` at the top level, although the unwrap chain retains it. | **DEFER** | PR 2 for interim guidance; upstream follow-up for the fix | No |
| `AUTH-03` | A bare `401` without `WWW-Authenticate` fails closed but matches no public sentinel and exits `1`. | **FIX SOON** | PR 2 documents the accepted `0.1.0` behavior | No |
| `AUTH-03-O1` | A bare `401` is attempted four times on publish blob existence but once on `Fetch`. | **DEFER** | Backlog, preferably with the upstream transport work | No |
| `ADV-03` unsupported-compression re-check | A syntactically valid unsupported publish compression such as `x-ft-brotli` fails before I/O but matches no public sentinel. | **FIX SOON** | PR 2 documents the accepted `0.1.0` behavior | No |
| `ADV-03` zstd re-check | The former single-segment “window” diagnostic now says `zstd: decode: decompressed size exceeds configured limit`, matches `ErrDecode`, and rejects before a large allocation. | **CLOSED** | — | No |
| `FAIL-01-O1` | `Retry-After: 0` is a floor beneath the client’s jittered backoff, not an instruction to retry immediately; no shipped contract says otherwise. | **CLOSED** | — | No |
| `BIG-01-O1` | Write-path requests may carry Go’s default `Accept-Encoding: gzip`; all read-path manifest/blob GETs carried `identity`, which is the actual `P-WIRE-01` contract. | **CLOSED** | — | No |
| `NET-01-F2` | Darwin’s untrusted-certificate wording differs from Linux while preserving the same `*tls.CertificateVerificationError` condition. | **CLOSED** | — | No |
| `CLI-03` residual | CLI exit `10` is unreachable through the shipped grammar; `ErrSelectionMismatch` was verified through the library in `ADV-02`. | **CLOSED** | — | No |
| `LIB-03/N1` | The test plan incorrectly groups grammar-malformed publish references with semantic publish-reference violations under `ErrInvalidSpec`. | **DEFER** | No product PR; correct `.journal/005/FUNCTIONAL_TEST_PLAN.md` before reuse | No |
| `BIG-02-F1` | The plan’s all-zero `mkfile -n 3g` source deduplicates to one unique 256 MiB part and cannot prove GiB-scale publish traffic. | **DEFER** | No product PR; correct `.journal/005/FUNCTIONAL_TEST_PLAN.md` before reuse | No |

## PR 1 — `docs: publish the security policy`

**Type:** docs-only. This PR is deliberately limited to the release blocker and its directly related reporting-route hedge. It can be reviewed and merged immediately.

**Scope**

- `SECURITY.md:1-25`

**Changes**

Replace `SECURITY.md` in full with:

```markdown
# Security Policy

imgoci/go uses GitHub private vulnerability reporting.

## Supported Versions

imgoci/go is a pre-v1 project. Only the latest release is supported. Before the first release, use the latest commit on `master`. Older releases and commits are not supported.

## Reporting a Vulnerability

Report vulnerabilities privately through [GitHub private vulnerability reporting](https://github.com/imgoci/go/security/advisories/new).

Do not use public GitHub issues, pull requests, discussions, chat channels, or other public forums for vulnerability reports.

When reporting a vulnerability, include as much of the following as possible:

- affected version, commit, or deployment identifier
- a description of the issue and the security impact
- steps to reproduce or a minimal proof of concept
- any relevant logs, screenshots, or traces
- any suggested mitigations or fixes, if available
```

This states a durable pre-v1 policy without inventing a support window, response time, disclosure deadline, or remediation SLA. It also names the enabled private route unconditionally and removes all four authoring directions at `SECURITY.md:7-8,24-25`.

**Acceptance**

- `## Supported Versions` answers which revision is supported before and after the first release.
- The strings `Do not claim support windows`, `usually enough`, `If the project has`, `add it here`, and `avoid inventing guarantees` are absent.
- The reporting route is direct and unconditional; no public fallback is required.
- `gh api -i repos/imgoci/go/private-vulnerability-reporting` still returns `HTTP/2.0 200 OK` and `{"enabled":true}`.
- A module zip derived from the PR 1 merge commit contains the corrected `SECURITY.md`, not the 1,039-byte file observed in `REL-04` at `0b4be41`.

**Verification**

Re-run scenario `REL-04` in full against the PR 1 merge commit. In particular:

```sh
grep -n 'Do not claim support windows\|If the project has\|add it here\|usually enough\|avoid inventing guarantees' SECURITY.md
gh api -i repos/imgoci/go/private-vulnerability-reporting
```

The `grep` command must have no matches. Re-derive the module zip with an isolated `GOMODCACHE` using `go mod download -json github.com/imgoci/go@<PR-1-MERGE-SHA>`, then inspect the returned zip with `unzip -l` and read its `SECURITY.md`. Campaign sign-off remains blocked until this `REL-04` rerun passes.

**Risk**

The only product risk is accidentally promising support or disclosure terms the project cannot meet. The proposed policy avoids dates and SLAs. Keeping the blocker in its own PR prevents unrelated review or test work from delaying the release gate.

## PR 2 — `docs: correct release-readiness contracts`

**Type:** documentation-only behavior. It is source-touching because `publish.go` and `cli/doc.go` contain Godoc, but it changes no executable statement. This PR is independent of PR 3 and may be prepared in parallel after PR 1 is sent for review.

**Scope**

- `publish.go:25-28,90-93`
- `cli/doc.go:61-64`
- `docs/docs/index.md:9`
- `docs/docs/tutorials/first-release.md:16,47,53,62,106,122,139,160`
- `docs/docs/reference/api.md:284-298,303-305,332-340`
- `docs/docs/reference/cli.md:107-108`
- `docs/docs/reference/errors.md:7-10,66-80`
- `docs/docs/explanation/architecture.md:109-114`
- `testdata/canonical/README.md:36-38`

**Changes**

### `publish.go`

Replace the two `ReleaseSpec` field comments with the exact Godoc below:

```go
// Name is io.imgoci.name. It must be a basic token: 1 to 128 ASCII
// bytes matching ^[a-z0-9]+([._-][a-z0-9]+)*$ (spec §5.1 and §5.3).
Name string
// Version is org.opencontainers.image.version. It must contain 1 to 128
// printable ASCII characters and no whitespace or control characters
// (spec §5.1).
Version string
```

In `Client.Publish`’s validation paragraph, replace the current “non-empty Name/Version” wording with:

```go
// The spec is validated against producer rules 1–8 before network: Name
// grammar (spec §5.1 and §5.3), Version grammar (spec §5.1), UTF-8 of every
// caller string, reserved io.imgoci.* keys, selector and filename grammar,
// duplicate five-tuples, required representation roles, incus-vm→incus,
// filename collisions, and shared-source consistency.
```

These comments quote the implemented contract precisely. `internal/index/decode.go:55-56` defines both 128-byte ceilings; `internal/index/validate.go:65-67` checks the version, `validate.go:120-121` checks the name, and `validate.go:422-457` implements the two grammars.

### `cli/doc.go`

Replace the publish-spec requirement paragraph beginning at line 61 with:

```go
// name, version, and files are required. name must be a basic token: 1 to 128
// ASCII bytes matching ^[a-z0-9]+([._-][a-z0-9]+)*$. version must contain 1
// to 128 printable ASCII characters and no whitespace or control characters.
// Each file requires path, filename, and the five selector fields. filename is
// 1–255 bytes, ASCII alphanumeric first and last, with ASCII alphanumerics plus
// ".", "_", "+", "-" internally.
```

Do not soften the existing statement that `filename` is required; PR 3 makes the adapter honor it.

### `docs/docs/reference/api.md`

Replace the `ReleaseSpec` excerpt with:

```go
type ReleaseSpec struct {
	Name        string            // 1–128-byte io.imgoci.name basic token
	Version     string            // 1–128 printable ASCII characters; no whitespace or controls
	Annotations map[string]string // extra root annotations; io.imgoci.* keys rejected
	Files       []FileSpec
}
```

Insert this paragraph immediately after the `ReleaseSpec`/`FileSpec`/`MultipartSpec` code block and before `type PublishOption`:

```markdown
`ReleaseSpec.Name` is `io.imgoci.name`. It must be a basic token: 1 to 128 ASCII bytes matching `^[a-z0-9]+([._-][a-z0-9]+)*$` (spec sections 5.1 and 5.3). `ReleaseSpec.Version` is `org.opencontainers.image.version`. It must contain 1 to 128 printable ASCII characters and must not contain whitespace or control characters (spec section 5.1).
```

In the publishing prose at lines 332-340, use this exact validation sentence:

```markdown
Spec validation (producer rules 1–8, `Name` and `Version` grammar, UTF-8 of every caller string, reserved `io.imgoci.*` keys, selector and filename grammar, duplicate five-tuples, required roles, filename collisions, and shared-source consistency) also runs before any network I/O.
```

After the existing `Dest` paragraph at lines 293-298, add:

```markdown
`ToDir` reserves `.imgoci-stage` beneath each destination parent for private working state. A standard fetch removes its per-call workspace when cleanup succeeds. A BigOCI fetch also creates `.imgoci-stage/stored` for its content-addressed stored-file cache. Successful commit removes the cache entries and lock files, but the empty `.imgoci-stage/stored/` directory remains. Treat `.imgoci-stage` as library-owned working state, not as a fetched release file.
```

### `docs/docs/reference/cli.md`

Replace the `name` and `version` rows at lines 107-108 with:

```markdown
| `name` | yes | `io.imgoci.name`. A basic token: 1 to 128 ASCII bytes matching `^[a-z0-9]+([._-][a-z0-9]+)*$`. |
| `version` | yes | `org.opencontainers.image.version`. 1 to 128 printable ASCII characters; no whitespace or control characters. |
```

### `docs/docs/index.md`

Replace line 9 with:

```markdown
imgoci/go is the canonical Go implementation of the imgoci release format ([spec v1 draft, 2026-08-11](https://github.com/imgoci/spec), commit `5b957102eeda16498fdcb80a738431b83abd4197`). The library is under active development. Related projects include [bigoci](https://github.com/imgoci/bigoci) and [go-oci-blob](https://github.com/imgoci/go-oci-blob).
```

This makes the landing page agree with the other nine rendered pages.

### `docs/docs/tutorials/first-release.md`

Replace the prerequisite line at line 16 with:

```markdown
- `git`, `curl`, `shasum`, and `cmp`
```

Use host port `5500` consistently while retaining zot’s container port `5000`. The affected tutorial text must read:

```markdown
Run zot, an OCI registry, in a container:

```sh
docker run --rm -d --name imgoci-zot -p 5500:5000 ghcr.io/project-zot/zot:v2.1.20
```

Wait a few seconds, then confirm the registry answers:

```sh
curl -sf -o /dev/null -w '%{http_code}\n' http://localhost:5500/v2/
```

You see:

```
200
```

The registry speaks plain HTTP on `localhost:5500`. That is why every command below passes `-plain-http`; without it the CLI talks `https://`.
```

The four command examples must use these exact references:

```sh
./imgoci publish -plain-http release.json localhost:5500/tutorial/example:v1
./imgoci list -plain-http localhost:5500/tutorial/example:v1
./imgoci resolve -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5500/tutorial/example:v1
./imgoci fetch -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5500/tutorial/example:v1 out
```

The campaign ran this complete walkthrough successfully with the `5500:5000` substitution. Do not add an AirPlay troubleshooting branch to the tutorial; a working default is shorter and less error-prone.

### `docs/docs/explanation/architecture.md`

After “Cache entries are removed on successful commit and retained on failure for reuse” at lines 113-114, add:

```markdown
After a successful BigOCI commit, the cache entries and their lock files are removed, but the empty `<parent>/.imgoci-stage/stored/` directory remains. The directory is reserved library working state, not a deliverable from the release.
```

The finding inventory’s mechanism shorthand needs one correction: `internal/file` does contain directory cleanup. `internal/file/staging.go:172-197` removes per-call workspaces and attempts to remove `.imgoci-stage` when empty. The persistent pair occurs because `StoredCache.Remove` at `internal/file/cache.go:146-170` removes only the cache entry and lock file; no code removes the empty `stored/` directory, so the subsequent `.imgoci-stage` removal cannot remove the non-empty parent. The product change is documentation, not a new cleanup algorithm.

### `docs/docs/reference/errors.md`

Replace the opening error-surface paragraph with:

```markdown
The public error surface is nine sentinel values in `errors.go`. Failures that carry a sentinel wrap it, so match with `errors.Is`. Most messages keep the underlying detail. On the standard blob path, `go-oci-blob` can redact a transport cause from the top-level message while retaining it in the `errors.Unwrap` chain. This page describes the implemented spec revision: imgoci v1 draft, 2026-08-11 (`imgoci/spec` commit `5b957102eeda16498fdcb80a738431b83abd4197`).
```

Add these bullets under `## Unclassified errors`:

```markdown
- A `401 Unauthorized` response without `WWW-Authenticate` cannot start an authentication exchange. It matches no public sentinel and returns `the registry refused the request without saying how to authenticate`; the CLI exits `1`.
- `Client.Publish` rejects a syntactically valid but unsupported compression, such as `x-ft-brotli`, before registry I/O. The error matches no public sentinel.
- A standard blob transport failure may render as `registry request failed`. The underlying cause remains in the `errors.Unwrap` chain. For example, a proxy that applies a non-identity content coding retains `the response is not identity coded` in that chain. The BigOCI path reports that cause directly.
```

This documents the accepted `0.1.0` behavior without promising new sentinels. Do not document the four-attempt bare-401 inconsistency as a contract; that behavior is deferred for implementation review.

### `testdata/canonical/README.md`

Keep the fixture bytes unchanged and make the catalog describe what those bytes actually test. Replace lines 36-38 with:

```markdown
| `exponent-1e2.json` | Descriptor `size` is written as `1e2`, which is not a JSON integer (section 5.2 type check). |
| `exponent-1e0.json` | Descriptor `size` is written as `1e0`, which is not a JSON integer (section 5.2 type check). |
| `unsorted-keys.json` | Duplicate `schemaVersion` keys are rejected by the duplicate-key scan. |
```

Do not silently alter the fixtures to recover their apparent intended rule-10 cases in this remediation. The finding is catalog drift, all three existing files still prove rejection with `ErrInvalidIndex`, and changing byte-level fixtures would be a separate test-design decision.

**Acceptance**

- `go doc github.com/imgoci/go.ReleaseSpec` states the exact name and version limits and grammar from spec §5.1/§5.3.
- `docs/docs/reference/api.md`, `docs/docs/reference/cli.md`, and `cli/doc.go` state the same grammar without changing its meaning.
- `ADV-04` still accepts the 128-byte boundaries, rejects the 129-byte boundaries before I/O, and finds no public-documentation omission.
- The three canonical catalog rows match the actual errors: `unsorted-keys.json` reports `duplicate object key "schemaVersion"`; both exponent files report `manifests[0]: size must be a JSON integer`; all three continue to match only `ErrInvalidIndex`.
- All ten docs pages identify draft `2026-08-11` and commit `5b957102eeda16498fdcb80a738431b83abd4197`.
- The tutorial’s literal walkthrough succeeds on `localhost:5500`, including its now-declared `cmp` prerequisite.
- User references state that an empty `.imgoci-stage/stored/` tree remains after successful BigOCI `ToDir` commit and that it is library-owned working state.
- The error reference explicitly covers bare `401`, unsupported publish compression, and the standard-blob flattened diagnostic without claiming a new sentinel.

**Verification**

Run the verified repository tasks:

```sh
mise exec -- moon run docs:build
mise exec -- moon run root:build
mise exec -- moon run cli:build
```

Then re-run scenarios `DOC-01`, `DOC-02`, and `ADV-04`. `DOC-01` must use the published `5500:5000` mapping with no substitution. `DOC-02` must again build strict docs, render all ten pages, and find the exact spec commit on each page. `ADV-04` must repeat both the grammar boundary matrix and the canonical-fixture catalog audit. The stage-directory wording is checked against the already-observed `NET-01`, `BIG-01`, `BIG-02`, `CLI-02`, and `FAIL-02` filesystem evidence; no behavior change is being claimed.

**Risk**

There is no executable behavior change. The main risks are transcription errors in normative grammar, an inconsistent port substitution, or overstating cleanup. Strict docs, the literal tutorial, and `ADV-04` directly cover those risks. Grouping the campaign’s remaining documentation corrections in one PR is safe because each edit is independently reviewable and reverting any one does not affect runtime behavior.

## PR 3 — `fix(cli): reject a missing publish filename`

**Type:** code-touching. This is the only local executable behavior change in the plan. It is independent of PR 2; PR 2 preserves the existing documented requirement that this PR enforces.

**Scope**

- `cli/spec.go:127-149`, symbol `fileToFileSpec`
- `cli/spec_test.go:29-70`, test `TestDocumentToReleaseSpecRequiresMembers`

**Changes**

In `fileToFileSpec`, immediately after the existing `Path` check and before the selector checks, add:

```go
if file.Filename == "" {
	return imgoci.FileSpec{}, errors.New("filename is required")
}
```

Extend the existing table in `TestDocumentToReleaseSpecRequiresMembers` with a `missing filename` case whose path and all five selector fields are present. Keep it in the existing table; do not create a new test function. The case must assert the observable `filename is required` error. No documentation text changes in this PR: `cli/doc.go:61-62` and `docs/docs/reference/cli.md:112` already say `filename` is required, satisfying AGENTS.md D6 once the adapter matches them.

**Acceptance**

- A publish document that omits `files[0].filename` exits `2` and prints `imgoci: publish: files[0]: filename is required` followed by publish usage.
- Failure stdout is empty and the registry request count does not change.
- A present but grammar-invalid filename still reaches library validation, matches `ErrInvalidSpec`, and exits `6`; the new check distinguishes absence from invalid syntax rather than replacing library validation.
- Valid publish documents are unchanged.

**Verification**

```sh
mise exec -- moon run cli:test
```

Then re-run the missing-filename row of scenario `CLI-02` as a real process against the request-counting witness. Verify exit `2`, the exact complaint above, empty stdout, and zero registry requests. Also run one same-shape valid publish to prove the witness is live.

**Risk**

The change affects only an empty string already documented as missing. It moves that case from library error classification (`ErrInvalidSpec`, CLI exit `6`) to adapter usage classification (exit `2`), matching every sibling required member. Placing the guard in the existing adapter function preserves the hexagonal boundary and adds no abstraction.

## Release Sequencing

1. **Merge PR 1 first by GitHub squash merge.** It is the only remediation PR that gates `0.1.0`; do not wait for PR 2 or PR 3 review.
2. **Re-run `REL-04` against the PR 1 merge commit.** Confirm the enabled private-reporting endpoint and inspect the corrected `SECURITY.md` inside a newly derived module zip. Only a passing rerun satisfies the campaign exit condition and changes the campaign verdict from `NOT READY` to `READY`.
3. **Prepare PR 2 and PR 3 independently.** They can run in parallel after PR 1 is opened. Both are recommended before the first release because they are small findings already demonstrated by the campaign, but neither is a release blocker. If either is deliberately moved after `0.1.0`, record that decision rather than relabeling it as a gate.
4. **Keep Release Please PR #9 open.** Do not close, replace, or retarget `chore(master): release 0.1.0`. After the remediation PRs intended for the first release merge, allow Release Please to refresh PR #9. Re-check that it still proposes `0.1.0`, changes only `.release-please-manifest.json` and `CHANGELOG.md`, and leaves `initial-version: 0.1.0` and the manifest’s pre-release history intact.
5. **Merge PR #9 only after PR 1 and the passing `REL-04` rerun.** PR 2 and PR 3 should be merged first if they remain in the `0.1.0` scope, but their absence alone does not overturn release readiness.
6. The normative spec remains `Status: draft, 2026-08-11`. **No `v1.0.0`, other v1 tag, v1 release, or v1 Release Please proposal may be created or merged while that status remains draft.** The standing release posture remains `0.1.0`, with zero existing tags and releases at the tested baseline.

## Declined and Deferred

- **`CLI-01-F1` — decline.** `[command]` accurately describes the operand, the accepted help-topic set is identical, and the following hint is illustrative rather than an exhaustive grammar. Changing it would be cosmetic churn with no usability or machine-contract gain.
- **`NET-02-F1` — defer the implementation fix upstream.** The cause remains programmatically reachable, transfers fail closed, and a local one-off diagnostic policy would duplicate dependency behavior. PR 2 supplies interim user guidance.
- **`AUTH-03-O1` — defer.** Four bounded existence attempts are wasteful but do not write a tag, disclose credentials, or create an infinite retry. Resolve it with the authentication/retry classification boundary, not as a special-case string check.
- **`LIB-03/N1` — defer to the next plan reuse.** Runtime behavior matches `reference.go:26-31`; only the campaign plan sentence is wrong. There is no product PR.
- **`BIG-02-F1` — defer to the next plan reuse.** The supplementary distinct-parts run already closed the campaign’s scale gap. Correct the generator before another campaign instead of changing product code.

No code change is proposed for the bare-401 sentinel or unsupported-compression sentinel before `0.1.0`. PR 2 makes their accepted classifications explicit. A future sentinel change would alter the public error/CLI-exit contract and needs its own design and behavior tests.

## Upstream (`go-oci-blob`)

`github.com/imgoci/go-oci-blob v1.1.1` owns the top-level flattening in `transport.go:188-205`: `requestError.Error` returns only `<operation> failed`, while `Unwrap` retains the original transport failure. `retryableError` in `retryable.go:9-24` preserves that wrapper unchanged. In this repository, `internal/registry/classifyAdapterError` already recognizes `*contentCodingError` through the chain, so retry behavior is correct and terminal; the loss is diagnostic rendering, not classification or integrity enforcement.

Realistic options are:

1. **Upstream patch — recommended.** Add a safe diagnostic mechanism in `go-oci-blob` that preserves actionable vetted causes without rendering peer-selected URLs, response bodies, redirect locations, credentials, or arbitrary headers. Add regression coverage for a standard blob pull/exists request whose wrapped transport rejects non-identity coding. Once released, update the pin from v1.1.1 in a separate dependency PR and re-run `NET-02` plus the `NET-01` publish-side TLS failure.
2. **Local wrapper at the adapter boundary.** At `internal/registry/get.go:149-152`, the repository could render the known-safe local `contentCodingError.Error()` while retaining the original error as `%w`; `internal/registry/blobwiring.go:239-258` would continue to map sentinels and retry metadata. This is small and reversible, but it fixes only this repository and creates a second diagnostic policy beside the dependency.
3. **Accept and document.** Leave v1.1.1 unchanged. Operators can inspect the `errors.Unwrap` chain, and PR 2 tells them why `registry request failed` may appear. This is safe but leaves a poor first-line diagnosis for gzipping proxies and standard blob TLS failures.

Recommendation: open the upstream issue/patch and accept-and-document until a redaction-safe release exists. Do not block `0.1.0` on it, and do not render arbitrary nested transport text merely to make this one message clearer. Use the local wrapper only if upstream cannot provide a safe fix and real operator reports justify the divergence.

The interim user-facing guidance in `docs/docs/reference/errors.md` should be exactly:

```markdown
- A standard blob transport failure may render as `registry request failed`. The underlying cause remains in the `errors.Unwrap` chain. For example, a proxy that applies a non-identity content coding retains `the response is not identity coded` in that chain. The BigOCI path reports that cause directly.
```

## Journal Updates

Journal changes remain on the personal journal branch; they are not product PRs and must not be bundled into PR 1–3.

### `.journal/TECH_NOTES.md`

Retire the final “Remaining manual coverage gaps” bullet: session 005 closed all eight listed gaps. Replace the session-004 implementation/rehearsal summary with durable campaign context equivalent to:

```markdown
- Release-readiness campaign: session 005 tested `master` at `0b4be41` through 8 phases and 28 external-consumer scenarios. All eight manual coverage gaps carried from session 004 are closed. The campaign found one release blocker: the distributed `SECURITY.md` contained author-facing template directions. `0.1.0` sign-off requires that policy fix and a passing `REL-04` rerun.
- Accepted non-blocking behavior at `0b4be41`: standard-blob transport causes can be flattened by `go-oci-blob v1.1.1` while remaining in the unwrap chain; bare `401` without a challenge and unsupported publish compression match no public sentinel; a successful BigOCI `ToDir` fetch retains an empty `.imgoci-stage/stored/` directory. CLI exit `10` remains unreachable through the shipped grammar and is covered through the root API.
```

Retire the session-004 “misleading zstd single-segment window diagnostic” item entirely. It is no longer an open thread: `ADV-03` observed `zstd: decode: decompressed size exceeds configured limit`, `ErrDecode`, and only a 65,536-byte peak-RSS delta against a 9,437,184-byte declared size.

Do not promote these closed observations into durable product debts:

- `Retry-After: 0` did not bypass the client’s jittered backoff; no shipped document promised otherwise.
- Write-path `Accept-Encoding: gzip` is outside `P-WIRE-01`; all read-path GETs used `identity`.
- Darwin’s x509 wording is platform presentation, not different TLS behavior.

After PR 1 merges and `REL-04` passes, append the actual merge SHA, pseudo-version/module-zip evidence path, and sign-off result rather than rewriting the historical session-005 notes.

### `.journal/005/FUNCTIONAL_TEST_PLAN.md`

Before any reuse, make these plan-only corrections:

1. In `LIB-03`, replace the `*Expected*` sentence that currently says all invalid publish reference/spec cases match `ErrInvalidSpec` with:

   ```markdown
   *Expected*: publish returns `sha256:<64 lowercase hex>`; tag/digest fetches agree; both destination forms contain byte-identical files; invalid destinations match `ErrInvalidDest` and cause zero request-count change; digest-only, tag+digest, and name-only publish references match `ErrInvalidSpec`; grammar-malformed publish references fail before I/O without a public sentinel; invalid producer specs match `ErrInvalidSpec`; every rejected publication causes zero request-count change and writes no release tag.
   ```

   This matches the verified `Reference` Godoc at `reference.go:26-31`.

2. Keep `mkfile -n 3g` for fast allocation in `BIG-02`, but make the twelve parts distinct before publishing. Replace the environment setup command with:

   ```sh
   mkfile -n 3g "$FT/work/bigoci-3g.img"
   python - <<'PY'
   from pathlib import Path

   path = Path("/tmp/imgoci-functional-0b4be41/work/bigoci-3g.img")
   part_size = 256 << 20
   with path.open("r+b") as f:
       for part in range(12):
           f.seek(part * part_size)
           f.write(f"imgoci-part-{part:02d}\n".encode("ascii"))
   PY
   ```

   If the evidence root changes, pass the path as an argument rather than baking a new location into the Python body. Add a setup assertion that the raw BigOCI manifest contains 12 distinct layer digests. Then require publish and fetch terminal `WireBytes` to be `3221225472` for this fresh distinct-parts case. This replaces the supplementary run with the primary scenario instead of weakening the GiB-scale assertion.

3. Correct `BIG-02` step 4’s stale cleanup expectation. Replace “Confirm no `.imgoci-stage` residue after successful commit” with:

   ```markdown
   Confirm that no staged or cache file remains after successful commit. An empty `<dest>/.imgoci-stage/stored/` directory may remain and must contain no files.
   ```

4. In `ADV-03`, close the old diagnostic wording re-check. Require the current message `zstd: decode: decompressed size exceeds configured limit`, `ErrDecode`, and rejection before a codec-sized allocation. Do not continue describing the wording as a known misleading-window finding.

No change is needed to the plan for `Retry-After: 0`, write-path content negotiation, Darwin’s platform-specific x509 sentence, or CLI exit `10`; those results already satisfy the intended contract or residual-risk treatment.

### `.journal/005/NOTES.md` and closeout

Keep the Phase 1–8 entries append-only. After PR 1 and the mandatory `REL-04` rerun, append one sign-off entry containing the tested merge SHA, the new `REL-04/result`, the module pseudo-version and zip evidence, and the final `READY` verdict. The session summary should distinguish the one fixed blocker from non-blocking PR 2/PR 3 work and preserve the rule that no v1 release is allowed while the spec remains draft.
