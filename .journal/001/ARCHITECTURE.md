# Architecture: `imgoci/go` — canonical Go implementation of the imgoci release format v1

Status: final (reviewed, 3 rounds; updated 2026-08-15 for bigoci v0.2.0). Targets spec `imgoci/spec` (draft 2026-08-11), `go-oci-blob v1.1.1`, `bigoci v0.2.0`.

## 1. Goals / non-goals

**Goals**

- A Go library implementing both sides of imgoci v1: a **producer** (publish a release: file manifests, blobs, canonical release index) and a **consumer** (fetch, validate, list, resolve, retrieve, verify).
- Full consumer conformance: ten-rule index validation including canonical-order and canonical-bytes checks, original-byte digest discipline, atomic resolve, strict single-unit decompression, end-to-end content digest/size verification, no post-selection fallback.
- Full producer conformance: fixed member sets, RFC 8785 encoding, deterministic descriptor sort, standard-manifest default, ≥2-part BigOCI profile, single-repository publication.
- Hexagonal internals matching sibling idioms: core declares ports; network/disk/auth are adapters; sealed functional options; error sentinels + `errors.Is`; progress snapshots.
- Delegation over reimplementation where the delegate can be conforming: `go-oci-blob` is the blob wire kernel; `bigoci` is the multipart transport **gated on upstream conformance prerequisites** (§6.4). The manifest-endpoint adapter and all imgoci semantics live here.
- The spec's conformance fixtures are a first-class test oracle.

**Non-goals** (inherited from the spec)

- No signatures/attestation/trust (the release-index digest is exposed for external signers; nothing more); no tag discovery, listing, version ordering, or catalogs; no deltas, sparse restore, update policy, or revocation; no boot/import/conversion or parsing of decoded content beyond digest/size; no general OCI image tooling.
- Not v1: producer-side compression convenience (callers supply stored files with declared compression) and streaming consumer output (v1 writes verified files, like bigoci).

## 2. Naming: module, package, CLI, dependencies

- Module `github.com/imgoci/go`; **root package `package imgoci`** at the module root. Go binds the import identifier from the package clause, not the path, so `import "github.com/imgoci/go"` yields `imgoci` with no alias (the `gopkg.in/yaml.v3` → `yaml` pattern). Rejected: `github.com/imgoci/go/imgoci` (stutter); renaming the repo (not this design's call).
- **CLI in a `cli/` submodule** with a `replace` directive, exactly like bigoci, so Cobra/Viper never enter the library `go.mod`. The template's `cmd/template-go` and root-module CLI wiring are deleted in the rename pass. Position: the CLI (`imgoci publish|list|resolve|fetch`) is a **private reference tool** in v1 (bigoci's `cli/doc.go` rationale); its stream/exit-code contract is designed shippable from day one; promotion bar = spec stable + external demand.
- **Direct dependencies** (a direct import is a direct `go.mod` requirement regardless of transitive provenance): `opencontainers/go-digest`; `imgoci/go-oci-blob`; `imgoci/bigoci`; `distribution/reference` (our `Reference` parsing); `opencontainers/image-spec` (we consume `ocispec.Descriptor` from `bigoci.Push` and build descriptors in the local BigOCI producer fallback); a JCS transform (`gowebpki/jcs` or the cyberphone reference port — pinned at slice 1 under the §6.2 audit); `ulikunitz/xz`; `klauspost/compress` (zstd); ORAS credential store (once `WithDockerCredentials` lands, slice 6). "Stdlib + go-digest" describes the pure core packages, not the module.

## 3. Public API sketch

One public package. Everything below is `package imgoci`.

### 3.1 Pure model (no network)

```go
// ParseIndex fully validates: UTF-8 validity, JSON decode with duplicate-
// key rejection, the ten rules of spec §6 including canonical descriptor
// order (rule 9) and canonical bytes (rule 10). Never re-encodes for
// identity; the input bytes are the identity. The returned Index records
// the SHA-256 of the input bytes.
func ParseIndex(b []byte) (*Index, error)

type Index struct { /* unexported: parsed model + canonical digest */ }
func (x *Index) Digest() digest.Digest          // sha256 of the canonical input bytes
func (x *Index) Name() string                   // io.imgoci.name
func (x *Index) Version() string                // org.opencontainers.image.version
func (x *Index) Entries() []FileEntry           // canonical order; deep copies
func (x *Index) Annotations() map[string]string // copied on every call

type FileEntry struct {
    MediaType, ArtifactType string // as written; compare via EqualMediaType
    Digest        digest.Digest    // manifest digest
    Size          int64            // manifest byte length
    Selector      Selector
    ContentDigest digest.Digest
    ContentSize   int64
    Filename      string
    Annotations   map[string]string // deep copy incl. unknown keys
}
type Selector struct{ Architecture, Target, Representation, Role, Compression string }
```

**Immutability:** `Index` and `Resolved` are immutable value views; `Entries()`/`Annotations()` return fresh deep copies on every call. `Release` is genuinely concurrency-safe.

**Media types:** `EqualMediaType(a, b string) bool` compares parameter-free field values per spec §4 (ASCII case-insensitive). HTTP `Content-Type` headers — the only place parameters legally appear — go through `internal/registry`'s `parseContentType`, which strips valid parameters before comparison.

**Capabilities** — the single representation of the spec §7 capability set, shared by offline resolve and the client:

```go
type Capabilities struct{ /* unexported normalized type set */ }
// NewCapabilities validates: must include the standard type (case-
// insensitively), no duplicates after case folding, no parameters, RFC 6838
// syntax.
func NewCapabilities(types ...string) (Capabilities, error)
// StandardCapabilities: {application/vnd.imgoci.file.v1} only — the zero-
// value default everywhere. BigOCI is never assumed.
func StandardCapabilities() Capabilities
```

