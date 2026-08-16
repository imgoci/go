# Spec Conformance Audit — imgoci/go vs imgoci/spec

Date: 2026-08-16
Spec: `~/code/imgoci/spec/spec.md`, draft 2026-08-11, commit `5b95710` (942 lines)
Implementation: `~/code/imgoci/go` at `master` `0b4be41`
Fixture pin: `testdata/conformance/SPEC_COMMIT` = `5b957102eeda16498fdcb80a738431b83abd4197` (matches spec HEAD; 12 pass + 21 fail fixtures byte-identical to the spec repo)

## Method

Five read-only `conformance` agents audited disjoint normative areas rule-by-rule
with `path:line` evidence over both production code and the tests that claim to
prove conformance:

| Agent | Spec area |
|---|---|
| IndexStructure | §4, §5.1, §5.2, §5.3, §5.4 value tables, §6 rules 1–3 |
| CrossEntryCanonical | §6 rules 4–10, §9, §3.1 canonical obligations |
| DiscoverySelection | §7 (capabilities, 7.1 fetch, 7.2 list, 7.3 resolve), §10, §3 repo rule |
| RetrievalVerify | §8, §3.1 as retrieval target, §5.4 compression strictness |
| ProducerPublish | all producer requirements: §3, §3.1, §4, §5.1–5.5, §9, §10, §12 |

Severities below are **reconciled**, not taken from the agents. Four agent
"blocker" claims were downgraded after direct verification of reachability; two
findings were escalated after empirical reproduction. Every reconciliation is
stated.

## Verdict

The consumer validation core (§4, §5.1–5.3, §6 rules 1–10, §9 canonical
verification, §7.1 wire rules, §7.2/§7.3 selection, §8 verification ordering) is
substantively conformant. No path was found that accepts an invalid release
index, produces a non-canonical index, re-encodes bytes used for identity,
presents unverified content as output, or falls back to another transport
alternative after a verification failure.

Two real conformance defects exist, one of them a practical interoperability
break. Beyond those, the dominant issue is **proof, not behavior**: many rules
are implemented correctly but defended by tests that would survive their
deletion, and a handful of test fixtures encode a misreading of the spec.

## Findings

### F1 — Common `xz -9` and long-window `zstd` streams are rejected (blocker, interop)

- Spec: `spec.md:486-494`. `xz` content is "the output of one xz stream";
  `zstd` is "the output of one non-skippable Zstandard frame", constrained only
  by single-unit/complete-consumption rules and, for zstd, no dictionary. The
  spec defines **no** decoder resource profile. §7.1:590-591 further requires
  that every value in a consumer's accepted-compression list "name a
  compression that the consumer can decode".
- Code: `internal/decomp/zstd.go:39-42` defines `maxDecodeWindow = 8 << 20`;
  `zstd.go:187-192` rejects any non-single-segment frame whose window exceeds
  it; `internal/decomp/xz.go:242-276` rejects any LZMA2 dictionary above the
  same ceiling before decoding.
- Empirical reproduction (this audit, throwaway probe against
  `internal/decomp.Decoder`):
  - `xz -9` (64 MiB dictionary, 2 MB payload) → `xz: LZMA2 dictionary 67108864 exceeds 8388608: decode`
  - `zstd -3 --long=27` (128 MiB window, 32 MiB payload) → `zstd: decode: decompressed size exceeds configured limit`
  - `xz -6` (8 MiB) and `zstd -19` (8 MiB window) decode correctly.
- Why it matters: `xz -9` is the ordinary high-compression setting for OS disk
  images, and `--long` is the ordinary zstd setting for multi-GiB payloads. This
  client advertises `xz` and `zstd` capability and then refuses conforming
  streams produced by default tooling. It is not a theoretical hostile-input
  case; it is the mainstream producer configuration.
- Secondary defect: the zstd rejection is reported as a *decompressed size*
  limit, which misdiagnoses a *window* rejection.
- Escalated from the agent's `major`: the agent argued from spec text alone; the
  reproduction shows mainstream tooling output is rejected.
- Decision required (the org owns both repos). Either:
  1. add an explicit decoder-resource clause to the spec (e.g. a minimum
     required window/dictionary a conforming consumer must support, and
     permission to reject beyond it), and raise the default here to cover
     `xz -9`/`zstd --long`; or
  2. keep the ceiling as an opt-in hardening knob with a conforming default.
  The current state — an undocumented-in-spec hard limit below common producer
  settings — is the one option that is both non-conforming and user-visible.
  Tests `internal/decomp/xz_test.go:178-188` and
  `internal/decomp/zstd_test.go:175-184` currently assert the rejection as
  required behavior, so they encode the same misreading and must change with it.

### F2 — Standard layer *size* is never verified for compressed entries (major)

