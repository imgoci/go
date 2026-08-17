<!-- Source: planner subagent UsagePlanner, session 007, 2026-08-16. Spec target 46d18b74cc407ac7d61ded7692fc42b644f4d1e2 (imgoci/spec PR #17). -->

# Implementation plan: add the deliverable usage selector

## Scope and source findings

This plan targets the root module `github.com/imgoci/go` and the private `cli/` submodule. It preserves the required clean cutover: no compatibility fields, aliases, deprecated paths, or producer-only checks in consumer validation.

The binding decisions are workable. A public `Usage` value can remain comparable by storing one unexported canonical string in a small struct. The one unavoidable qualification is that a decoded `internal/index.Descriptor` must temporarily retain an untrusted raw annotation until `Validate` runs; `internal/index.Selector.Usage` is therefore guaranteed canonical only for validated `Value`s and producer models accepted by `Build`.

Two corrections/additions to the supplied delta summary:

1. The unsupported-value permission is the final paragraph of **§7.3**, lines 760–763. There is no §7.4 heading in this snapshot.
2. The summary omits three normative points that need coverage: consumers must not infer usage from filenames (§5.3:391–392); producers must declare every applicable standard usage and usage is independent of representation (§5.4:502–513); those semantic assertions cannot be proven by validation or retrieval (§5.4:513–514). The implementation can expose and preserve the declaration, but cannot inspect arbitrary OS images to prove those behaviors.

## Spec delta table

| Requirement | Spec reference | Implementation symbols |
|---|---|---|
| A deliverable is keyed by architecture, target, representation, and the exact usage set; a file adds role. | §2:50, 66–74; §5.5:558–563 | `internal/index.deliverableKey`, rule-6 `fileID`, rule-7 `nameKey`; root `deliverableKey`, `listedGroup`, `matchingDeliverable`; `Deliverable.Usage` |
| `io.imgoci.usage` is descriptor-only and optional; absence means the empty set. | §5.2:294–295 | `internal/index.AnnotationUsage`, `Descriptor.Selector`, `descriptorFromModel`, `isDescriptorOnlyAnnotation`; public `Selector.Usage`; `fileEntryFromDescriptor` |
| A present value is 1+ unique basic tokens, separated by one comma without whitespace, strictly UTF-8-byte sorted, at most 4096 ASCII bytes; present empty is invalid and empty must be omitted. | §5.3:331–336 | New `internal/index/usage.go`; `validateDescriptorRule3`; `descriptorFromModel`; public `NewUsage`, `Usage.String`, `Usage.Values` |
| Consumers must use the selector annotation, never parse a filename for usage. | §5.3:391–392 | Keep `Descriptor.Selector`/`fileEntryFromDescriptor` as the only source; do not change `Filename` interpretation in `index.go`, `fetchfiles.go`, or `internal/filemanifest` |
| The public usage registry is `live`, `install`, `install-offline`; public registries are append-only, while private values retain `x-<owner>-<name>`. | §5.3:359–373; §5.4:494–500 | `producerUsages`, `producerRegistries.usages`, `validateProducerSelector`, `pinnedUsages`, `pinnedSpecCommit` |
| `install-offline` requires `install`; this is consumer rule 4, not a producer-only registry check. Required-role checks are per exact usage set. | §5.4:502–505; §6:584–590 | `validateRule4`, its `deliverableKey`, shared usage relationship helper; **not** a producer-only condition in `validateProducerModel` |
| Producers must declare every applicable standard usage; usage is representation-independent and is only a producer assertion. | §5.4:502–514 | Public/CLI publish APIs and docs expose the complete set; `producer.go` validates names but cannot infer omitted semantic capabilities from content. Document this caller responsibility. |
| Rule 5 uniqueness is the six-field tuple. | §6:589–590 | Adding `Usage string` to comparable `internal/index.Selector` automatically expands `map[Selector]int`; update rule comments and mutation-sensitive tests |
| Rules 6 and 7 use the usage-aware file/deliverable keys. Rule 8 remains unchanged and must allow selectors, including usage, to differ when its listed agreement fields agree. | §6:591–606 | `validateRule6.fileID`, `validateRule7.nameKey`, rule-8 permitted-differences tests; `internal/transfer.checkFileIdentity.fileID` |
| Rule 9 and producer order compare `(architecture,target,representation,usage,role,compression)`, using canonical usage or `""` when absent. | §6:597–600; §9:842–850 | `descriptorOrder`, `sortManifests`, `manifestsInCanonicalOrder`, `formatSelector`; public `sortDeliverables` uses the four-field deliverable key |
| Query usage values must be basic tokens; usage and role lists reject duplicates; a present list filter is non-empty; resolve carries a complete set that may be empty. | §7.1:628–634 | `ListQuery.Usage`, `ResolveQuery.Usage`, `validateUsageList`, `validateListQuery`, `validateResolveQuery`; CLI `queryFlags.usages` |
| List usage matching is containment; nil is wildcard; results expose the exact set and sort by the usage-aware key. | §7.2:684–704 | `Index.List`, `matchesListUsage`, `listedGroup.usage`, `Deliverable.Usage`, `sortDeliverables`, CLI `writeDeliverables` |
| Resolve requires exact equality with the complete requested usage set; empty is valid. | §7.3:716–724 | `Index.Resolve`, `validateResolveQuery`, `matchingDeliverable`; CLI resolve/fetch flag mapping |
| An interpreting operation may report an unknown usage as unsupported, while list/mirror/integrity operations may continue. | §7.3:760–763 | No mandatory new rejection. `Index.List` and `Index.Resolve` must preserve syntactically valid unknown usage. `Capabilities` remains file-manifest-type-only; `ErrUnsupportedType` remains available to operations that actually interpret a usage. |
| Producer output must omit empty usage and emit the canonical non-empty string. | §5.3:335–336; §9:842–850 | `descriptorFromModel`, `wireFromValue`, `Build`; `toIndexSelector`; CLI `documentToReleaseSpec` |

## Canonicalization contract

Add `internal/index/usage.go` as the sole implementation of usage token parsing, ordering, serialization, containment, and the standard relationship. Proposed package-internal API:

```go
// CanonicalizeUsage validates individual basic tokens, clones, sorts,
// de-duplicates, and joins them with one comma. It does not mutate values.
func CanonicalizeUsage(values []string) (string, error)

// ValidateUsage validates the serialized descriptor form. present=false is
// valid only as the empty set; present=true rejects "" and enforces syntax,
// strict order, uniqueness, and the 4096-byte bound.
func ValidateUsage(serialized string, present bool) error

// ValidateUsageRelationship rejects install-offline without install.
func ValidateUsageRelationship(canonical string) error

// UsageValues returns a fresh token slice from a validated canonical string.
func UsageValues(canonical string) []string

// UsageContainsAll performs allocation-free containment over two canonical,
// sorted strings.
func UsageContainsAll(set, subset string) bool
```

Use one unexported comma-token iterator inside this file so `ValidateUsage`, relationship checking, value extraction, and containment do not grow separate parsers. `CanonicalizeUsage` is the only join/serialization function. It must silently de-duplicate because producer callers are not required to sort or de-duplicate; query validation checks duplicates before calling it because queries normatively reject them.

The public type should be invariant-preserving and comparable:

```go
type Usage struct {
    canonical string
}

func NewUsage(values ...string) (Usage, error)
func (u Usage) String() string
func (u Usage) Values() []string
```

`NewUsage` calls `CanonicalizeUsage`, then `ValidateUsage` and `ValidateUsageRelationship`. The zero `Usage` is the empty set. A struct with an unexported string is preferable to `type Usage string`: the latter is comparable but lets callers bypass the constructor with a conversion such as `imgoci.Usage("bad,,value")`.

Layer rules:

- Raw JSON exists only in `Descriptor.Annotations`/wire structs. Invalid usage may exist there between `Decode` and `Validate`.
- A validated `internal/index.Selector.Usage` and every producer `ModelEntry.Selector.Usage` reaching encoding are canonical strings; `""` means absent/empty.
- Public `Selector.Usage` and `Deliverable.Usage` hold `Usage`, never slices, so both remain comparable where required.
- Query `[]string` fields deliberately remain unordered caller input. Validation rejects duplicates, then canonicalizes once per operation. List containment and resolve equality use that one canonical local value.
- CLI publish JSON carries token arrays only until `NewUsage` converts them.

## Ordered implementation steps

### 1. Internal usage value and wire projection

**Files:** add `internal/index/usage.go`; update `internal/index/decode.go`, `internal/index/build.go`, `internal/index/decode_test.go`, `internal/index/build_test.go`.

**Changes:**

- Add `AnnotationUsage = "io.imgoci.usage"`.
- Add `Usage string` between `Representation` and `Role` in `internal/index.Selector`; update its godoc from five-field to six-field identity.
- Have `Descriptor.Selector()` project the raw annotation into `Selector.Usage` so rule validation can see present invalid input. Presence is still read directly from the annotation map because absent and present-empty both project to `""`.
- Implement the canonicalization API above.
- In `descriptorFromModel`, copy normal annotations, then set `io.imgoci.usage` only when `Selector.Usage != ""`; explicitly delete it for empty usage so a caller-supplied annotation cannot smuggle a present-empty or disagree with the selector. Keep the selector authoritative as for the other defined fields.
- Do not modify `internal/filemanifest/testdata/standard-v1.json` or `testdata/canonical/pass/minimal.json`; their omission of usage remains the canonical empty-set encoding.

**Dependency/compile boundary:** none. Keyed selector literals continue compiling with the zero usage field.

**Verify:** `mise exec -- go test ./internal/index -run 'Test(Descriptor|Usage|Build)'`.

### 2. Internal consumer rules and canonical order

**Files:** `internal/index/validate.go`, `internal/index/sort.go`, `internal/index/validate_test.go`, `internal/index/sort_test.go`.

**Changes:**

- `validateDescriptorRule3`: when the annotation key is present, call `ValidateUsage`; omission remains valid. This is where present-empty, whitespace/delimiter errors, token errors, duplicates, order, and 4096-byte overflow become rule 3.
- `validateRule4`: include canonical usage in `deliverableKey`, so required roles are checked independently for every exact set. Independently call `ValidateUsageRelationship` for every descriptor/set and report rule 4 for `install-offline` without `install`.
- `validateRule5`: no new algorithm is needed; `map[Selector]int` gains usage through the comparable string field. Update the six-field comment and error context.
- `validateRule6.fileID` and `validateRule7.nameKey`: add usage.
- Leave rule 8 comparison fields unchanged; add a test that two descriptors sharing a manifest digest may have different usage when every rule-8 agreement field is equal.
- Add usage between representation and role in `descriptorOrder`; update all five-field comments to six-field.
- Add usage to `formatSelector`. Render empty usage as `<empty>` rather than an ambiguous blank positional component; render non-empty canonical text verbatim.

**Dependency:** step 1.

**Verify:** `mise exec -- go test ./internal/index -run 'TestValidateRule[3456789]|TestDescriptorOrder'`.

### 3. Public domain API, list, and resolve

**Files:** add `usage.go` and `usage_test.go`; update `entry.go`, `index.go`, `list.go`, `resolve.go`, `parse_test.go`, `list_test.go`, `resolve_test.go`, and relevant selector-copy helpers in `e2e_bigoci_helpers_test.go`.

**Changes:**

- Add the comparable `Usage` domain type and methods above. Add `Usage Usage` between representation and role in public `Selector`.
- Update `fileEntryFromDescriptor` to construct the public invariant-preserving value from an already-validated internal canonical string through an unexported `usageFromCanonical` helper; do not re-sort or re-validate every accessor call.
- Add `Usage []string` to `ListQuery`. Document `nil` as no filter and non-nil empty as invalid. Change `validateListQuery` to validate unique basic tokens and return the once-canonicalized subset. Add allocation-free containment to the entry filter before grouping.
- Add `Usage Usage` to `Deliverable` so results carry the exact set. Add usage to `listedGroup`, `toDeliverable`, the map key, and sorting. Make `deliverableKey` return a comparable `[4]string` or private struct rather than extending the current NUL-concatenated allocation.
- Add `Usage []string` to `ResolveQuery`. Document nil and empty as the complete empty set in Go. `validateResolveQuery` rejects duplicate or malformed values and returns one canonical local value; it does **not** apply the standard relationship to a query subset (for example, list filtering by only `install-offline` is valid and can match `install,install-offline`).
- Change `matchingDeliverable` to require exact equality of architecture, target, representation, and canonical usage.
- Keep `Selector` comparable; add a compile/behavior assertion using it as a map key. Do not add a slice to it.
- Existing fetch/fetchfiles code needs no selection algorithm change: it consumes `Resolved` entries after exact matching. Update only comments that still describe three-/five-field identities.

**Dependency:** steps 1–2.

**Verify:** `mise exec -- go test . -run 'Test(NewUsage|List|Resolve|Index)'`.

### 4. Producer registry and publish path

**Files:** `internal/index/producer.go`, `internal/index/producer_test.go`, `internal/index/build_test.go`, `publish.go`, `publish_test.go`, `internal/transfer/publish.go`, `internal/transfer/publish_test.go`, `e2e_bigoci_helpers_test.go`, and any test-only public→internal selector copier found during implementation.

**Changes:**

- Add `producerUsages()` with exactly `live`, `install`, and `install-offline`; add `usages` to `producerRegistries`; validate every token in a non-empty canonical usage string against that registry or `isPrivateSelector`.
- Add `AnnotationUsage` to `isDescriptorOnlyAnnotation` so it is invalid on a producer root. Do not add this public-registry check to `Validate`.
- Do not implement the `install-offline` relationship as a producer-only registry rule; `Build` reaches consumer `validateRule4`, which is the authoritative check.
- Extend `pinnedUsages()` and the registry-set test. Bump `pinnedSpecCommit` only with the conformance pin in step 6.
- Update `FileSpec`/`ReleaseSpec` comments from five-field identity and duplicate five-tuples to the usage-aware six-field form.
- Update `toIndexSelector` to copy `s.Usage.String()`.
- Include usage in `preparePublish`’s placeholder content-identity grouping key.
- Include usage in `internal/transfer.checkFileIdentity.fileID`, so equal old four-field file identities with different usage sets may legitimately carry different content.
- Ensure every test/E2E helper that manually copies public selector fields to `index.Selector` copies usage; keyed literals that intentionally model the empty set need no change.

**Dependency:** public `Usage` from step 3.

**Verify:**

```sh
mise exec -- go test ./internal/index -run 'Test(Build|Producer)'
mise exec -- go test ./internal/transfer -run 'Test.*(Identity|Publish)'
mise exec -- go test . -run 'TestPublish'
```

### 5. CLI flags, publish JSON, and TSV contracts

**Files:** `cli/run.go`, `cli/query.go`, `cli/spec.go`, `cli/output.go`, `cli/doc.go`, `cli/run_test.go`, `cli/spec_test.go`, `cli/output_test.go`. `cli/list.go`, `cli/resolve.go`, and `cli/fetch.go` should require no behavioral edits beyond their shared `queryFlags`, but recompile all three commands.

**Changes:**

- Add `flagUsage = "usage"` and `queryFlags.usages stringList`.
- List help: `-usage` is repeatable containment—“require this usage value; repeat to require several (unset: match every usage set).” Map occurrences to `ListQuery.Usage`.
- Resolve/fetch help: `-usage` is repeatable exact-set input—“one value in the complete usage set; repeat for several (unset: select the empty usage set).” Map occurrences to `ResolveQuery.Usage`. Do not mark it required: no flag is the Go/CLI spelling of the complete empty set.
- Add optional `usage []string` to `publishFile`. Convert it through `imgoci.NewUsage(file.Usage...)`; unsorted and duplicate input is normalized, invalid tokens/length/relationship return a path-specific publish-spec usage error. The stored selector receives the resulting domain value.
- Insert usage in both TSV layouts immediately after representation, matching the key order:
  - list: `architecture target representation usage role compression artifactType`
  - resolve: `architecture target representation usage role compression filename artifactType contentDigest contentSize`
- Render the empty set as an empty field (`...representation\t\trole...`), not `-`, `[]`, or `<empty>`. This retains fixed column count, is lossless, and cannot be mistaken for a token.
- Update `cli/doc.go` output grammar and flag semantics in lockstep.

**Dependency:** steps 3–4.

**Verify:** `mise exec -- go -C cli test ./...`.

### 6. Spec pin, conformance corpus, and repo-owned canonical corpus

**Files:** `testdata/conformance/SPEC_COMMIT`, `testdata/conformance/v1/pass/usage-variants.json`, the four new conformance fail files, `internal/index/decode_test.go`, `internal/index/validate_test.go`, `internal/index/producer_test.go`, `.github/scripts/cue_crosscheck.sh`, `testdata/canonical/README.md`, `parse_test.go`, and the canonical fixtures listed below.

**Changes:**

1. Run `.github/scripts/sync_conformance.sh --pin 46d18b74cc407ac7d61ded7692fc42b644f4d1e2`; do not hand-copy around the sync script. Review the resulting five files.
2. Update `conformancePassCount` from 12 to 13 and `conformanceFailCount` from 21 to 25.
3. Map the new fail fixtures in `failFixtureRule`: duplicate/noncanonical/present-empty usage → rule 3; missing `install` → rule 4.
4. Set `pinnedSpecCommit` to the same new hash and add the exact usage registry review table.
5. Raise the CUE script’s conformance minima to 13 and 25 and canonical-pass minimum to account for the two new pass fixtures, so removal of the new cases is detected.
6. Hand-author or independently RFC-8785-canonicalize the repo-owned fixtures. Never generate them with `index.Build` or this repository’s encoder.
7. Add every fail file to `canonicalFailPhases()` as `phaseValidate`; the harness will then prove Decode accepted it, Validate rejected it with `index.ErrRule`, and the fixture itself was byte-canonical so rule 10 did not mask the intended rule.

**Dependency:** all consumer and producer behavior is implemented before importing fixtures.

**Verify:**

```sh
.github/scripts/sync_conformance.sh --check
mise exec -- moon run root:conformance-cue root:conformance-drift
mise exec -- go test ./internal/index
mise exec -- go test . -run 'Test(ParseIndexCanonical|Conformance)'
```

### 7. Documentation and stale source language

**Files:** `README.md`, `doc.go`, `entry.go`, `list.go`, `publish.go`, `internal/index/decode.go`, `internal/index/sort.go`, `internal/index/validate.go`, `internal/index/producer.go`, `internal/transfer/publish.go`, `cli/doc.go`, and the seven documentation pages below.

**Changes:** update every `2026-08-11`/old commit pin to draft date `2026-08-16` and commit `46d18b…`; replace three-/five-field key language; document the empty-set spelling, exact-vs-containment query distinction, and output break. Do not alter `docs/build/` or `docs/.venv/`.

**Dependency:** documentation follows the settled public and CLI contracts.

**Verify:** `mise exec -- moon run docs:build`.

## Answers to the seven open questions

### 1. CLI TSV column and empty-set rendering

**Recommendation:** put `usage` immediately after `representation` in both list and resolve output; render empty usage as the empty TSV field.

**Reason:** this is exactly the spec key/order position, keeps a fixed machine-readable column count, and emits the canonical serialized value (`""` for absent).

**Rejected alternative:** appending usage at the end hides the key structure and makes list/resolve layouts diverge from §9; rendering `[]`, `-`, or `<empty>` invents a noncanonical value consumers must special-case.

### 2. CLI flag design

**Recommendation:** one repeatable `-usage` spelling with command-specific help:

- list: containment; omitted means wildcard/all sets;
- resolve/fetch: the complete exact set; omitted means the empty set.

**Reason:** it maps directly onto the two typed query fields and the spec. Help and `cli/doc.go` must say “contains every requested value” versus “complete exact set,” not merely “usage filter.”

**Rejected alternative:** a comma-valued flag duplicates annotation parsing in the CLI and is inconsistent with repeatable `-role`/`-compression`; requiring one resolve occurrence would make the valid empty set unrepresentable.

### 3. Publish-spec JSON usage shape and ordering

**Recommendation:** `files[].usage` is an optional array of tokens. Pass it to `NewUsage`, which sorts and de-duplicates; omission, `null`, and `[]` all map to the empty set under the existing `encoding/json` slice behavior.

**Reason:** arrays express sets naturally, avoid asking producers to know wire serialization, and honor the binding that producers need not hand-sort.

**Rejected alternative:** a comma string leaks the descriptor wire format into the ergonomic CLI spec and would either require hand sorting or a second parser.

### 4. `Deliverable` usage exposure

**Recommendation:** expose one `Usage Usage` field. Callers get the canonical string with `String()` and a fresh token slice with `Values()`.

**Reason:** this provides both useful views without duplicated state or a non-comparable selector. It also makes exact-set equality explicit.

**Rejected alternative:** a public `[]string` field is mutable, makes containing structs non-comparable, and can diverge from a parallel string field; a bare string lacks safe token access and permits invalid values.

### 5. Repo-owned canonical fixtures

**Recommendation:** add the exact inventory in the next section. All six fails reject at `phaseValidate`; none belong to `phaseDecode` or `phaseVerifyCanonical`.

**Rejected alternative:** relying only on the pretty-printed conformance fixtures does not exercise `ParseIndex`’s ordered phases or prove the fixture is RFC-8785 canonical before semantic validation.

### 6. Existing fixture/E2E meaning

**Recommendation:** keep all existing selectors as the empty usage set. No current repository fixture contains `io.imgoci.usage`, so no existing deliverable splits or merges. Existing resolve queries with nil usage continue selecting those empty-set deliverables.

The assertions that do change are:

- CLI list/resolve output strings gain an empty column after representation;
- selector/order/key comments and explicit key arrays gain usage;
- conformance counts and fail-rule maps grow;
- helper functions that copy selectors must copy usage even if their current values are zero.

Add one focused usage round-trip or in-memory E2E-style unit scenario rather than relabeling unrelated real-registry fixtures. Existing live registry scenarios retain their meaning.

**Rejected alternative:** assigning a standard usage to old fixtures would silently change their semantic claim and force every old resolve query to change without testing a real usage behavior.

### 7. Errors and sentinels

**Recommendation:** retain the sentinel taxonomy.

- Retrieved present-invalid usage and relationship violations: `index.ErrRule` → public `ErrInvalidIndex`.
- Producer registry violations/invalid publish models: `index.ErrRule`/producer error → `ErrInvalidSpec`.
- `NewUsage` and invalid query values: ordinary argument errors; CLI publish-spec/query handling reports usage errors (exit 2) as it does for other bad input.
- `ErrUnsupportedType` stays available for an operation that truly cannot interpret a usage, but list/resolve must not reject syntactically valid unknown usage merely because it is absent from the producer registry.
- Update `formatSelector` to six fields and show `<empty>` explicitly so rule diagnostics identify which deliverable was checked.

**Rejected alternative:** a new `ErrUnsupportedUsage` fragments the spec’s existing unsupported-type behavior without an operation in this codebase that needs a separate policy; mapping unknown consumer usage to `ErrInvalidIndex` would violate producer/consumer separation.

## Test plan and mutation sensitivity

| Step / behavior | Test file and case | Regression it must catch |
|---|---|---|
| Canonical public construction | `usage_test.go`: unsorted input becomes sorted; duplicates collapse; zero/empty round-trip; invalid token, >4096 result, and `install-offline` alone fail; private/unknown syntactic token succeeds | If sorting, de-duplication, size, relationship, or unknown-value acceptance is deleted, at least one assertion fails |
| Comparability | `usage_test.go` or `entry` test: use two `Selector` values as `map[Selector]...` keys and compare equal usages | Compile/runtime regression if a slice is introduced |
| Wire presence | `internal/index/decode_test.go`: absent gives `""`; valid present is projected exactly; present-empty remains observable through annotations for validation | Catches missing decode mapping and accidental absent/present conflation in rule 3 |
| Producer omission/emission | `internal/index/build_test.go`: zero usage emits no annotation; unsorted public input is canonicalized before model conversion; non-empty emits exact string | Catches always-present empty annotation or dropped usage. Keep golden artifacts independent. |
| Rule 3 | `validate_test.go`: malformed delimiters/whitespace, duplicate, descending order, present empty, invalid token, exactly-4096 boundary pass and 4097 fail | Every negative directly calls `Validate`; deleting the production check makes it fail the test by succeeding |
| Rule 4 relationship | `validate_test.go`: `install-offline` without `install` must be rule 4; with both passes | Deleting relationship check makes the negative succeed and fails test |
| Rule 4 keying | `validate_test.go`: one `linux-netboot` empty-usage deliverable has kernel, a `live` deliverable with same first three fields has only initramfs; expect rule 4 | If usage is omitted from required-role key, old merged group would incorrectly pass |
| Rule 5 | `validate_test.go`: exact non-empty six-field duplicate must be rule 5; same old tuple with different usage passes rule 5 | Catches both failure to include usage and loss of duplicate rejection |
| Rule 6 | `validate_test.go`: same old file key, different usage, different content identity is valid | Catches omission of usage from `fileID` (an erroneous rejection) |
| Rule 7 | `validate_test.go`: same filename across different usage deliverables is valid; retain existing same-usage/different-role rejection | Catches omission of usage from `nameKey` while preserving actual collision rejection |
| Rule 8 | `validate_test.go`: shared manifest digest with different usage and equal rule-8 fields is valid | Catches an accidental new agreement requirement |
| Rule 9 | `sort_test.go`: add “usage decides before role” and “empty usage sorts before present usage”; update expected tuple comments | If usage comparison is deleted, the intentionally reversed role/usage case yields the wrong sign/order |
| List validation | `list_test.go`: nil valid; non-nil empty, duplicate, invalid token fail | Deleting any query check makes a negative unexpectedly succeed |
| List containment/exact grouping | `list_test.go`: same old key with empty, `install`, `install,install-offline`, `live`; nil returns all exact sets in order; `install` returns two; `install-offline` alone returns the compound set; unrelated returns none | Catches equality instead of containment, wildcard errors, grouping without usage, and missing result exposure |
| Per-usage roles in list | `list_test.go`: role filter applies within one exact usage group, not the union across usage variants | Catches grouping before usage filtering |
| Resolve validation/equality | `resolve_test.go`: duplicate/invalid query values fail; nil and empty select empty set; unsorted complete list canonicalizes and selects compound set; a subset does not match | Catches deleted validation, containment in resolve, or nil-as-wildcard |
| Unknown consumer usage | `list_test.go`/`resolve_test.go`: a valid private or future usage lists/resolves normally | Catches an illegal consumer registry check |
| Producer registry | `producer_test.go`: all pinned public usages and `x-owner-name` pass; unknown bare usage fails; pin mismatch fails | A public-values-only happy test would pass if validation were deleted, so the unknown bare negative is mandatory |
| Publish mapping | `publish_test.go`: publish with a valid usage yields that annotation; unknown bare producer usage wraps `ErrInvalidSpec`; two old-identical selectors split by usage are accepted | Catches a dropped `toIndexSelector`, missing producer registry, or old identity grouping |
| Transfer identity | `internal/transfer/publish_test.go`: different usage sets may carry different content identity | Catches omission from the pass-1 `fileID` |
| CLI publish spec | `cli/spec_test.go`: array order/duplicates normalize; absent/empty is zero; invalid token/relationship error names `files[i].usage` | Catches direct string copying or skipped constructor |
| CLI flags/help | `cli/spec_test.go`, `cli/run_test.go`: map repeated flags to the right query; help includes distinct list versus resolve/fetch wording | Catches omission on any command or ambiguous contract text |
| TSV | `cli/output_test.go`: update every existing expected row with the empty column and add non-empty multi-token output | Catches wrong column position and lossy empty rendering |
| Conformance | `decode_test.go` counts 13/25; `validate_test.go` maps all four names; `producer_test.go` pins `46d18b…` | Deleting a fixture or rule mapping fails the harness |
| Canonical phases | `parse_test.go` maps every new fail to `phaseValidate` | Earlier Decode and later VerifyCanonical are asserted to accept, so a generic `ErrInvalidIndex` from the wrong phase cannot satisfy the test |

No new normative rejection should be covered only by a happy-path test. The producer public-registry, relationship, duplicate/order, query-validation, and exact-match checks each have an input that is accepted if that specific check is deleted, so the test then fails. Positive tests are used only where the bug would be an erroneous rejection or incorrect projection/order.

## Fixture inventory

### `testdata/canonical/pass`

1. **`usage-variants.json`** — independently canonicalized byte twin of the new spec pass fixture; proves canonical serialization and ascending usage order across `install`, `install,install-offline`, `install,install-offline,live`, and `live`.
2. **`usage-empty-and-present.json`** — two otherwise identical standard deliverables with absent usage and `live`, absent first, with separate manifest digests and valid `disk` roles. Proves the empty/present usage split and empty-string §9 ordering.

Add README rows describing those exact properties.

### `testdata/canonical/fail`

All are RFC-8785-canonical bytes and all map to **`phaseValidate`**:

1. **`duplicate-usage-value.json`** — present `install,install`; consumer rule 3.
2. **`noncanonical-usage-order.json`** — present `live,install`; consumer rule 3.
3. **`present-empty-usage-value.json`** — present `""`; consumer rule 3.
4. **`install-offline-without-install.json`** — syntactically valid `install-offline`; consumer rule 4.
5. **`duplicate-six-field-selector.json`** — two descriptors with the same non-empty `(architecture,target,representation,usage,role,compression)` and otherwise non-masking fields; consumer rule 5.
6. **`canonical-wrong-usage-descriptor-order.json`** — two valid, distinct usage variants of the same old key placed `live` before absent usage; give each deliverable its required role and distinct manifest digest while keeping other rules valid; consumer rule 9.

Add one README table row per file naming the rule and why Decode/rule 10 do not mask it.

With the bumped pin, `.github/scripts/cue_crosscheck.sh` loads the updated CUE schema. It will vet both new canonical **pass** files successfully. The script intentionally does not submit `testdata/canonical/fail` to CUE because that directory also contains byte-canonicalization failures that a structural CUE schema may accept; no new loop should change that. If run manually, the updated CUE schema should reject all six usage fail files through its usage syntax/order/relationship or cross-entry/order constraints.

## Documentation impact

- **`docs/docs/reference/api.md`** — the pinned revision is stale; `Selector` is no longer five-field; the `Index`, `ListQuery`, `Deliverable`, `ResolveQuery`, `ReleaseSpec`, and `FileSpec` excerpts omit usage; duplicate-five-tuple and three-field-deliverable descriptions are wrong. Add `Usage`, `NewUsage`, exact/containment semantics, and the producer caller’s semantic responsibility.
- **`docs/docs/reference/cli.md`** — the pin is stale; publish JSON calls five fields required and omits `files[].usage`; list/resolve/fetch tables omit `-usage`; both TSV grammars have one fewer column. Document array normalization and empty-field output.
- **`docs/docs/reference/capabilities.md`** — the pin is stale and the page could imply all “unsupported” selection is file-manifest capability filtering. State that `Capabilities` does not registry-filter usage and that syntactically valid unknown usage remains listable/resolvable.
- **`docs/docs/reference/errors.md`** — the pin is stale; `ErrInvalidIndex` and `ErrInvalidSpec` descriptions omit invalid usage relationship/syntax and producer usage registry failures. Keep `ErrUnsupportedType`; do not promise unknown usage is rejected universally.
- **`docs/docs/how-to/resolve-deliverables.md`** — “same architecture, target, representation” is now incomplete; inspection output and commands omit usage. Explain list containment and require resolve/fetch callers to pass the complete exact set, with no flags for empty.
- **`docs/docs/how-to/verify-a-release.md`** — the pin is stale; the resolve example omits the newly explicit complete set. Add `Usage: nil` with an empty-set explanation or a concrete complete set, and mention usage syntax/relationship/key checks in index validation.
- **`docs/docs/tutorials/first-release.md`** — the pin/output transcript is stale. Keep the sample’s usage omitted so it truthfully means empty; show the added empty TSV column and explain why resolve/fetch uses no `-usage`. Do not claim the tutorial’s dummy disk has a real standard usage capability.
- **`README.md` and root `doc.go`** — update the implemented date/commit and package summary. Update stale three-/five-field source godoc throughout the touched files.

## Risks and breaking changes

- **Public source API:** adding fields breaks unkeyed composite literals for `Selector`, `ListQuery`, `ResolveQuery`, and `Deliverable`; keyed callers compile but resolve semantics change. `Selector` equality/map identity now includes `Usage`.
- **Resolve behavior:** nil/no usage is not wildcard. It means the exact empty set. A caller that previously selected an old-key deliverable will stop matching if the producer republishes that logical deliverable with non-empty usage; it must request the complete set.
- **List behavior:** one old three-field group can become several results. Role containment is evaluated independently per usage set, not over their union.
- **CLI machine output:** both TSV formats insert a column in the middle. Existing scripts must update column numbers; this is intentionally a clean break.
- **CLI semantic asymmetry:** the same flag name is wildcard-on-omission for list and exact-empty-on-omission for resolve/fetch. Precise help and docs are mandatory.
- **Public invariant:** use an unexported-field `Usage` struct. A named string type would allow invalid direct conversion and undermine the canonical-form premise.
- **Decoded invalid state:** `Descriptor.Selector().Usage` can expose raw noncanonical data before `Validate`; never use it for keys/sorting/selection until validation succeeds. Presence-sensitive rule 3 must read the annotation map.
- **Consumer/producer separation:** never move the public usage registry into `Validate`. Future syntactically valid public or private values must remain consumable. Only the current standard relationship is a consumer rule.
- **Semantic producer obligation:** “declare every applicable usage” and whether an image is truly live/installable/offline-installable are not mechanically verifiable without executing or deeply interpreting the deliverable; the spec explicitly says validation and retrieval do not prove it. The only implementable enforcement is syntax, relationship, and producer registry membership; docs must assign truthfulness to the producer.
- **Prefetch timing:** the existing Go API separates `Fetch` from `Index.List`/`Resolve`, so query validation happens before index inspection but may happen after a caller fetched the index; the current source/docs already call this out as a §7.1 deviation. Usage follows the same validation location. Fixing that API architecture is outside this focused revision and would be a separate breaking redesign.
- **ASCII rules:** do not touch media-type comparison helpers or introduce `strings.EqualFold`. Usage is exact byte comparison; canonical sorting uses `strings.Compare`/`cmp.Compare` on UTF-8 bytes (tokens are ASCII).

## Verification sequence

Run targeted checks as each compile boundary lands, then the full gate:

```sh
# Internal value/rules/order/producer
mise exec -- go test ./internal/index

# Publish transfer identity
mise exec -- go test ./internal/transfer

# Root public API, parsing, selection, publish
mise exec -- go test ./...

# Private nested module
mise exec -- go -C cli test ./...

# Corpus pin and CUE behavior
.github/scripts/sync_conformance.sh --check
mise exec -- moon run root:conformance-cue root:conformance-drift

# Strict docs
mise exec -- moon run docs:build

# Full formatting, lint, builds including Windows, race tests, real-registry
# E2E, strict docs, CUE, drift, and both Go modules
mise exec -- moon run root:check
```

The final gate is the deliverable proof. It must run after all source, fixtures, scripts, CLI output expectations, and docs have been updated; targeted tests are not a substitute for it.