`List` and `Resolve` are pure functions over a validated index:

```go
type ListQuery struct {
    Architecture, Target, Representation string // "" = match all
    Roles []string
}
type ResolveQuery struct {
    Architecture, Target, Representation string // required, exact
    Roles        []string     // nil = spec default-role rule
    Compressions []string     // required, preference order, no dups
    Capabilities Capabilities // zero value = StandardCapabilities()
}
func (x *Index) List(q ListQuery) ([]Deliverable, error)   // §7.2 incl. sort rules
func (x *Index) Resolve(q ResolveQuery) (*Resolved, error) // §7.3, atomic

type Resolved struct { /* selected entries + source index digest */ }
func (r *Resolved) Entries() []FileEntry
func (r *Resolved) IndexDigest() digest.Digest
```

### 3.2 Client (network)

```go
type Reference string // "registry/repo[:tag][@sha256:...]" — bigoci grammar,
                      // parsed with distribution/reference

type Client struct{ /* immutable settings, lazy transports */ }
func New(opts ...Option) (*Client, error)
// Options (sealed): WithHTTPClient, WithPlainHTTP, WithDockerCredentials,
// WithCredentials, WithUnverifiedExternalTransport.

// Capabilities reports what this built client can retrieve conformingly.
// Standard always; BigOCI only when the pinned bigoci version satisfies the
// §6.4 prerequisites (a compile-time fact of the dependency).
func (c *Client) Capabilities() Capabilities
```

**Consumer path:**

```go
func (c *Client) Fetch(ctx context.Context, ref Reference) (*Release, error) // §7.1
type Release struct{ /* index + pinned digest + origin repo */ }
func (r *Release) Digest() digest.Digest // == r.Index().Digest()
func (r *Release) Index() *Index

// Resolve: identical to r.Index().Resolve(q) except a zero q.Capabilities
// defaults to c.Capabilities(), so selection can never outrun retrieval.
func (c *Client) Resolve(r *Release, q ResolveQuery) (*Resolved, error)

// FetchFiles performs spec §8 for every entry in sel.
// Preconditions (checked before any network I/O):
//   sel.IndexDigest() == rel.Digest()      else ErrSelectionMismatch
//   every sel type ∈ c.Capabilities()      else ErrUnsupportedType
//   destination plan valid (§5.5)          else ErrInvalidDest
func (c *Client) FetchFiles(ctx context.Context, rel *Release, sel *Resolved,
    dest Dest, opts ...FetchOption) error

func ToDir(path string) Dest                // names outputs by io.imgoci.filename
func ToFiles(byRole map[string]string) Dest // explicit per-role paths; map cloned
// FetchOptions: WithProgress(func(Progress)), WithWorkers(int)
```