- Spec: `spec.md:745`. "Fetch its file layer and verify the layer digest **and
  size**."
- Code: `internal/transfer/fetchfiles.go:465` wraps the blob in
  `decomp.NewBoundedReader(..., layer.Size)`;
  `internal/decomp/bounded.go:48-72` only converts an **overrun** into
  `ErrSizeExceeded` and returns the underlying `io.EOF` when the stream ends
  early. `copyLayer` then verifies decoded content digest/size
  (`fetchfiles.go:476-478`) but never compares consumed raw bytes with
  `layer.Size`.
- Trigger: a standard manifest declaring `layers[0].size = N+1` whose blob is
  the `N` bytes matching `layers[0].digest`, with correct content annotations.
  The verified-blob reader confirms the digest at EOF, `BoundedReader` accepts
  the short stream, decode and content verification pass, and the file commits.
  A required §8 check silently did not run.
- `compression=none` is incidentally protected by the §8 equality rule
  (`spec.md:766-768`) plus the final content-size check. Every compressed entry
  is exposed.
- No content corruption results (content digest is still verified), so this is
  major rather than blocker.
- The test suite codifies the misreading: `TestBoundedReaderShortStream`
  (`internal/decomp/bounded_test.go:107-118`) asserts that a short stream
  succeeds.

### F3 — Producer mechanism can emit non-conforming standard file manifests (major, unreachable today)