**`Source`/`Dest` are concrete opaque structs**, path-backed, built only by `FromFile`/`ToDir`/`ToFiles` (bigoci `FileSource`/`FileDest` idiom — no typed-nil interface hazards, no substitution point v1 doesn't offer). `ToFiles` clones the caller's map at construction so later mutation cannot race preflight. Reader/writer variants are an explicit non-goal for v1.

**Side-effect contract (stage-then-commit):** every selected output is downloaded and fully verified into private staging (§5.5) first. Only when **all** roles verify does the commit phase run: sequential fsync+rename per file, in canonical entry order. Promises:

- Any failure before commit (network, verification, decode) ⇒ **zero** committed outputs; reusable staging state retained.
- A commit-phase failure at file N (rename/fsync error — same as a crash there) ⇒ files 1..N−1 committed, N and later retained in staging; the error names the committed roles. Commit is per-file atomic, not transactional across files.
- **Retry after a partial commit:** `FetchFiles` is not resumable across the commit boundary. A retry re-stages every selected role (reusing the content-addressed stored cache where digests still match) and re-commits **all** roles, atomically replacing previously committed outputs. It never skips or trusts prior outputs.
- Injected rename-failure and last-role-verify-failure are both functional tests.

**Producer path:**

```go
type ReleaseSpec struct {
    Name, Version string
    Annotations   map[string]string // extra root annotations (io.imgoci.* rejected)
    Files         []FileSpec
}
type FileSpec struct {
    Source      Source
    Selector    Selector // compression declares what Source already is
    Filename    string
    Annotations map[string]string
    Multipart   *MultipartSpec // nil = standard form
}
type MultipartSpec struct{ PartSize int64 } // 0 = bigoci default

func (c *Client) Publish(ctx context.Context, ref Reference, spec ReleaseSpec,
    opts ...PublishOption) (digest.Digest, error)
func FromFile(path string) Source
```

**Publish reference contract:** tag-only. Digest-only (nothing to name the index) and tag+digest (a read binding with no defined write meaning; silently dropping the digest would be worse) are rejected with `ErrInvalidSpec` **before any network I/O**. An optimistic "publish only if tag still points at X" precondition is a possible future feature, not smuggled into v1.

**Source stability — the honest contract:** a `Source` **must not change during `Publish`**; this is a documented caller precondition, matching bigoci (which hashes in one pass and re-reads ranges via `SectionReader` for upload without wire re-hashing — `push.go:105-151, 350-379`). Defense-in-depth detects most violations but is not a guarantee under concurrent mutation:

- pass-1 `stat` (size, mtime) re-checked before upload;
- **standard path:** the upload reader cryptographically re-hashes the bytes actually streamed to the registry and fails the push at EOF on mismatch with pass 1 — on this path wrong bytes cannot be committed under the declared digest even by a registry that skips commit checks;
- **multipart via bigoci (≥ v0.2.0):** bigoci re-hashes each part's bytes as the wire consumes them and fails the part upload on divergence from its hash pass (upstream #59), giving the standard-path guarantee; imgoci additionally fetches the returned manifest by descriptor digest and requires `io.bigoci.file.digest` to equal the pass-1 stored digest.

A conforming registry additionally rejects mismatched commits. All caller-supplied strings (name, version, annotations, filenames) are validated `utf8.ValidString` before encoding (`json.Marshal` otherwise substitutes U+FFFD silently).

### 3.3 Errors and progress

```go
var (
    ErrNotFound, ErrUnauthorized error
    ErrInvalidIndex      error // any §6 rule; detail names the rule
    ErrInvalidSpec       error // producer-side violations incl. reference form
    ErrInvalidDest       error // destination-plan preflight failure
    ErrDigestMismatch    error // any digest/size verification failure
    ErrUnsupportedType   error
    ErrSelectionMismatch error // Resolved not derived from the given Release
    ErrDecode            error // strict decompression violation
)
```

`Progress` is a bigoci-style absolute snapshot: `{Direction, Phase, TotalFiles, CompletedFiles, TotalBytes, CompletedBytes, WireBytes, Retries}`. `Retries` is unified across both retry domains: our own loop's count plus, per multipart transfer, the **latest** `Retries` value of that transfer's bigoci snapshots (bigoci's field is cumulative — merged as latest-absolute per transfer, never summed per snapshot).

## 4. Internal decomposition

```
imgoci (public root)
 ├─→ internal/transfer   orchestration core; declares ports Manifests/Blobs/Multipart
 │     ├─→ internal/index         index codec + 10-rule validator (pure)
 │     ├─→ internal/filemanifest  standard codec + BigOCI profile reader
 │     ├─→ internal/decomp        strict decoders + bounded counting readers/writers
 │     └─→ internal/retry        transient tagging + THE loop for our own adapters
 ├─→ internal/index ─→ internal/jcs   UTF-8 gate + dup-key scan + JCS transform
 ├─  internal/registry   manifest HTTP adapter + go-oci-blob wiring
 │     ├─  identityTransport      scoped identity-encoding wrapper (§6.6)
 │     └─→ internal/auth          docker/static/anonymous, bearer (bigoci pattern)
 ├─  internal/multipart  bigoci adapter — OWNS ITS OWN RETRY BUDGET, never wrapped
 └─  internal/file       destination planning, staging + stored cache, stage-then-commit
```

Pure core packages import stdlib + `go-digest` (+ codec deps). `transfer` imports only pure packages and declares its ports; adapters satisfy them structurally; the root wires everything.

| Package | Responsibility |
|---|---|
| `internal/jcs` | **Verify** (rule 10): (1) `utf8.Valid(original)` — mandatory first step; neither `gowebpki/jcs` (copies non-ASCII bytes unvalidated) nor `encoding/json` (silently replaces invalid UTF-8) enforces I-JSON's Unicode requirement, so we do, on the raw input. (2) Token-level scan rejecting duplicate keys **compared after JSON string decoding** (`"\u0061"` duplicates `"a"`). (3) Full-domain RFC 8785 transform of the parsed value, byte-compared with the input — booleans, null, nesting, negative/fractional numbers, canonical exponent forms, full escape and UTF-16 key-sort rules. **Encode** (producer): stdlib `json.Marshal` of our fixed shapes through the same transform; caller strings pre-validated. One canonicalization path for both sides. |
| `internal/index` | Three seams: `Decode(bytes)` (UTF-8 + shape + duplicate keys; preserves all members incl. unknown values), `Validate(value)` (rules 1–9: structure, syntax, required roles, `incus-vm` target, dup tuples, cross-entry consistency, filename collisions, shared-digest agreement, descriptor order), `VerifyCanonical(bytes)` (rule 10). Producer: `Build` sorts by the five-field UTF-8 tuple and canonical-encodes. Public `ParseIndex` composes all three; no lenient mode. |
| `internal/filemanifest` | Standard codec: build (fixed members, empty-config constant, canonical bytes) and consumer-validate (§3.1 + canonical bytes, tolerant of extras). BigOCI **profile** reader: ≥2 parts, `io.bigoci.file.{digest,size}` extraction, case-insensitive type checks. (A local BigOCI producer codec was specified pre-v0.2.0 and retired when `PushByDigest` shipped upstream — §6.4.) |
| `internal/decomp` | Strict decoders: `gzip` (stdlib `Multistream(false)`; decoder and trailing-byte probe share one `bufio.Reader` so buffered trailing bytes cannot vanish), `xz` (`ulikunitz/xz` single stream, padding rejected), `zstd` (`klauspost/compress`, single non-skippable frame, no dictionary; frame-header inspection spike at slice 4), `none`. `CountingHashWriter` with content-size abort ceiling. `BoundedReader(r, exact)`: errors the moment raw bytes exceed the declared size (a hostile server cannot force an unbounded drain); at exactly the limit it issues one further read on the underlying reader and requires `(0, io.EOF)` — which for go-oci-blob's verified reader also means the digest passed — propagating an extra byte as a size error and a digest mismatch as itself. It never synthesizes EOF, so the underlying verification is always reached. |
| `internal/transfer` | `Publish`/`Fetch` orchestrators + ports: `Manifests{Get(ctx, ref/digest, accept)(bytes, contentType, error); Put(ctx, digest|tag, mediaType, bytes) error}`, `Blobs{Exists;Push;Pull}` (go-oci-blob shape), `Multipart{Push(ctx, repo, path, partSize)(ocispec.Descriptor, error); PullTo(ctx, repo, dgst, path) error}` (path-typed, tag-free). Owns worker scheduling, plan execution, stage-then-commit sequencing, verification ordering, progress. |
| `internal/registry` | Manifest-endpoint adapter (neither sibling has one): OCI Distribution manifest GET/PUT, exact `Accept`, `parseContentType`, raw-byte discipline. Owns `identityTransport` (§6.6) and constructs the go-oci-blob client bigoci-style (authenticated registry transport + credential-stripped storage transport, `RetryPolicy{}`, write redirects off). `Docker-Content-Digest` ignored (spec-permitted). |
| `internal/auth` | Cloned bigoci pattern: anonymous bearer, static creds, opt-in Docker config store, token caching, off-origin credential stripping. Token-realm requests are routed outside identity enforcement by construction (we own this code). Duplication with bigoci accepted for v1. |
| `internal/multipart` | Wrapper over the **public** `bigoci.Client`: maps settings onto bigoci options, converts progress (latest-absolute `Retries` merge) and sentinels. Self-retrying: never wrapped by `internal/retry`. |
| `internal/file` | Destination-plan preflight, per-call staging workspaces, content-addressed stored cache with locking and secure reopen (§5.5); commit invoked by `transfer` only after all files verify. |
| `internal/retry` | Single loop for this repo's own adapters (`registry`, go-oci-blob ops — where `RetryPolicy{}` truly means one attempt). Full-jitter backoff, `Retry-After` floor. Exactly two non-nesting retry domains exist: ours and bigoci's. |

## 5. Data flow

### 5.1 Publish

```
Publish(ref, spec):
 0. Reference contract: tag-only, else ErrInvalidSpec (no network I/O).
 1. Validate spec: producer rules, dup tuples, required roles, incus-vm
    target, filename collisions, shared-source consistency, UTF-8 of all
    caller strings (§6 rules 1–8 + producer-only rules).
 2. Per file, pass 1 (single read): hash stored bytes; tee into strict
    decoder; hash+count decoded bytes → stored digest + content digest/size.
    Producer strictness = consumer strictness. stat captured.
 3. Upload per file (bounded workers; stat re-checked; source-stability
    contract per §3.2):
    standard (default):
      blob.Exists → blob.Push(layer) with digest-re-verifying reader
      blob.Push(empty config {})
      registry.Put(manifest bytes, by digest)        // canonical bytes
    multipart (explicit opt-in; plan <2 parts ⇒ standard fallback, reported):
      multipart.Push(repo, path, partSize) → descriptor      // §6.4
      registry.Get(desc.Digest) → require io.bigoci.file.digest == pass-1
      stored digest
 4. Build index: sort 5-tuple UTF-8, canonical-encode.
 5. registry.Put(index, by tag). Manifests always after their blobs;
    index always last (no broken artifact on interruption).
 6. Return index digest.
```

Shared-source dedupe by digest; rule 8 enforced before encoding.

### 5.2 Fetch → List/Resolve

```
Fetch(ref):
  registry.Get(ref, Accept: index.v1+json)      // identity invariant active
  → require 200; parseContentType(resp) ≡ index type
  → sha256(original bytes); if ref had @digest, must match
  → parse: top-level mediaType ≡ Content-Type (EqualMediaType)
  → index.Decode + Validate + VerifyCanonical   // failure ⇒ ErrInvalidIndex
  → Release{digest, index}                      // tag pinned; later fetches by digest

List/Resolve: pure, offline — exact case-sensitive filters, UTF-8-sorted
results, atomic stepwise selection (any failure ⇒ no roles). Capability
filtering via the explicit Capabilities value.
```

### 5.3 FetchFiles — standard form (streaming, no stored temp)

```
preconditions: selection binding, capabilities, destination plan (§3.2, §5.5)
STAGE, per entry (bounded workers):
  m := registry.Get(repo@entry.Digest, Accept: entry.MediaType)
  verify sha256(m.bytes)==entry.Digest && len==entry.Size
  verify parseContentType(resp) ≡ manifest.mediaType ≡ entry.MediaType
  verify manifest.artifactType ≡ entry.ArtifactType
  filemanifest.ValidateStandard(m.bytes)          // §3.1 + canonical bytes
  if compression==none: precheck layer digest/size == content digest/size
  rc  := blob.Pull(repo, layer.digest)            // digest-verified at EOF
  br  := BoundedReader(rc, layer.Size)            // errors on excess DURING read;
                                                  // exact-limit probe reaches verify
  dec := decomp.Decoder(entry.Compression)(br)
  n,h := copyCounting(stagedFile, dec, ceiling=entry.ContentSize)
  require h==entry.ContentDigest && n==entry.ContentSize
COMMIT (only if every role verified): fsync+rename in canonical order (§3.2).
No alternative fallback on any failure.
```

### 5.4 FetchFiles — BigOCI form (two-phase, cached stored file)

```
m := registry.Get(repo@entry.Digest, Accept: entry.MediaType)  // checks as 5.3
filemanifest.BigOCIProfile(m.bytes)     // ≥2 parts, whole-file annotations,
                                        // case-insensitive types
stored := storedCachePath(io.bigoci.file.digest)   // content-addressed, §5.5
under per-key lock:
  if stored exists && securely reopened && sha256(stored)==key: reuse
  else multipart.PullTo(repo, entry.Digest, stored)
       // bigoci: per-part verify, mid-pull resume via its own partial
       // (inside our cache workspace), atomic publish of stored
single read of stored: hash raw bytes (require digest == io.bigoci.file.digest,
count == io.bigoci.file.size — the spec layer bigoci does not provide) while
feeding the strict decoder → staged output, counting hash, ceiling.
Success: staged output joins the shared COMMIT phase; cache entry removed on
commit (or retained per policy) — a failed decode retains it for reuse, and
reuse ALWAYS re-verifies the full digest first, so a poisoned or pre-planted
cache entry can never corrupt output, only force a re-pull.
```

### 5.5 Destination planning, staging, and the stored cache

Suffix-derived sibling paths are gone (the filename grammar permits both `a` and `a.imgoci-stored`; `ToFiles` permits arbitrary aliasing). Replaced by:

1. **Preflight (before any network I/O; failure ⇒ `ErrInvalidDest`):** resolve each role's final path — parent directories resolved via `EvalSymlinks` so lexical aliases through symlinked parents are caught — then reject duplicates (two roles → one resolved path), paths that are existing directories, and paths whose final parent's reserved staging entry (see below) would be shadowed. Only the exact staging entry name in each destination parent is reserved; caller trees like `/srv/.imgoci-cache/output` are legal (the reservation is not a global prefix rule). Cross-filesystem `ToFiles` mappings are **allowed**: each role stages beside its own final parent, so every rename is same-filesystem regardless of where other roles live.
2. **Per-call output staging:** in each destination parent, a workspace under the reserved entry `.imgoci-stage/`, created per call via `MkdirTemp` (`0700`) — unique by construction, so concurrent calls never share output staging and need no locking. Staged partials live here; final outputs can never alias it (grammar guarantees `ToDir` names never start with `.`; `ToFiles` paths are checked against the resolved reservation).
3. **Content-addressed stored cache (BigOCI reuse):** completed stored files are cached under `.imgoci-stage/stored/sha256-<full 64-hex digest of io.bigoci.file.digest>` — the key **is** the identity of the bytes, untruncated, so distinct deliverables/compressions/destinations can never collide, and two entries sharing a stored file share the cache correctly. A per-key lock file (`O_CREATE|O_EXCL` + flock; context-cancellable wait) serializes concurrent fetchers of the same stored file: the second caller waits, then finds the completed entry and reuses it after digest re-verification. Reuse never trusts the cache: integrity always comes from re-hashing, so pre-planted or corrupted entries only cause a re-pull.
4. **Secure open/reopen (bigoci partial-hardening patterns):** staging and cache entries are opened with no-follow semantics and checked for expected ownership, mode, and regular type before reuse; mismatches are treated as absent (re-pull). Directories are fsynced where durable reuse matters; handles are closed before rename/removal for Windows-like platforms.
5. **Cleanup:** per-call workspaces removed after commit; cache entries removed on successful commit, retained on failure for reuse.
6. **Tests:** two deliverables sharing role `disk` in one index; two concurrent `FetchFiles` of the same selection (lock contention path); two-role `a`/`a.imgoci-stored` filenames; duplicate `ToFiles` paths (rejected); symlinked-parent alias (rejected); pre-planted wrong-content cache entry (re-pulled, output correct); cross-filesystem `ToFiles` (accepted, commits per-file); staging-reuse round trip.

## 6. Key decisions

### 6.1 CUE schema at runtime: no

Hand-written Go validator; CUE schema and conformance fixtures are test oracles only. (a) A CUE evaluator is a large dependency; (b) CUE cannot check rule 10 or duplicate keys, so a Go layer exists regardless; (c) the validator names the violated spec rule. Drift risk mitigated by the fixture corpus in CI plus an optional `cue vet` cross-check job. Rejected: generating Go from CUE.

### 6.2 RFC 8785: one transform, hard audit gate

- **Consumer verify (rule 10):** `utf8.Valid` on the raw input → decoded-duplicate-key token scan → full-domain JCS transform → byte-compare. Non-canonical spellings (whitespace, `1e2`, non-minimal escapes, unsorted keys) diverge and are rejected; non-I-JSON input (invalid UTF-8, duplicates) is rejected before the transform. Unknown members are preserved in `Decode`'s generic tree so verification sees them even though the semantic model ignores them.
- **Dependency audit — verified 2026-08-14 against `gowebpki/jcs` v1.0.1** (source review + executable probe; RFC 8785 §3.1 requires I-JSON input, §3.2.2.2 requires erroring on lone surrogates):
  - **Invalid UTF-8 round-trips silently** — `{"a":"\xff"}`, an invalid key, and an overlong `\xc0\xaf` all Transform successfully and byte-identically (`parseQuotedString` copies bytes ≥ 0x20 unvalidated; `decorateString` re-emits them). Without the `utf8.Valid` pre-gate a non-UTF-8 index would pass byte-compare. The pre-gate is load-bearing, confirmed.
  - **`encoding/json` cannot substitute for the pre-gate** — `Unmarshal`, `Valid`, and `Decoder.Token` all accept invalid UTF-8 and silently substitute U+FFFD; `Marshal` silently escapes to `\ufffd`; `Unmarshal` silently keeps the last duplicate key. All confirmed empirically.
  - **Duplicate keys: the transform already errors**, including decoded-equal duplicates (`{"\u0061":1,"a":2}` → "Duplicate key" via UTF-16 sort-key equality). Our own decoded-dup token scan is retained as defense-in-depth and as the guarantee that survives a dependency swap, but it is not the only line.
  - **Lone surrogates error; invalid surrogate *pairs* do not** — `"\ud800x"` and `"\udead"` error, but `"\ud800\ud800"` is silently accepted as U+FFFD (via `utf16.DecodeRune`), violating the RFC's MUST-error. Safe in our verifier **only because** the U+FFFD output can never byte-equal the escape spelling, so byte-compare rejects.
  - **Numbers:** `1e400` and `NaN` error; `2⁵³+1` silently rounds and `-0` serializes as `0` — both rejected by byte-compare (output ≠ input), never by the transform itself. `1e2`→`100` canonicalization confirmed.
  - **The transform is not a JSON grammar validator** — `[1 2]` is accepted as `[12]` (whitespace absorbed inside literals). Grammar validity MUST come from the `Decode` seam, which always runs before `VerifyCanonical`; the composition in `ParseIndex` is therefore an ordering requirement, not a convenience.
  - **Audit framing follows from the above:** the required property is *"for every non-canonical or non-I-JSON input that survives the utf8.Valid pre-gate and Decode, the transform errors or produces output ≠ input"* — not "the transform errors on every violation," which v1.0.1 demonstrably does not satisfy (surrogate pairs, precision loss) and does not need to. The RFC vector corpus plus the probe cases above are the slice-1 acceptance suite; the pin is by audit, not reputation (last release 2023); the `internal/jcs` wrapper keeps it swappable.
- **Producer encode:** stdlib `json.Marshal` of our fixed shapes through the same transform; caller strings UTF-8-validated first. Producer bytes and consumer verification cannot disagree.

### 6.3 Binding selection to release

`Index` records the SHA-256 of its input bytes; `Resolved` carries it; `FetchFiles` requires equality with `rel.Digest()` (`ErrSelectionMismatch`). Digest identity, not pointer identity: independently parsed copies of the same canonical index interoperate, and the binding survives serialization. This closes the spec's chain: fetch+validate → select from that index → retrieve those descriptors from that repository.

### 6.4 BigOCI integration: prerequisites satisfied by v0.2.0

**Upstream prerequisites: satisfied by bigoci v0.2.0 (released 2026-08-15).** All five asks from the upstream request round landed and were verified in source:

1. **ASCII case-insensitive media-type decoding** — `strings.EqualFold` at every decode comparison (`internal/manifest/decode.go` #55); encoder bytes unchanged, so the digest oracle holds.
2. **Identity coding on manifest and blob reads** (#58) — bigoci itself sends `Accept-Encoding: identity`, parses `Content-Encoding` as an RFC 9110 case-insensitive token list, refuses coded responses before reading the body, and carries the marker across manual redirect hops (`internal/oci/encoding.go`, `redirect.go` allow-list; coded-store-rejection and hop-preservation tests upstream). Token realms remain exempt upstream.
3. **`Client.PushByDigest(ctx, repo, src, opts...) (ocispec.Descriptor, error)`** (#57) — publishes the manifest by computed digest with **no tag write**; `repo` is a repository-only reference (registry/name, no tag or digest). Same split/hash/retry/ordering guarantees as `Push`.
4. **The `BigociExternalBase`/`BigociWrapExternal` structural seam is documented as a stable public contract** (#56; `options.go` `WithHTTPClient` doc + docs-site API reference).
5. **Wire re-hash of part bytes during upload** (#59) — parts are digest-verified as the wire consumes them (see §3.2 source-stability).

The capability gate is therefore: **pin `bigoci ≥ v0.2.0`** and keep the slice-5 interop fixtures green (case-varied media types; coded-response refusal on all paths; cross-host redirect under default verified mode). `Capabilities()` advertises `application/vnd.bigoci.file.v1` once those fixtures pass against the pinned version.

**Producer path.** `Multipart.Push` maps directly onto `Client.PushByDigest`: no tag is ever written for a file manifest; the release tag is written exactly once, by our own index PUT. Historical note: pre-v0.2.0 this was impossible through bigoci's public API (`Push` writes its manifest at the bound tag/digest, unknowable prospectively), and revisions 2–3 of this document specified a complete local BigOCI producer fallback (split rule, empty-config blob, canonical codec byte-identical to bigoci's, PUT-by-digest). That fallback is **retired, not needed** — kept in review history only. The ≥2-part imgoci profile check (plan <2 ⇒ standard form) still lives above the port.

Consumer-side pull delegation is unchanged: `repo@sha256:…` is a valid bigoci pull reference.

**Port shape:** `Multipart.Push(ctx, repo, path, partSize) (ocispec.Descriptor, error)` — repository-scoped, tag-free, path-typed; adapter implements it with `PushByDigest`.

### 6.5 Retry: two domains, never nested

bigoci exposes no public retry control and its private zero policy normalizes to four attempts, so injecting a one-attempt policy is impossible and wrapping bigoci calls would multiply whole-transfer attempts. `internal/retry` is the single loop for this repo's own adapters (manifest HTTP, go-oci-blob — where `RetryPolicy{}` genuinely means one attempt); the bigoci adapter is self-retrying and never wrapped. Unified `Progress.Retries` merges both domains (§3.3). If upstream later exposes retry control, collapsing to one domain is a small follow-up.

### 6.6 Identity-encoding invariant: scoped enforcement with honest provenance

The spec requires `Accept-Encoding: identity` and identity-only `Content-Encoding` on every manifest and blob GET. Enforcement is a `RoundTripper` decorator (`identityTransport`) whose applicability differs by how much provenance each path gives us:

1. **Our manifest client (registry transport):** the adapter sets the header and enforces the response rule on manifest/blob-path GETs (`/v2/…/manifests/…`, `/v2/…/blobs/…`); token-exchange and auxiliary requests are untouched — our `internal/auth` routes realm requests outside enforcement by construction. `Content-Encoding` is parsed as a comma-separated, ASCII-case-insensitive token list accepting only `identity`; rejected response bodies are closed.
2. **go-oci-blob:** its registry transport gets the path-scoped wrapper; its **storage transport** (`WithStorageTransport`) is enforced unconditionally — go-oci-blob's off-origin client carries only redirected blob traffic (auth is the caller's RoundTripper on the registry side), so "external means blob" is actually true there.
3. **bigoci (≥ v0.2.0): enforced natively upstream.** bigoci sends `Accept-Encoding: identity` on its own manifest/blob GETs, refuses coded responses before reading the body, and carries the marker across its manual redirect hops (#58) — the invariant no longer depends on any wrapper we inject. Revisions 2–3's marker-predicate mechanism and its preservation test are retired; review history retains them. If we inject a policy transport via `WithHTTPClient` for any other reason, it implements the documented-stable `BigociExternalBase`/`BigociWrapExternal` seam (#56) so bigoci's default verified mode can clone and guard the concrete base — `WithUnverifiedExternalTransport` is never implied.

**Tests:** gzipping reverse proxy fails manifest and blob fetches on all enforced paths (ours, go-oci-blob's, and — enforced upstream — bigoci's); cross-host signed-storage redirect succeeds under bigoci's default verified mode and fails if the second host applies a content coding; a token realm that compresses its responses keeps working.

### 6.7 Delegation boundaries (summary)

| Concern | Where | Why |
|---|---|---|
| Blob Exists/Push/Pull | `go-oci-blob` | Proven wire kernel; streaming digest verify at EOF. `RetryPolicy{}`, guarded redirects, identity enforcement per §6.6. |
| Multipart pull; multipart push (`PushByDigest`) | `bigoci ≥ v0.2.0` public API | Its reason to exist; internals unimported; owns its own retries; identity coding and wire re-hash enforced upstream (#58/#59). |
| Bounded stored-size read, assembled-file digest/size, strict decode, content verify | here | Spec layers no delegate provides. |
| Manifest/index GET/PUT | here (`internal/registry`) | Neither sibling exposes manifest endpoints. Rejected: oras-go/go-containerregistry — dependency trees for two verbs that fight original-byte discipline. |
| Auth | here (bigoci pattern cloned) | bigoci's is internal. |
| Identity enforcement | here (`identityTransport` + adapter scoping) | Provenance-aware invariant per egress path. |

### 6.8 Miscellaneous positions

- `Docker-Content-Digest`: ignored (spec-permitted).
- `content.size` as `int64` (cap exactly 2⁶³−1); manifest `size` as `int64` with the 2⁵³−1 bound in the validator.
- Validation seams exist because the conformance corpus is parsed-value only; `ParseIndex` always composes all three.
- No `Mount`/cross-repo promotion in v1 — release copying is out of spec scope.

## 7. Testing strategy

**Conformance fixtures as oracle.** `testdata/conformance/` synced from `spec/conformance/v1/{pass,fail}` pinned to a recorded spec commit; CI re-syncs and fails on drift. Every pass fixture → `Decode`+`Validate` succeeds (incl. `additional-members.json` with its boolean extension); every fail fixture → fails.

**Byte-level canonical fixtures (owned here).** Canonical twins of each pass fixture accepted by `ParseIndex`; rejections: pretty-printed, `1e2`, duplicate keys (raw and decoded-equal `"\u0061"`/`"a"`), unsorted keys, non-minimal escapes, invalid UTF-8 in values and keys, lone surrogates, canonical-bytes-but-wrong-order (rule 9 vs 10 separation). Extension-domain positives: canonical unknown members with booleans, nulls, nesting, negative/fractional numbers, canonical exponents. `internal/jcs` runs the full RFC 8785 vectors plus the §6.2 negative audit suite.

**Unit tests:** selector grammar; five-tuple UTF-8 sort edges; per-rule validator tables; decomp strictness (two gzip members, buffered trailing byte, xz padding, zstd skippable/concatenated/dictionary frames); `BoundedReader` excess-during-read abort and exact-limit probe semantics; resolve atomicity; form-selection fallback; capability validation; `ErrSelectionMismatch`; Publish reference-form rejection; destination preflight (duplicates, directories, symlinked-parent aliases, staging reservation).

**Functional (gate for "feature complete"), testcontainers zot + CNCF Distribution:**

- Round trips over {standard, bigoci} × {none, gzip, xz, zstd} × {single-role, `linux-netboot`, shared-digest multi-deliverable}.
- Adversarial: bit-flipped layer; truncated part; wrong-size descriptor; over-long layer stream; non-canonical index PUT; tag mutated between Fetch and FetchFiles; last-role verify failure ⇒ zero committed outputs; injected rename failure ⇒ committed-prefix semantics + retry-overwrites contract; `a`/`a.imgoci-stored` two-role suite; concurrent same-selection fetches (cache lock); pre-planted wrong-content cache entry; staging reuse round trip.
- Wire invariants: gzipping reverse proxy fails all enforced egress paths (bigoci's enforcement is upstream as of v0.2.0 — our fixture confirms it end-to-end); cross-host signed-storage redirect under bigoci default verified mode; compressing token realm unaffected.
- BigOCI gate (pin verification against v0.2.0): case-varied media-type interop fixture green before capabilities advertise BigOCI; `PushByDigest` writes no tag (tag list unchanged) and its descriptor round-trips through our consumer path and the bigoci CLI; graph-completeness pull of every blob including the empty config.
- Auth: htpasswd static creds; anonymous bearer.

## 8. Delivery plan (agile, vertical slices)

**Slice 0 — rename pass** (hours): module → `github.com/imgoci/go`, root `package imgoci`, CLI to `cli/`, CI green.

**Slice 1 — core increment (offline):** `internal/jcs` (audit per §6.2, pin), `internal/index`, public `ParseIndex`/`Index`/`List`/`Resolve`/`Capabilities`; conformance corpus + byte-level suite green. Independently useful; the foundation everything trusts.

**Slice 2 — first user vertical (consumer, standard form):** `internal/auth` (anonymous + static), `internal/registry` GET + identity scoping, `decomp` (`none`+`gzip`) with `BoundedReader`, destination planner + per-call staging, `transfer.Fetch`/`FetchFiles`, `Client.Fetch`/`Resolve`. Functional test against a production-representative fixture: canonical bytes, digest addressing, identity enforcement, stored-size bound, tag mutation after fetch.

**Slice 3 — producer, standard form:** `registry` PUT, go-oci-blob wiring with re-verifying upload reader, `transfer.Publish` with the tag-only reference contract. Round trip becomes self-hosting.

**Slice 4 — full compression:** xz + zstd strict decoders; adversarial suite; zstd frame-inspection spike resolved.

**Slice 5 — BigOCI:** upstream gates cleared by bigoci v0.2.0 (§6.4); remaining work is ours. Pin `bigoci v0.2.0`, build `internal/multipart` on `Pull`/`PushByDigest`, profile reader, stored cache with locking, ≥2-part rule, and the pin-verification interop fixtures (§7). Capabilities advertise BigOCI only when those fixtures are green.

**Slice 6 — polish:** Docker credential store, unified progress end-to-end, reference CLI, mkdocs, first `v0.x` release plumbing.

Each slice ends with its functional tests passing — nothing is "done" on unit tests alone.

## 9. Open questions and risks

1. **Spec is a draft.** Churn localized by the pure-core/adapter split; implemented revision pinned in docs; no `v1.0.0` before the spec promotes.
2. **zstd/xz strictness mechanics.** Skippable-frame/trailing rejection with `klauspost/compress` likely needs frame-header inspection; `ulikunitz/xz` padding behavior needs verification. Slice 4 spike; the `decomp` contract is fixed regardless.
3. **JCS pin.** `gowebpki/jcs` v1.0.1 pre-audited 2026-08-14 (§6.2): usable behind the pre-gate + byte-compare; fork-and-fix of the reference port is the floor if the RFC vector corpus fails at slice 1. **Tracked successor:** Go 1.26's `encoding/json/jsontext` (behind `GOEXPERIMENT=jsonv2`) ships `Value.Canonicalize` implementing RFC 8785 with I-JSON-strict defaults (rejects invalid UTF-8 and duplicate names natively). Unusable for v1 — experimental, outside the Go 1 compatibility promise, and every downstream builder of this library would need the experiment flag — but once json/v2 lands in stdlib proper it replaces the dependency, the pre-gate, and the dup-scan in one move. Revisit at each Go minor release.
4. ~~Upstream bigoci sequencing.~~ **Resolved 2026-08-15:** all five asks shipped in bigoci v0.2.0 (#55 case-insensitive decode, #58 identity coding incl. redirect hops, #57 `PushByDigest`, #56 seam docs, #59 wire re-hash) and were verified in source. The local producer fallback and the marker-predicate mechanism are retired; slice 5 reduces to pinning v0.2.0 and passing the interop fixtures.
5. **Structural-seam coupling.** `BigociExternalBase`/`BigociWrapExternal` is now a documented stable contract (#56). Residual risk is ordinary semver trust; the interop test still guards it. Only relevant if we inject a policy transport at all — bigoci no longer needs one for identity.
6. **Stored-cache retention policy.** Retained on failure, removed on commit is the v1 rule; a size-bounded cache or explicit `Clean` API may be wanted once real usage shows retention patterns.
7. **Windows staging semantics.** The secure-open/no-follow and close-before-rename patterns are specified from bigoci's precedent; exact Windows behavior (ownership checks, lock semantics) is verified at implementation time, mirroring bigoci's platform splits.
8. **Shared auth extraction.** Duplicates bigoci's stack; extract on a third consumer.
9. **Streaming consumer output; producer-side compression convenience.** Both deliberately out of v1; additive later.
10. **Fixture sync mechanism.** Script-copy pinned to a spec commit; revisit when the spec tags releases.
11. **Multi-architecture publish ergonomics.** `Selectors []Selector` convenience awaits real-use feedback.
12. **Commit-phase failure window.** Per-file-atomic sequential commit with committed-prefix semantics and retry-overwrites-all contract (§3.2); a marker-file protocol is possible later if callers need better.

## Appendix: review history

Three adversarial review rounds shaped this document.

**Post-review update (2026-08-15):** the five upstream asks were filed with bigoci and all shipped the same day in v0.2.0 (#55 casefold decode, #56 seam docs, #57 `PushByDigest`, #58 identity coding, #59 upload wire-verify). §§3.2, 6.4, 6.6, 6.7, 7, 8 (slice 5), and 9 were updated accordingly: the local BigOCI producer fallback and the §6.6 marker-predicate mechanism are retired, and BigOCI support now gates only on pinning v0.2.0 and passing imgoci's interop fixtures. Round-2/3 text below describes the pre-v0.2.0 state.

- **Round 1 (7 blockers, 7 suggestions):** replaced a restricted hand-rolled JCS verifier with a full-domain proven transform; bound `Resolved` to `Release` by canonical index digest; withdrew an unimplementable tag-bound `bigoci.Push` producer flow in favor of upstream digest publication or a local BigOCI producer; gated BigOCI capability on upstream conformance instead of "documented errors"; added the stored-layer size check and identity-encoding enforcement; replaced the impossible single-retry-loop claim with two explicit non-nesting domains; made the capability set an explicit validated value. Adopted: deep-copy immutability, stage-then-commit, strict-gzip buffering, honest dependency accounting, stored-file reuse-with-reverify, media-type helper split.
- **Round 2 (5 blockers, 7 suggestions):** made the identity wrapper implement bigoci's structural observer seam (verified at `endpoint.go:77-193`) so secure off-origin redirects keep working; added a mandatory `utf8.Valid` gate and hard audit criteria for the JCS dependency; specified the local BigOCI producer to a complete reachable graph (empty config, bounds, ordering, oracles); replaced suffix-derived staging paths with preflighted plans and private staging; required tag-only Publish references. Adopted: commit-failure semantics, bounded stored reads during transfer, concrete `Source`/`Dest` structs, precise dependency list, scoped identity enforcement, unified retry progress, full destination preflight.
- **Round 3 (3 blockers, 6 suggestions):** made bigoci identity enforcement provenance-honest (bigoci's external client also carries token-realm traffic → marker predicate with a preservation test, else upstream prerequisite); rebuilt staging as per-call unique workspaces plus a content-addressed, locked, securely-reopened stored cache keyed by the full stored digest; downgraded the multipart mutable-source claim to an honest source-immutability precondition with per-path defense-in-depth (wire re-hash on standard and local-fallback paths only). Adopted: cross-filesystem `ToFiles` allowed, narrow staging reservation, cloned caller maps, latest-absolute retry merge, exact-limit `BoundedReader` probe semantics, bigoci-grade staging hardening.