- Spec: `spec.md:151-152` ("must not add other top-level members"),
  `spec.md:156` (`artifactType` must be `application/vnd.imgoci.file.v1`),
  `spec.md:186-189` ("The producer member sets are fixed. A conforming
  producer's standard file manifest is a function of its layer digest and layer
  size alone").
- Code: `internal/filemanifest/build.go:13-25` exposes `BuildInput.ArtifactType`
  and `BuildInput.Annotations`; `build.go:79-101` copies both into the encoded
  manifest, so identical `(digest, size)` can produce different bytes, a sixth
  top-level member, or a non-lowercase artifact type.
- Reachability: the **only** production caller,
  `internal/transfer/publish.go:649-652`, passes just `LayerDigest` and
  `LayerSize`. No public API exposes the other two fields. So no shipped path
  produces a non-conforming manifest.
- Downgraded from both agents' `blocker` on that basis. It remains major because
  the two fields exist solely to enable non-conformance, have no caller, and are
  blessed as valid by `internal/filemanifest/build_test.go:22-40,46-56` — the
  test suite therefore asserts that a non-conforming producer output is correct.
- Fix: delete both fields and the tests that bless them. This is weightless code
  whose only effect is to make a spec violation constructible.

### F4 — Mechanically checkable producer value rules are not enforced (major)

Two enforceable subsets of §5.2/§5.3 producer discipline are missing. Both are
reachable from the public `Publish` API and from the CLI, which adds no stricter
gate (`cli/spec.go:149-179` → `cli/publish.go:49-64` → `Client.Publish`).

- **Private-value naming** (`spec.md:353-355`): a producer-defined
  target/representation/role/compression value "must use `x-<owner>-<name>`",
  and a producer-defined architecture "must use a private `x-` token"
  (`spec.md:330-331`). `internal/index/validate.go:132-156` checks basic-token
  syntax only, so `Target: "qemuu"` or `Architecture: "mysterycpu"` is published
  unchallenged. The public-registry membership test is mechanical; only
  *synonym intent* is not.
- **Annotation location** (`spec.md:306-310`): "A producer must not emit a
  defined annotation at another location". `publish.go:254-271` rejects only the
  `io.imgoci.` prefix, so
  `FileSpec.Annotations{"org.opencontainers.image.version": ...}` — defined at
  the index root only — is emitted on a file-entry descriptor.

Downgraded from `blocker`: consumers must accept both (§6:559-561), so no
interop breaks; the canonical implementation simply fails to prevent its callers
from producing non-conforming output.

Not accepted from the agent: the claim that the library must enforce "use the
public value when it matches the producer's intended meaning". Intent is not
available to a library, and `spec.md:357-358` explicitly scopes the registry to
producer conformance. Enforce the mechanical half only.

### F5 — Standard config/layer `annotations` value types are unchecked (minor)

- Spec: `spec.md:179-182`, in the consumer-acceptance paragraph covering the
  manifest, config descriptor, and layer descriptor: "An `annotations` member,
  when present, must map string keys to string values."
- Code: `internal/filemanifest/standard.go:108` validates the manifest root
  only; `validateEmptyConfig` (`:127-160`) and `validateSingleLayer`
  (`:164-205`) never inspect a nested `annotations` child. So
  `config.annotations = {"bad": 1}` is accepted.
- Minor, and partly a spec-reading question: annotations carry no imgoci
  meaning, the rule's binding location is ambiguous between "manifest root" and
  "all three objects", and nothing observable changes. Downgraded from
  `blocker`.

### F6 — Query validation cannot precede the index fetch (minor)

- Spec: `spec.md:587`. "Before fetching the release, a consumer must validate
  the query."
- Code: `fetch.go:26-62` takes a reference and no query; query validation lives
  in `list.go:88` / `resolve.go:108`, after `transfer.FetchIndex` has run. An
  invalid `ResolveQuery` therefore costs one manifest GET before rejection.
- All required validations exist and are tested; only the ordering relative to
  I/O differs, and the API split (fetch once, query many) is the reason. Impact
  is one wasted round trip, never a wrong result. Downgraded from `major`.

### F7 — Resolve merges §7.3 step barriers (minor)

- Spec: `spec.md:694-695`. Steps 7–11 must each complete for every selected role
  before the next begins; any failure returns no roles.
- Code: `resolve.go:282-290` performs step 8 then step 9 per role;
  `resolve.go:303-306` performs step 10 then step 11 per role.
- The mandated *observable* outcome — no partial result — holds
  (`resolve_test.go:8-26`). The spec does not specify which failure to report,
  so the only difference is internal error attribution. Downgraded from `major`.

## Test-oracle defects

These matter as much as code defects: a rule that no test defends is unproven,
and three fixtures currently prove something other than what they claim.

**Fixtures that fail for the wrong reason** (`testdata/canonical/fail/`):

1. `unsorted-keys.json` contains `"schemaVersion":2` twice, so it is rejected by
   duplicate-key decoding before key-order verification. This is the misnamed
   fixture noted in session 004.
2. `exponent-1e2.json` and `exponent-1e0.json` place the exponent in the known
   descriptor `size` member, so `internal/index/decode.go:477-513` rejects them
   as non-integer tokens before rule 10 ever runs. To test rule 10, the exponent
   must sit in an ignored unknown numeric member.
3. `parse_test.go:51-76` asserts only `ErrInvalidIndex`, so it cannot detect
   any of the above. The canonical harness should assert the failing phase.

**Tests that assert a misreading:** `internal/decomp/bounded_test.go:107-118`
(short stream must succeed — see F2); `internal/decomp/xz_test.go:178-188` and
`internal/decomp/zstd_test.go:175-184` (8 MiB rejection required — see F1);
`internal/filemanifest/build_test.go:22-40,46-56` (non-conforming producer
output is valid — see F3).

**Self-oracle problem (producer side).** No test compares producer bytes with an
independent golden. `internal/filemanifest/build_test.go:13-53` validates
`BuildStandard` output with the same package's `ValidateStandard` and `jcs`;
`internal/index/build_test.go:7-51` round-trips through the same
`Decode`/`Validate`/`VerifyCanonical`; `fixtures_e2e_test.go:354-382` builds its
"canonical" manifests with the same `internal/jcs.Encode` and never compares
them with `BuildStandard`. A shared constant or member-set error passes
everything. This is exactly how F3 stayed green. The spec ships
`testdata/canonical/pass/*.json`, but `parse_test.go:10-37` only checks that
they parse; nothing builds an equivalent model and compares bytes.

**Unproven-but-correct rules** (each would survive deleting its check):

- §3.1 retrieval matrix: wrong/missing `schemaVersion`, wrong top-level
  media/artifact type, missing config, wrong config media type, zero layers,
  malformed/uppercase layer digest, fractional or overflow layer size,
  non-canonical unknown-member content.
- §5.2 annotations: only `io.imgoci.content.digest` and `io.imgoci.filename`
  omissions are tested; the other six required keys are not.
- §5.3 grammars: no invalid target/representation/role/compression value; no
  128/129-byte single-token boundary; no filename 255/256 or `.`/`..` case; no
  uppercase 64-hex content digest; no `schemaVersion: 2.0`; no descriptor size
  `2^53-1` acceptance or `1.0` rejection; version boundaries untested beyond an
  embedded space.
- §5.4 required roles: `raw-4kn` and `iso` missing-`disk` branches have no test.
- §6 rule 6 size/filename disagreement; rule 8 descriptor-size, content-digest,
  and content-size disagreement; rule 8's permitted differences beyond
  architecture; rule 9 precedence for target/representation/role.
- §6:567-568 whole-index rejection is never tested with a *later* invalid
  descriptor.
- §7.1 exact-200: no test rejects 201/202. §7.2: only the architecture key
  component of the deliverable sort is exercised, and the returned
  `ArtifactType` value is never asserted. §7.3: no test limits a multi-role
  result to a requested subset, requests an absent role, or proves different
  per-role compression choices.
- §8 integrity: no standard-blob digest corruption test; no isolated BigOCI
  part short/long size, assembled-digest, or assembled-size case; no post-decode
  compressed content-digest mismatch; no `none` size-equality mismatch; no
  BigOCI-title-vs-filename case.
- §9 producer: no exact `9007199254740991` output assertion, no
  invalid-model-refused-before-encoding test, no repeat-`Build` byte/digest
  determinism test.
- BigOCI unit fixtures (`internal/transfer/fetchfiles_bigoci_test.go:676-693`)
  are not valid BigOCI v1 manifests (no `schemaVersion`, no empty `config`, no
  `io.bigoci.part.size`) and mock `PullTo`, bypassing the validating adapter.
  Real validation is only proven by the container-gated e2e suite.

## Verified clean

Confirmed conformant **and** defended by a test that asserts the rule:

- ASCII-only media-type folding everywhere in §4 comparisons; U+017F/U+212A
  look-alikes rejected. `strings.EqualFold` appears only in
  `internal/auth/static.go:84` for registry-host comparison, outside §4.
- Byte-level index identity: original response bytes are hashed and never
  re-encoded; `utf8.Valid` → decoded-duplicate-key scan → JCS transform →
  byte-compare, in that order, with the "errors OR output ≠ input" audit
  property asserted rather than assumed (`internal/jcs/audit_test.go:193-215`).
- Duplicate JSON keys rejected at root, nested-object, and array-object depth.
- Consumer accepts everything §5.1/§5.2/§3.1 require it to accept: additional
  top-level and descriptor members, unknown annotations including unknown
  `io.imgoci.*` keys, misplaced defined annotations, syntactically valid unknown
  file-manifest types, and case-varied media types.
- Cross-entry rules 4–8 use the correct grouping keys (file key for rule 6,
  deliverable+filename across roles for rule 7, digest for rule 8) and permit
  the differences §6:563-565 allows.
- Rule 9/§9 ordering uses `strings.Compare` on unmodified strings — byte order,
  no collation or normalization.
- `Accept-Encoding: identity` is set on every manifest and blob GET, unconditionally
  and before redirects, so `net/http` transparent gzip never applies;
  non-identity `Content-Encoding` is rejected including list, repeated, and
  case-varied forms.
- `Docker-Content-Digest` is deliberately ignored (permitted by §7.1:621-623),
  proven by a test that supplies a false header and still succeeds.
- Tag → digest resolution, digest pinning, SHA-256-only digest references, and
  digest-only subsequent fetches; same-repository enforcement with descriptor
  `urls` never becoming fetch input.
- §8 ordering: nothing is presented as verified output before every check
  passes; output stays in private staging and is removed on failure; the
  resolved set commits only after all roles verify; there is no
  select-another-alternative path anywhere in retrieval.
- Strict decoders: gzip rejects concatenated members and trailing bytes; xz
  rejects concatenation, four-byte stream padding, trailing bytes, and a missing
  Index/Footer; zstd rejects concatenation, leading and trailing skippable
  frames, trailing bytes, and dictionary-required frames.
- Decoded bytes are counted and hashed while streaming, stopping at
  `io.imgoci.content.size`.
- Stored-cache entries are fully rehashed before reuse; cached bytes are never
  trusted.
- Producer: index published last after blobs and manifests; empty OCI config
  blob always ensured; BigOCI manifests never rewritten or re-encoded; the exact
  `raw` slice that is PUT is the slice whose digest becomes the release
  identity; one-part BigOCI falls back to a standard manifest; rules 1–8
  validated before encoding; `manifests` sorted before encoding.
- All 12 pass and 21 fail conformance fixtures run, and the fail harness maps
  each file to its specified §6 rule rather than accepting any error
  (`internal/index/validate_test.go:301-402`). CUE cross-check vets pass
  fixtures and rejects all fail fixtures at the same pin.

## Recommended order of work

1. Decide F1 (spec clause vs. configurable ceiling). It is the only user-visible
   interop break, and its resolution changes three tests.
2. Fix F2 (verify consumed raw bytes equal `layer.Size`), and invert
   `TestBoundedReaderShortStream`.
3. Delete `BuildInput.ArtifactType` and `BuildInput.Annotations` (F3) plus the
   tests blessing them.
4. Enforce the mechanical producer rules in F4: private-value naming and
   root-only annotation placement.
5. Repair the three canonical fail fixtures and make the canonical harness
   assert the failing phase.
6. Add independent golden-byte tests for `BuildStandard` and `index.Build`.
7. Work through the unproven-rule list; each entry is a rule whose check can be
   deleted today without a red test.
8. F5, F6, F7 as cleanup; all three are defensible as-is if consciously accepted.
