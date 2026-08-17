---
id: 005
title: Release-readiness functional test plan
started: 2026-08-16
---

## 2026-08-16 12:58 — Kickoff

Goal for the session: spawn a planner agent to compose a manual, real-world
functional testing plan for the public surfaces of `imgoci/go`. The plan must
demonstrate that the project is fully ready to be released and that it delivers
on every promise it makes. The final plan document lands in this session folder
and is presented to the user for review.

Current state of the world:

- `master` is at `b4b5921` (PR #15 merged); PLAN slices 0–6 and the
  manual-release documentation follow-up are all on `master`.
- No release or tag exists. Release Please PR #9 proposes `release 0.1.0` with
  `initial-version: 0.1.0` durably configured. The v1 guard stands while the
  spec is draft.
- Session 004 already ran a manual release rehearsal: 101 scenarios through 24
  external probe programs against zot v2.1.20, no release-blocking defect, two
  documentation gaps fixed in PR #15.
- Known coverage gaps carried forward from 004: TLS/custom CAs, cross-host blob
  redirects, external Docker credential helpers, Bearer/OAuth token exchange,
  publish-side retry injection, multi-GiB BigOCI payloads, concurrent same-tag
  publication, and forced commit-phase partial filesystem failure.
- Public surfaces to cover: root `package imgoci` (module
  `github.com/imgoci/go`), the private `cli/` submodule, and the user-facing
  `docs/` set.

Plan: prime this session, then dispatch a planner agent with full repo and
prior-session context to produce the functional test plan document. Store it as
`.journal/005/FUNCTIONAL_TEST_PLAN.md` and present it for review.

## 2026-08-16 13:25 — Plan delivered

Correction to the kickoff entry: `master` is at `0b4be41` ("docs: remove plan
references and over-dense comments (#16)"), not `b4b5921`. PR #16 landed after
session 004's follow-up. The plan is written against `0b4be41`.

Planner agent `ReleaseTestPlanner` produced
`.journal/005/FUNCTIONAL_TEST_PLAN.md` (998 lines): 24 promises, 28 scenarios,
8 phases, verdict criteria, residual risk, execution notes.

Groundedness spot-checks I ran against the working tree before accepting it:

- Exported root surface in the plan matches `go doc -short .` exactly (types,
  functions, options, `Client`/`Index`/`Release`/`Resolved` methods, nine
  sentinels).
- CLI exit mapping 0-11/130/143 matches the `exit*` constants and
  `sentinelExits()` in `cli/run.go`.
- Per-command flag sets match `commonFlags.register`, `registerWorkers`,
  `registerProgress`, `queryFlags.registerList`, and `registerResolve`
  (`resolve` has no workers/progress; `fetch` and `publish` do).
- `imgoci version` line matches `versionLine` in `cli/run.go:100`.
- Progress line format matches `cli/progress.go:148`.
- Empty-command stderr text matches `cli/run.go:143`.

All eight coverage gaps from `TECH_NOTES.md` have concrete scenarios: NET-01
(TLS/custom CA), NET-02 (cross-host redirect), AUTH-01 (external credential
helper), AUTH-02/AUTH-03 (Bearer/OAuth, bare 401), FAIL-01 (publish-side retry
injection), BIG-02 (multi-GiB BigOCI, with a stated 15 GiB budget exception),
RACE-01 (concurrent same-tag publication), FAIL-02 (forced commit-phase partial
filesystem failure). The plan declares CLI exit `10` unreachable through the
shipped grammar and records it as residual risk rather than faking it.

Next: user review of the plan, then execute it.

## 2026-08-16 14:05 — Phase 1 executed

Plan approved by the user; Phase 1 run with three `functional-tester` agents
(cap of 4 respected): `CM01Tester`, `DOC01Tester`, `DOC02Tester`. I bootstrapped
the shared environment inline first (evidence root, tool pins, image digests,
`$FT/consumer-local`, `$FT/bin/imgoci`, shared `registry:2` on 127.0.0.1:5100).
Evidence root: `/tmp/imgoci-functional-0b4be41/evidence`.

Verdicts: **no release blockers. Phase 1 stop rule not triggered.**

- `CM-01` PASS. External module `ft.local/imgoci-consumer` compiled and ran:
  exit 0, stdout byte-exact `consumer-smoke ok\n` (18 bytes), stderr 0 bytes.
  Every symbol the plan lists exists under import identifier `imgoci`; the
  plan's surface inventory matched reality with zero delta (22 types, 15
  functions, 9 sentinels). Build graph is 250 packages / 10 non-stdlib modules,
  0 `imgoci/go/cli` packages, 0 test-only deps. `go list -m all` = 64 modules.
- `DOC-01` NON-BLOCKING FINDING. Full tutorial walkthrough verified
  byte-for-byte on the substituted port: version line exact, `/v2/` 200,
  publish stdout one 72-byte digest line with all diagnostics on stderr, list
  stdout tab-exact, resolve 9 fields with content digest == `shasum disk.img`
  and size 1048576, fetch stdout empty, `cmp` identical, digest-pinned list
  immediate, `--rm` container gone after stop.
- `DOC-02` NON-BLOCKING FINDING. Strict docs build exit 0; all 10 nav targets
  200 with expected titles; 777 anchors checked, 0 failures, 0 dangling;
  copied `resolve-deliverables` and `verify-a-release` examples ran from an
  external module (`verified against sha256:61e6c55f…`); contract audit matched
  source on decode ceilings, per-command CLI flags, exit codes 0-11/130/143,
  capability defaults, retry attempts (4), 4096-part cap, 512 MiB part size,
  and the "CLI is never released" claim.

Findings raised (both non-blocking under the plan's stated rules):

1. `DOC-01-F1` — the tutorial's port 5000 is owned by macOS ControlCenter
   (AirPlay Receiver). Sharper than the plan predicted: `docker run -p 5000:5000`
   *succeeds* (no "already allocated"), so the failure surfaces one step later as
   the documented `curl` returning `403` with `Server: AirTunes/940.23.1` and
   exit 22. Walkthrough passes with 5000→5500 substituted consistently.
   Suggested fix: publish the tutorial on a less contested port, or warn that
   the bind can silently succeed while curl answers from AirTunes.
2. `DOC-02-F1` — `docs/docs/index.md` names `spec v1 draft, 2026-08-11` but
   omits spec commit `5b95710…`. Verified independently: 9 of 10 pages carry the
   commit; only the landing page does not.
   `DOC-02-F2` is the already-classified sparse `ReleaseSpec` name/version
   grammar, deferred to `ADV-04`.

Observations below the finding threshold: the tutorial's "What you need" list
omits `cmp`, which its final step uses.

Process notes:

- I dirtied `$REPO/go.mod` during bootstrap by running `mise exec -C "$REPO" --
  go mod edit`, which executes in the repo directory. Reverted immediately with
  `git checkout -- go.mod`; tree verified clean before the agents started and
  after they finished. Agents were told to use the pinned Go binary by absolute
  path (`/Users/josh/.local/share/mise/installs/go/1.26.5/bin/go`) and never
  `mise exec -C $REPO`. No agent mutated the repository.
- `DOC-02` used its own module `$FT/consumer-docs` and published its own release
  to `127.0.0.1:5100/docs/example:v1` instead of sharing `$FT/consumer-local`
  and DOC-01's torn-down registry. Both deviations were recorded; they avoid a
  `go.mod` race and a cross-agent teardown dependency.
- DOC-01's literal `go build` used the reader's plain `go1.26.4` (goenv shim),
  which `GOTOOLCHAIN=auto` upgraded to 1.26.5 for the build; buildinfo confirms
  `go1.26.5`. No pinned-toolchain substitution was needed.
- Host artifact: global git config rewrites `https://github.com/` to SSH, so the
  literal HTTPS clone went over SSH. Anonymous HTTPS reachability was proven
  separately with a neutralized git config.
- Shared `imgoci-ft-dist` registry left running on 127.0.0.1:5100 for later
  phases. Port-8000 docs server stopped; no stray `imgoci-zot` container.

Next: Phase 2 (core library contracts, `LIB-01`..`LIB-04`) on the user's go.

## 2026-08-16 14:45 — Phase 2 executed

Four `functional-tester` agents, one scenario each, at the cap: `LIB01Tester`,
`LIB02Tester`, `LIB03Tester`, `LIB04Tester`. Per-scenario throwaway modules
(`$FT/consumer-lib0{1..4}`), registry prefixes (`ft/core`, `ft/progress`), and
counting-shim ports (5110, 5111) to keep four concurrent runs from racing.

Verdicts: **`LIB-01` PASS, `LIB-02` PASS, `LIB-03` PASS, `LIB-04` PASS. No
blockers, no new findings. Phase 2 stop rule not triggered.**

- `LIB-01` (34+8 assertions, 0 failures). Export set matches the inventory with
  zero delta: 22 types, 15 functions, 16 methods, 9 sentinels; `Option` is a
  func type, `FetchOption`/`PublishOption` are sealed interfaces. Every exported
  symbol and every exported struct field carries godoc (D1) — mechanical scan
  found zero undocumented fields. Identity proof: `Index.Digest()` ==
  `sha256:e475b1ed…` == stdlib `sha256.Sum256(raw)` of the exact 870 input bytes;
  the re-indented copy of the same JSON value hashes differently and
  `ParseIndex` rejects it with `invalid index: spec §6 rule 10: jcs: input is
  not RFC 8785 canonical` matching `ErrInvalidIndex`. All accessors copy on
  return; two independent parses are equal by digest, distinct by pointer.
- `LIB-02` (99 cases, 259 assertions, 0 failures, fully offline). Three-level
  sort order, capability-independent `List`, empty no-match with nil error,
  default roles (`incus-vm` → `disk,metadata`; `linux-netboot-complete` → all;
  unknown representation → all), and the documented precedence all hold. 33
  error-producing cases carry a full nine-sentinel matrix: exactly four rows
  have any sentinel true, all four `ErrUnsupportedType` from capability
  exhaustion; every other failure (no deliverable, absent role, no accepted
  compression, `NewCapabilities` rejections) is deliberately unclassified. No
  failure returned a partial `Resolved` — selection pointer nil in every case.
  The Unicode trap was proven *both* ways: `strings.EqualFold` does fold U+017F→s
  and U+212A→k (so the ban is load-bearing), while `EqualMediaType` rejects both
  look-alikes.
- `LIB-03` (real registry round trip through a counting shim on 5110). Publish
  returned `sha256:a80e4cb6…`; tag fetch and `ft/core@<digest>` fetch both agree;
  entry content digests equal the source hashes (262144 / 393216 bytes). `ToDir`
  and both `ToFiles` parents produced byte-identical files (`cmp` + sha256), all
  outputs mode `0600`, and the unrelated pre-existing file was unchanged
  (hash, size, mode, mtime). Post-construction mutation of the `ToFiles` map did
  not move the outputs — the constructor clones. **24 of 24 preflight rejections
  sent zero requests** (shim line count 35→35), and five rejected publications
  aimed at fresh tag names left the tag list at `["smoke","v1"]`.
- `LIB-04` (progress and options through a counting shim on 5111). Callback
  serialization guard never exceeded 1 on publish, fetch, or any of four
  concurrent `FetchFiles`; counters non-decreasing; totals fixed once
  established; publish emitted `hashing → upload×3 → exactly one index`; fetch
  emitted `staging×3 → exactly one commit` with
  `{fetch,commit,2/2 files,8388608/8388608 bytes}`. Four concurrent
  `FetchFiles` from one `Client`/`Release`/`Resolved`: 8/8 outputs hash-match,
  `-race` build exit 0 with 0-byte stderr. Default worker count proven
  behaviorally with a 300 ms-per-blob shim delay: omitted `WithWorkers` → max 4
  concurrent blob GETs, controls at 2 and 1. `WithWorkers(0)`/`(-1)` fail with
  `worker count must be positive, got 0/-1`, zero shim traffic, no sentinel
  match, no tag written. `WithProgress(nil)` and `WithHTTPClient(nil)` ignored.

Observations (no defect, recorded for owner visibility):

1. `LIB-03/N1` — the plan's `LIB-03` Expected line compresses "invalid publish
   reference/spec cases match `ErrInvalidSpec`". Reality, verified against
   `reference.go:26-31` godoc: the three *semantic* reference rejections
   (digest-only, tag+digest, name-only) wrap `ErrInvalidSpec`; the four
   *grammar-malformed* references return descriptive unclassified parse errors
   because "a malformed reference is a caller error … not `ErrInvalidSpec` (that
   sentinel is producer-only)". Behavior matches the shipped contract; the plan
   sentence is the imprecise part. All eight fail closed with delta 0 and write
   no tag. Plan text left unedited since the user reviewed it; correct it if this
   plan is ever re-run.
2. `LIB-03/N2` — the successful `v1` publish shows a shim delta of only 6
   because the shim end-to-end smoke publish had already stored the same content
   blobs, so registry blob dedup answered the HEADs. Count is not a defect.
3. `LIB-04` — `P-RETRY-01` is only partly exercised here: no failure was
   injected, so `Progress.Retries` stayed 0 throughout. The positive
   retry-accounting case belongs to `FAIL-01`.

Process notes:

- `git -C $REPO status --porcelain` empty before and after every scenario; HEAD
  still `0b4be41`. No agent mutated the repository this phase.
- `registry:2` uses monolithic blob PUT (no PATCH), so the shim smoke recorded
  3 HEAD + 3 POST + 6 PUT for a two-file publish — worth knowing when reading
  later phases' request counts.
- `LIB-03` added a `cmd/rejtag` probe so "no tag on failed validation" is proven
  against five previously nonexistent tag names rather than against `:v1`, which
  already existed. Additive; no step narrowed.
- Shared `imgoci-ft-dist` still running on 127.0.0.1:5100; shim ports 5110/5111
  released.

Next: Phase 3 (integrity and adversarial behavior, `ADV-01`..`ADV-04`).

## 2026-08-16 15:20 — Phase 3 executed

Four `functional-tester` agents at the cap: `ADV01Tester`, `ADV02Tester`,
`ADV03Tester`, `ADV04Tester`. Own modules `$FT/consumer-adv0{1..4}`, prefixes
`ft/integrity`, `ft/codec`, `ft/spec`, shim ports 5120/5130/5140.

Verdicts: **`ADV-01` PASS, `ADV-02` PASS, `ADV-03` PASS, `ADV-04` NON-BLOCKING
FINDING. No blockers. Phase 3 stop rule not triggered.**

- `ADV-01` — all 24 canonical fixtures driven through `ParseIndex` from an
  external module. 13 `pass/` accepted, each `Index.Digest()` equal to the
  independently computed SHA-256 of the exact input bytes; 11 `fail/` returned a
  nil index matching **only** `ErrInvalidIndex`. Rule 9 is textually distinct
  from the rule 10 canonical-bytes check: `spec §6 rule 9: manifests must be
  ordered by architecture, target, representation, role, and compression in
  ascending UTF-8 byte order` vs `spec §6 rule 10: jcs: input is not RFC 8785
  canonical`. No normalized identity leaks: rejected documents return no index
  at all, and the whitespace-stripped derivative of `pretty-printed.json`
  produces its own digest, never the pretty document's.
- `ADV-02` — the integrity centerpiece, all clean:
  - Tag mutation cannot redirect an in-hand `Release`. A published as
    `sha256:ed5c432a…`, B republished over `ft/integrity:v1` as
    `sha256:8ee13396…`; the already-fetched release A still fetched A's bytes
    (`sha256:9cc5b47a…`), the re-fetched tag yielded B, and both explicit digest
    references yielded their own bytes.
  - `corrupt-blob` shim verified as equipment first: `cmp -l` showed exactly one
    differing byte at offset 100000, `Content-Length` preserved,
    `Docker-Content-Digest` dropped, untargeted blobs byte-identical.
  - Flipped `none` blob → `ErrDigestMismatch` only, destination empty.
  - **No silent fallback**, proven from the request sequence: corrupting the
    selected gzip alternative while a valid `none` alternative existed gave
    `decode: role disk: gzip: decode: gzip: invalid checksum` (`ErrDecode` only)
    and the fetch sequence ends at the corrupted blob — three requests total, no
    request for the `none` alternative's manifest or blob.
  - Two-role failure after the other role was fully retrieved: both pre-created
    marker files byte-identical before and after (23 B and 26 B), no partial
    commit, no extra file.
  - `Resolved` from A + `Release` B → `ErrSelectionMismatch` only, zero shim
    request delta.
- `ADV-03` — 14 self-confirmed codec fixtures (every zstd fixture validated with
  `zstd.Header.Decode` for `SingleSegment`/`WindowSize`/`DictionaryID`; xz
  dictionary size parsed out of the block header; gzip member count via
  `Multistream(false)`).
  - All four compressions round-trip byte-for-byte (1048576 B source; stored
    1048576 / 572267 / 601660 / 641656).
  - Six structural violations (two-member gzip, padded xz, trailing-garbage xz,
    concatenated zstd, skippable-first zstd, dictionary-required zstd) all fail
    with `ErrDecode` only and **zero** registry requests — not merely zero
    uploads.
  - Working-set limits reject on header inspection, measured with
    `/usr/bin/time -l` against a same-probe noop baseline: xz 64 MiB dict
    +131072 B; zstd 16 MiB window +180200 B; zstd 512 MiB window **+16408 B**
    against a declared 536870912 B working set. Nothing close to the hostile
    declared size is ever allocated.
  - Known finding re-check 1: the single-segment 9 MiB zstd frame still matches
    `ErrDecode` with +65536 B peak delta. Wording is `zstd: decode:
    decompressed size exceeds configured limit` — it no longer misdescribes a
    window, so the session-004 wording complaint is largely moot; still
    non-blocking either way.
  - Known finding re-check 2: `x-ft-brotli` fails closed with `decomp: unknown
    compression "x-ft-brotli": unsupported compression`, zero registry requests,
    all nine sentinels false, and `ft/codec` tags remain exactly the four valid
    ones. Non-blocking confirmed on the plan's stated condition.
- `ADV-04` — runtime grammar is **exactly** spec §5.1/§5.3: name is a 1–128-byte
  basic token `^[a-z0-9]+([._-][a-z0-9]+)*$`, version is 1–128 printable ASCII
  with no whitespace/control. 17 invalid cases all matched `ErrInvalidSpec`,
  showed shim delta 0, and created no tag (verified against fresh tag names —
  the final `ft/spec` tag list holds exactly the 10 accepted publishes). Both
  128-byte boundaries publish successfully (name `sha256:cb0f9d3c…`, version
  `sha256:6812f404…`); 129 bytes rejected on both.

Findings from Phase 3 (both non-blocking; both are documentation defects with
correct runtime behavior underneath, i.e. the session-004 PR #15 pattern):

1. `ADV-04-F1` — the name/version grammar is enforced but documented nowhere a
   public API user looks. Verified myself: `go doc ReleaseSpec` says only "Name
   is io.imgoci.name." / "Version is org.opencontainers.image.version.", and
   `grep 'a-z0-9\|basic token\|128'` over `docs/docs/reference/api.md` and
   `cli.md` returns nothing. The constraints live in
   `internal/index/decode.go:55-56` and `internal/index/validate.go:121,422`.
   Safe workaround: `Publish` fails deterministically pre-network with
   `ErrInvalidSpec`, so a producer cannot ship a bad artifact.
2. `ADV-04-F2` — three `testdata/canonical/README.md` `fail/` rows name the
   wrong rejection class, not one as session 004 recorded. Verified myself:
   - `unsorted-keys.json` contains `"schemaVersion"` **twice** (781 bytes,
     count = 2), so the duplicate-key scan fires first; README claims "Object
     keys not in RFC 8785 order (rule 10)".
   - `exponent-1e0.json` / `exponent-1e2.json` put `1e0`/`1e2` in the descriptor
     `size` position (offset 687), so the size-must-be-a-JSON-integer check
     fires; README claims rule 10 for both.
   All three still reject with `ErrInvalidIndex`, which is what keeps this
   non-blocking. `ADV-01` independently flagged the same three rows.

Candidate pre-release documentation PR (owner's call, not actioned): document
the name/version grammar on `ReleaseSpec` godoc + `reference/api.md`; correct
the three `testdata/canonical/README.md` rows; add the tutorial's port-5000
AirPlay caveat and the missing `cmp` prerequisite; add the spec commit to
`docs/docs/index.md`.

Process notes:

- `git -C $REPO status --porcelain` empty before and after all four scenarios;
  HEAD still `0b4be41`. No agent mutated the repository.
- Shared `imgoci-ft-dist` still Up on 127.0.0.1:5100; shim ports 5120/5130/5140
  released.
- Cross-checked by hand rather than trusting agent claims: the three README
  mislabels (raw bytes), the absent grammar documentation (godoc + grep), the
  no-fallback request sequence, the RSS deltas, and the `ft/codec` / `ft/spec`
  tag listings.

Next: Phase 4 (authentication, TLS, and transport boundaries — `NET-01`,
`NET-02`, `AUTH-01`, `AUTH-02`, `AUTH-03`). Five scenarios against a cap of 4,
so this one needs a split decision.

## 2026-08-16 16:15 — Phase 4 executed

User authorized a temporary lift above the 4-agent cap, so all five scenarios
ran concurrently: `NET01Tester`, `NET02Tester`, `AUTH01Tester`, `AUTH02Tester`,
`AUTH03Tester`. Own modules `$FT/consumer-{net01,net02,auth01,auth02,auth03}`;
ports 5443 (TLS registry), 5200/5201 (redirect/storage), 5400 (basic front),
5300/5301 (bearer front/token realm), 5401 (bare 401).

Verdicts: **`NET-01` PASS, `NET-02` NON-BLOCKING FINDING, `AUTH-01` PASS,
`AUTH-02` PASS, `AUTH-03` NON-BLOCKING FINDING (pre-classified). No blockers.
Phase 4 stop rule not triggered.** This phase closed four of the eight coverage
gaps carried since session 004: TLS/custom CAs, cross-host blob redirects,
external credential helpers, and Bearer/OAuth exchange.

- `NET-01` — real HTTPS with a one-day local CA (CA `D2:16:7B:E9…`, server
  `D6:30:81:2D…`, SAN `DNS:localhost, IP:127.0.0.1`, `openssl verify` OK).
  Controls both ways: `curl --cacert` → 200 with `ssl_verify_result=0`; bare
  `curl` → exit 60 `unable to get local issuer certificate`.
  - Default client fails closed: `tls: failed to verify certificate: x509:
    “localhost” certificate is not trusted`, all nine sentinels false.
  - **`WithUnverifiedExternalTransport()` does not weaken TLS** — its error is
    character-for-character identical to the default client's, same
    `*tls.CertificateVerificationError`. That is the security-critical
    assertion of the scenario and it held.
  - `WithPlainHTTP()` against the TLS socket fails (400), and nothing was even
    routed — zero status-400 records in the registry access log.
  - The custom-CA client (`x509.NewCertPool` + cloned `http.DefaultTransport` +
    `TLSClientConfig.RootCAs` via `WithHTTPClient`) published and fetched both a
    standard release and a **genuine 5-part** BigOCI release over HTTPS,
    byte-exact. 64/64 access-log records are HTTP/2.0 on the TLS-only listener —
    zero HTTP fallback.
- `NET-02` — cross-host redirect and identity coding:
  - Standard and **genuine 3-part** BigOCI releases both fetched byte-exact
    across a real `307` to a different host:port. Proof is set-theoretic: the
    set difference of {front `307` Locations} minus {storage-served 200 URLs} is
    empty, and the front served `resp_bytes=0` on all 10 blob GETs, so every
    blob body genuinely crossed the boundary.
  - `Accept-Encoding: identity` on 22/22 front GETs and 10/10 storage GETs —
    the header survives the cross-host hop.
  - Storage saw **zero** `Authorization` headers, including a re-run with
    `WithCredentials` configured. No off-origin credential.
  - gzip-coded storage fails before commit, zero final files; the only residue
    is a provably all-zero preallocated staging partial.
  - Opaque transport rejected at adapter construction (`opaque HTTP transport
    requires WithUnverifiedExternalTransport`) with front and storage deltas
    both 0; after opting in, identity works and **gzip is still rejected**.
- `AUTH-01` — external helper processes, the largest scenario this phase:
  - Valid helper credentials completed `Fetch` and `FetchFiles` byte-exact. 22
    lookups produced **22 distinct pids** — every lookup execs the named helper
    afresh; `New` itself performs no lookup.
  - Helper stdin key is exactly `127.0.0.1:5400` on all 16 non-hub calls; the
    Docker Hub logical host maps to the legacy key
    `https://index.docker.io/v1/`.
  - Empty `DOCKER_CONFIG`: helper count unchanged, request carries
    `authorization_scheme=none`, and a recording decoy named
    `docker-credential-osxkeychain` placed ahead of the real one stayed 0 bytes
    — **no default platform helper is ever run**. The request fails with
    `ErrUnauthorized`.
  - Wedged helper: background context fails at **10.001340708s** with `the
    credential helper did not answer within 10s`; a 250 ms caller deadline
    returns near 250 ms; no orphaned helper or `sleep` survived either case.
  - Identity-token-only config fails without anonymous downgrade, and the
    marker `FTMARKER-Q7X2-KRAKEN-9931` appears nowhere. Leak hunt covered
    `ft-secret`, `ft-token`, the marker, and the base64 of `ft-user:ft-secret`
    across evidence, harness, and shim logs: all `exit=1` (no match).
  - `config.json` hash and directory listing identical before and after; CLI
    `list` succeeded through the Basic front with no credential flag.
- `AUTH-02` — Bearer/OAuth exchange:
  - The very first registry request is the real manifest GET, anonymous
    (`auth_scheme=none`); there is no pre-auth `/v2/` ping.
  - Challenge realm/service/scope survive verbatim into the realm query
    (`scope=repository%3Aft%2Fauth%3Apull%2Cpush&service=ft-registry`).
  - gzip-coded OAuth JSON providing **only** `access_token` (never `token`) is
    accepted; registry retries carry `Bearer` — per-host tally `{none: 1,
    Bearer: 10}`, `Basic: 0`.
  - Token reuse proven numerically: realm hit count goes 0 → 1 and stays flat
    while **11 registry requests** complete inside `expires_in=300`. Same in
    the Basic-protected-realm phase.
  - Static Basic went to the realm only, never to the registry front.
- `AUTH-03` — bare 401 with no challenge, the pre-classified known finding:
  all nine sentinels false; detail is exactly `the registry refused the request
  without saying how to authenticate`; CLI exits `1` with a 218-byte stderr
  ending `imgoci: no sentinel matched (exit 1)` and 0-byte stdout. Fails closed
  and finitely: `Fetch` = exactly 1 attempt (not retried), the publish
  blob-existence probe = exactly 4 attempts with backoff. Hostile peer bytes
  (ESC, BEL, invalid UTF-8, newlines) render as literal ASCII escapes —
  verified byte-wise, zero control bytes in captured streams. A client
  configured with credentials still sent none to a challenge-less server.

New findings this phase (all non-blocking; none is a safety issue):

1. `NET-02-F1` / diagnostics flattening — on the **standard** blob path the
   identity-coding cause is rendered only as `registry request failed` at the
   top level; the real cause is intact under `errors.Unwrap` (`*url.Error` →
   `*registry.contentCodingError` = `the response is not identity coded`). The
   BigOCI path surfaces it correctly. `NET-01` hit the same shape from another
   direction: a TLS verification failure through `Publish` renders `after 4
   attempts: checking blob existence: registry request failed`. Both flatten at
   the `go-oci-blob v1.1.1` layer (`retryableError`/`requestError`), so the fix
   is likely upstream or in our wrapper. Operator impact: a gzipping proxy in
   front of standard blob storage is diagnosed as a generic request failure.
2. `NET-01-F1` — a successful BigOCI `FetchFiles` into a `ToDir` destination
   leaves an empty `<dest>/.imgoci-stage/stored/` behind (3/3 BigOCI runs, 0/3
   standard). Verified against source: `StoredCache.Remove` deletes entries on
   successful commit (hence 0 files), but `internal/file` contains no directory
   removal at all, so the two directories persist by design. The real gap is
   documentation — `grep -rn 'imgoci-stage' docs/docs/` returns **nothing**, so
   a consumer listing the destination sees an undocumented directory.
3. `AUTH-03` observation — a bare `401` is retried 4× on the publish
   blob-existence path (an authentication refusal is not transient), while
   `Fetch` does not retry it at all. Bounded and harmless, but inconsistent.
4. `NET-01-F2` observation, not a defect — on darwin the untrusted chain reads
   `x509: “localhost” certificate is not trusted` rather than Linux's
   `certificate signed by unknown authority`. Same condition
   (`*tls.CertificateVerificationError`, server logged `remote error: tls:
   unknown certificate authority`); the plan's expectation is met in substance.

Docs-PR candidate list now (owner's call, still not actioned): name/version
grammar on `ReleaseSpec` godoc + `reference/api.md`; three
`testdata/canonical/README.md` row corrections; tutorial port-5000 AirPlay
caveat and missing `cmp` prerequisite; spec commit on `docs/docs/index.md`;
document that a BigOCI `ToDir` destination retains an empty `.imgoci-stage`
directory.

Process notes:

- `git -C $REPO status --porcelain` empty before and after all five scenarios;
  HEAD still `0b4be41`.
- `imgoci-ft-tls` stopped and gone; ports 5200/5201/5300/5301/5400/5401/5443 all
  released; shared `imgoci-ft-dist` still Up on 5100.
- `NET-01`'s first probe exited 1 on its own output-listing helper (it tried to
  hash a directory, triggered by finding F1) *after* the BigOCI fetch had
  already returned nil and committed byte-exact output. Harness fault; reruns
  with a directory-aware scanner exit 0.
- `AUTH-02`'s leak hunt reports one match, in its own `smoke-curl.txt` — that is
  the token realm's own response body captured by the harness, not
  library-produced output.
- Verified by hand rather than trusting agent claims: the identity-coding error
  type and message (`internal/registry/identity.go:154`), the absence of any
  directory cleanup in `internal/file`, `StoredCache`'s documented
  entry-removal-on-commit rule, the absence of `imgoci-stage` from all shipped
  docs, every `result` file, container/port cleanup, and the leak-hunt outputs.

Next: Phase 5 (BigOCI and scale — `BIG-01`, `BIG-02`). `BIG-02` needs a real
15 GiB budget; host currently reports 223 GiB free on the data volume, so the
budgeted run is feasible and its owner-acceptance exception should not be
needed.

## 2026-08-16 16:55 — Phase 5 executed

Two `functional-tester` agents (back inside the 4 cap): `BIG01Tester`,
`BIG02Tester`. Modules `$FT/consumer-big0{1,2}`, prefixes `ft/big01` and
`ft/big`, shim port 5150 for BIG-01; BIG-02 talked directly to 127.0.0.1:5100 so
proxy overhead would not distort a 3 GiB transfer in each direction.

Verdicts: **`BIG-01` PASS (52/52 assertions), `BIG-02` PASS. No blockers. Phase
5 stop rule not triggered.** `BIG-02` ran for real — the plan's
owner-acceptance resource exception was not needed (host 222 GiB free, Docker
overlay 211 GiB).

- `BIG-01` — deterministic 34603008-byte source (fixed-seed xorshift64*,
  reproducibility proven by regenerate + `cmp`).
  - Three-part plan (`PartSize: 16 << 20`) published
    `sha256:e63ba054…5baf`; entry artifact type `application/vnd.bigoci.file.v1`;
    the raw file manifest fetched **by `FileEntry.Digest`** self-verifies
    (967 bytes hashing to `sha256:60dc1082…a791`) with no re-encoding.
  - Full consumer path (`Fetch`/`List`/`Resolve`/`FetchFiles`) returned
    byte-exact output; terminal snapshots were exactly one `{publish,index}` and
    one `{fetch,commit}`, each with `WireBytes: 34603008`, `Retries: 0`,
    `Fallbacks: 0`.
  - One-part plan (`PartSize: 64 << 20`) fell back to the **standard** artifact
    type with `Fallbacks == 1` exactly.
  - `StandardCapabilities()` resolution of the BigOCI-only release →
    `ErrUnsupportedType`; `Client.Resolve` with zero capabilities → success.
  - 4097-part plan (`PartSize: 1` over 4097 bytes) → `ErrInvalidSpec` with a
    **zero** request delta; the 4096-part contrast case published successfully.
  - I verified all of this independently against the live registry rather than
    trusting the report: `three` = 3 layers sized 16 MiB/16 MiB/1 MiB summing to
    34603008 with `io.bigoci.file.size` 34603008; `onepart` = artifact type
    `application/vnd.imgoci.file.v1`; `cap4096` = 4096 layers / 256 unique
    blobs; neither file-manifest digest appears in the tag list
    (`["three","onepart","cap4096"]`).
- `BIG-02` — the real multi-GiB run, and the last scale gap from session 004:
  - Source exactly `3221225472` bytes via the plan's `mkfile -n 3g`.
  - Publish (`PartSize: 256 << 20`, `WithWorkers(2)`): exit 0, wall 4.69 s,
    **peak memory footprint 10322496 B (9.84 MiB = 0.32% of the file)**.
  - Fetch: exit 0, wall 5.19 s, **peak footprint 2195840 B (2.09 MiB =
    0.068%)**. Maximum RSS never exceeded 21.3 MiB in any run. Streaming is
    proven decisively; nothing approaches buffering.
  - Raw file manifest self-verifies, 12 layers of `268435456` each,
    `io.bigoci.file.size` = 3221225472, `io.bigoci.file.digest` = the source
    SHA-256. Output was exactly 3221225472 bytes, `shasum` equal to the source,
    `cmp` exit 0.
  - **The agent caught a real flaw in the plan's own setup and handled it
    correctly.** The mandated `mkfile -n 3g` source is all zeros, so all 12 part
    digests collide and the publish path legitimately uploads one 256 MiB blob,
    reporting `WireBytes: 268435456` — which cannot demonstrate GiB-scale
    publish traffic. It ran the mandated case unmodified and reported it first,
    then added a supplementary `ft/big:3g-distinct` run with a unique 21-byte
    marker at each part offset: 12 **unique** part digests, publish and fetch
    both `WireBytes: 3221225472`, `Retries: 0`, `Fallbacks: 0`, output
    byte-identical, peak footprint 2.09 MiB in both directions. I verified the
    distinct manifest myself: 12 layers, 12 unique digests, 268435456 each.
  - Registry storage grew 30408 KiB → 3508684 KiB (+3.32 GiB), confirming real
    bytes moved. Local 3 GiB artifacts were cleaned; registry contents left.

Findings this phase: none new. Two recurrences and two observations:

1. The known empty `<dest>/.imgoci-stage/stored/` directory pair reappeared on
   every BigOCI `ToDir` fetch (`BIG-01` 1/1, `BIG-02` 2/2). Both agents
   confirmed `find <dest>/.imgoci-stage -type f` returns nothing — zero cache
   entries, zero partial content. Correctly recorded as the existing Phase 4
   non-blocking documentation gap, not re-escalated.
2. `BIG-01-O1` — write-path requests (blob POST/PUT, 2 of 6 manifest PUTs) carry
   Go's default `Accept-Encoding: gzip`, and write-path existence HEADs carry
   none, while **every** read-path request carried `identity`: 11 GET manifest,
   4100 GET blob, 2 read-path HEAD. `P-WIRE-01` constrains manifest/blob GET
   responses, so this is correct; recorded so a future reader does not misread
   the log.
3. `BIG-01-O2` / `BIG-02-F1` — identical parts deduplicate, so `WireBytes`
   reflects unique blobs actually transferred (4096 one-byte parts → 256 unique
   blobs → `WireBytes: 256`). Correct content-addressed behavior; noted so a
   cheap contrast case is not mistaken for under-transfer.
4. `BIG-01` incidentally re-proved `ADV-04`'s grammar finding from a different
   direction: its first publish attempt used `ReleaseSpec.Name` with uppercase
   and was rejected with `invalid spec: spec §6 rule 3: io.imgoci.name must be a
   basic token` after **zero** registry requests. The agent had to discover the
   grammar by hitting it, which is exactly the documentation gap `ADV-04-F1`
   describes.

Coverage-gap status after Phase 5 — five of the eight session-004 gaps are now
closed: TLS/custom CAs, cross-host blob redirects, external credential helpers,
Bearer/OAuth exchange, and multi-GiB BigOCI payloads. Remaining three all land
in Phase 6: publish-side retry injection (`FAIL-01`), concurrent same-tag
publication (`RACE-01`), and forced commit-phase partial filesystem failure
(`FAIL-02`).

Process notes:

- `git -C $REPO status --porcelain` empty before and after both scenarios; HEAD
  still `0b4be41`.
- Shim port 5150 released; shared `imgoci-ft-dist` still Up on 5100 with ~3.3
  GiB of `ft/big` content retained for now (clean up at campaign end, not
  needed by Phase 6).
- Verified by hand: `ft/big01` and `ft/big` tag lists, the three-part layer
  sizes and total, the standard artifact type on the fallback publish, the
  4096-layer/256-unique-blob cap case, the 12-unique-part distinct manifest, and
  that no file-manifest digest is ever a tag.

Next: Phase 6 (failure injection and concurrency — `FAIL-01`, `RACE-01`,
`FAIL-02`), which closes the last three coverage gaps.

## 2026-08-16 17:35 — Phase 6 executed

Three `functional-tester` agents: `FAIL01Tester`, `RACE01Tester`, `FAIL02Tester`.
Modules `$FT/consumer-{fail01,race01,fail02}`, prefixes `ft/retry`,
`ft/retry-exhaust`, `ft/retry-big`, `ft/race`, `ft/commit`, shim ports
5160/5161/5162.

Verdicts: **`FAIL-01` PASS, `RACE-01` PASS, `FAIL-02` PASS. No blockers. Phase 6
stop rule not triggered.** **All eight session-004 coverage gaps are now
closed**, and six phases have produced zero release blockers.

- `FAIL-01` — publish-side retry injection through a `retry-put` shim that logs
  every request body's SHA-256:
  - Success case (first 2 manifest PUTs → `503`): the injected PUT shows
    **exactly three attempts** `[503, 503, 201]` with **one** body hash across
    all three (`sha256:1d03aeb0…128ea`, 425 bytes each). Publish succeeded; the
    terminal `{publish,index}` snapshot reported `Retries: 2`; the blob was
    uploaded once and `WireBytes` stayed 1048576 across both retry snapshots.
    Direct fetch from 5100 was byte-exact.
  - Exhaustion case (first 4 forced): **exactly four attempts**
    `[503, 503, 503, 503]`, identical bodies, publish failed, **no snapshot ever
    had `Phase: "index"`** (last was `{publish,upload}` with `Retries: 3`), and
    `Fetch` matched `ErrNotFound`. I verified the strongest form of this myself:
    the `ft/retry-exhaust` **repository does not exist at all**
    (`NAME_UNKNOWN`) — a failed publish left no tag and no repo.
  - BigOCI case (4 × 256 KiB parts through the same injecting shim): no nested
    whole-BigOCI retry; parts uploaded once; `Retries: 2` reported once for the
    single retried manifest PUT. The two retry domains stayed unnested exactly
    as `explanation/architecture.md` promises.
- `RACE-01` — concurrent same-tag publication, one `Client`, two goroutines on a
  closed-channel barrier, **40 iterations total** (20 plain + 20 under `-race`):
  - 80/80 returned digests matched `^sha256:[0-9a-f]{64}$` and were globally
    unique; **zero** `Publish` calls returned an error.
  - Every iteration genuinely overlapped in wall clock (136–228 ms).
  - The final tag was **exactly one publisher's digest in 40/40** —
    `wins_other = 0`. Split was A 12 / B 8, then A 10 / B 10. Never a third
    digest, never a hybrid: the winning index's `Name`+`Version` always matched
    exactly one side's spec and always agreed with the digest-based winner, and
    the fetched destination held exactly that side's two filenames.
  - `-race` run clean: no data race, no panic, no goroutine dump.
  - I verified the surviving tag independently: `ft/race:current` →
    `sha256:96e8ee41…fd65`, identity `ft-race-a` / `2.20.2199+a`, 2 entries, both
    file manifests `200`, all their blobs `200`. No dangling reference.
- `FAIL-02` — forced commit-phase partial filesystem failure, the most delicate
  scenario in the campaign:
  - Canonical role order was read back from the registry's own index bytes, not
    assumed: producer input was **metadata-first**, and the stored index is
    position 0 `disk`, position 1 `metadata`. I re-verified this against the
    live registry.
  - The fault landed exactly in the commit window. Triggering snapshot:
    `{fetch, staging, total_files: 2, completed_files: 2, total_bytes: 1048760,
    completed_bytes: 1048760}` → `chmod(parent B, 0500)`. **Zero** commit-phase
    snapshots on the failing call.
  - The error is exactly the promised shape: `commit failed; committed roles
    [disk]; failing role "metadata": file: commit role "metadata": rename …:
    permission denied`, with unwrap chain `*fmt.wrapError` → `*file.CommitError`
    → `*os.LinkError{rename}` → `syscall.EACCES(13)`. All nine imgoci sentinels
    false; `errors.Is(err, fs.ErrPermission)` true.
  - Filesystem state captured **before** restoring permissions: `disk` final held
    verified published bytes; `metadata` final still held its old 60-byte marker;
    neither final held partial or unverified content.
  - After restoring `0700`, the `disk` final was **deliberately corrupted** and
    the fetch retried: the retry restaged and recommitted **both** roles,
    replacing the corrupted disk with correct bytes — proving the retry does not
    trust an existing final — and produced exactly one terminal `{fetch,commit}`
    snapshot. 14/14 in-probe assertions true.

Findings this phase: none new. Notable observations:

1. `FAIL-02-N1` sharpens the known empty-`.imgoci-stage` finding with a precise
   mechanism: `Plan.Cleanup` successfully removed the staged file and the
   per-call workspace (both inside the still-writable `.imgoci-stage`), and only
   `os.Remove(parent-b/.imgoci-stage)` failed — because parent B was `0500` at
   that instant. No file remained, and the empty directory disappeared on the
   successful retry. Best-effort cleanup behaving correctly under an injected
   fault, not a leak.
2. `FAIL-01` observation — every injected `503` carried `Retry-After: 0` and the
   client did **not** honor it, applying its own jittered backoff instead
   (measured gaps: 601.5/1797.5 ms; 447.4/305.9/1165.8 ms; 724.4/985.8 ms). The
   B-case gaps are not monotonically increasing, i.e. jittered rather than
   strictly exponential. No shipped document promises `Retry-After` honoring or
   a specific backoff curve, so there is no contract to violate — recorded only
   so a future reader is not surprised.
3. `FAIL-01` also confirmed the documented retry-accounting rule exactly:
   `Progress.Retries` counts attempts after the first that actually begin (2 for
   3 attempts, 3 for 4), with no double counting across the standard and BigOCI
   domains. The known `go-oci-blob` error-flattening finding did **not** recur on
   the manifest path — the error preserved `after 4 attempts` and the underlying
   `503` at the top level.

Coverage-gap status: **all eight closed.** TLS/custom CAs, cross-host blob
redirects, external credential helpers, Bearer/OAuth exchange, multi-GiB BigOCI
payloads, publish-side retry injection, concurrent same-tag publication, and
forced commit-phase partial filesystem failure.

Process notes:

- `git -C $REPO status --porcelain` empty before and after all three scenarios;
  HEAD still `0b4be41`.
- `FAIL-02` ran unprivileged (euid 501, asserted in-probe — root would have
  defeated the `0500` fault) and restored every permission at the end; no
  unwritable directory left behind.
- Shim ports 5160/5161/5162 released; shared `imgoci-ft-dist` still Up.
- Verified by hand: all five prefix tag lists (including
  `ft/retry-exhaust` = `NAME_UNKNOWN`), the `ft/commit:v1` canonical role order,
  and full reachability of `ft/race:current`'s file manifests and blobs.

Next: Phase 7 (CLI binary — `CLI-01`, `CLI-02`, `CLI-03`), then Phase 8 (release
machinery and packaging — `REL-01`..`REL-04`).

## 2026-08-16 18:15 — Phase 7 executed

Three `functional-tester` agents: `CLI01Tester`, `CLI02Tester`, `CLI03Tester`.
Modules `$FT/consumer-cli0{1,2,3}`, prefixes `ft/cli2` and `ft/cli3`, ports 5169,
5170/5171, 5180–5185. Every command ran as a real OS process with argv, stdout,
stderr, and `$?` captured separately — no in-process test seams, and
`cli/run_test.go` was never opened.

Verdicts: **`CLI-01` PASS (69 invocations), `CLI-02` PASS, `CLI-03` PASS (all 13
reachable exit rows). No blockers. Phase 7 stop rule not triggered.**

- `CLI-01` — grammar and stream discipline. Bare invocation exits `2` with empty
  stdout and stderr beginning exactly `imgoci: no command given; run "imgoci
  help" for the commands`, with exactly **one** `imgoci: `-prefixed line and an
  unprefixed usage block. `version` stdout is byte-exact 31 bytes. `-h`/`-help`/
  `--help` are byte-identical to `help`; `-version`/`--version` to `version`;
  each `<cmd> -h` to `help <cmd>`. All 51 usage rows exit `2` with 0-byte
  stdout. Flag-after-operand gives the relocation hint: `imgoci: list: flags
  must come before the operands; move "-plain-http" before "…"`. Per-command
  flag sets diffed **empty** against the verified table, with `resolve`
  correctly lacking `-workers`/`-progress`. Zero registry contact across the
  whole matrix (witness shim: 2 log lines before, 2 after — both from its own
  curl smoke).
- `CLI-02` — real-registry data path. `publish` stdout is exactly one 72-byte
  digest line and **0 bytes** on every failure; `list` is 6-field TSV and
  `resolve` 9-field, both audited mechanically with `awk -F'\t'` (rows with the
  wrong field count: 0, order predicate true for every row); a no-match filter
  exits `0` with empty stdout; `fetch` stdout is always empty. **All 420
  progress lines** matched the documented shape exactly, and an `od` scan of all
  86 captured streams found **zero** `0x1b` and **zero** `0x0d` — no color, no
  carriage-return rewriting. Relative spec paths resolve against the **spec
  directory**, proven the hard way: a decoy `src/disk.img` in the run CWD was
  ignored and the digest was identical when run from `/`. All 13 JSON typo cases
  failed closed with zero registry I/O. Helper-backed `list` succeeded with no
  credential flag. `-timeout 100ms` through `stall` cancelled in 0.118 s with
  `timed out after 100ms:`. The CLI JSON spec expresses multipart natively
  (`"multipart": {"partSize": N}`), so no auxiliary publisher was needed.
- `CLI-03` — the exit-code matrix, every row as a real process:
  `0` version/list/fetch · `1` malformed ref and bare-401 · `2` missing selector
  · `3` `ErrNotFound` · `4` `ErrUnauthorized` · `5` `ErrInvalidIndex` · `6`
  `ErrInvalidSpec` (digest-only ref **and** bad producer spec) · `7`
  `ErrInvalidDest` (final path is a directory) · `8` `ErrDigestMismatch`
  (corrupt-blob) · `9` `ErrUnsupportedType` (BigOCI-only vs standard capability)
  · `11` `ErrDecode` (two-member gzip declared gzip) · `130` SIGINT · `143`
  SIGTERM. Every non-usage failure ended with exactly the two-line terminal
  report, and stdout was 0 bytes on every failure row.
  - Signals: SIGINT exited `130` in 0.027 s, SIGTERM `143` in 0.032 s, each
    logging `imgoci: interrupted (SIG…), stopping; press Ctrl-C again to force
    quit` then the sentinel line. The **second** signal path was also proven:
    with stderr blocked on an unread FIFO the process survived the first signal
    5.000 s, and the second killed it via the restored OS default
    (`WIFSIGNALED=True`, `WTERMSIG=2`/`15`). Destinations empty in every case.
  - Exit `10` recorded as unreachable-by-design with the source mapping quoted
    (`cli/run.go:43-44`, `:291`; sole producer `fetchfiles.go:159`;
    `cli/fetch.go` derives release and selection in one closure). Not faked, not
    silently skipped — residual risk, as the plan requires.

Findings this phase (both non-blocking):

1. `CLI-02-F1` — `cli/doc.go:61-62` states "Each file requires path, filename,
   and the five selector fields", but `cli/spec.go` has explicit `is required`
   checks for name, version, files, path, architecture, target, representation,
   role, and compression — **not** `filename`. I verified this in source myself.
   Omitting `filename` therefore skips the adapter's usage path and falls through
   to library validation: exit `6` with `spec §6 rule 3: manifests[0]
   io.imgoci.filename must match the filename grammar` instead of the usage exit
   `2` the documented adapter contract implies. Fails closed with zero registry
   requests and an accurate sentinel line, so this is presentational
   classification only — and a one-line fix.
2. `CLI-01-F1` — the binary's top-level usage renders `imgoci help [command]`
   and hints `Run "imgoci help publish" (or list, resolve, fetch)`, omitting
   `version`, while `cli/doc.go` and `reference/cli.md` both render
   `help [publish|list|resolve|fetch|version]`. The accepted topic set is
   identical (all five exit `0`; anything else exits `2`), so this is cosmetic
   help/doc drift.

Positive observations worth keeping:

- **Peer response bodies never reach diagnostics at all.** A hostile `400` whose
  body carried a forged `imgoci:` log record, ESC CSI sequences, and invalid
  UTF-8 produced only `imgoci: fetch index: manifest: registry returned status
  400` — `classifyManifestStatus` (`internal/registry/get.go:130-146`) maps
  status codes without echoing bytes. The tester then had to *manufacture* two
  extra vectors (a peer-controlled `Content-Type`, and raw argv bytes) to
  exercise the escaping path at all; both rendered as one escaped line with
  exactly 3 × `0x0a`, 0 × `0x1b`, 0 × `0xff` in captured stderr.
- Failure runs left no partial or committed output anywhere, including both
  signal-killed runs.
- Known findings reproduced through the CLI without change: bare-401 unclassified
  (`exit 1`, no credential disclosed) and the empty `.imgoci-stage/stored` pair
  after a BigOCI `ToDir` fetch.

Process notes:

- `git -C $REPO status --porcelain` empty before and after all three scenarios;
  HEAD still `0b4be41`. Ports 5169–5171 and 5180–5185 all released; shared
  registry Up.
- Verified by hand rather than trusting reports: `cli/doc.go` vs `cli/spec.go`
  required-member asymmetry, and a direct spot-run of the binary — bare → `2`
  with the exact text, `version` → 31 exact bytes, unknown flag → `2`,
  flag-after-operand → `2` with relocation guidance, nonexistent tag → `3`.

Docs-PR candidate list (owner's call, still not actioned): name/version grammar
on `ReleaseSpec` godoc + `reference/api.md`; three
`testdata/canonical/README.md` row corrections; tutorial port-5000 AirPlay
caveat and missing `cmp` prerequisite; spec commit on `docs/docs/index.md`;
document the retained empty `.imgoci-stage` directory; add the missing
`filename` required-check in `cli/spec.go` (or soften `cli/doc.go`); align the
`help` placeholder wording.

Next: Phase 8 (release machinery and packaging — `REL-01`..`REL-04`), the final
phase.

## 2026-08-16 18:50 — Phase 8 executed; campaign complete

Four `functional-tester` agents: `REL01Tester`, `REL02Tester`, `REL03Tester`,
`REL04Tester`. Read-only repository and read-only GitHub throughout; no `gh`
mutation of any kind and no vulnerability report submitted.

Verdicts: **`REL-01` PASS, `REL-02` PASS, `REL-03` PASS, `REL-04` BLOCKER.**

### THE ONE RELEASE BLOCKER — `REL-04-F1`

`SECURITY.md` at `0b4be41` publishes **author-facing template directives instead
of policy**. I read the file myself rather than trusting the report:

- `SECURITY.md:7-8`, the entire body of `## Supported Versions`: "Do not claim
  support windows or release lines until the project actually maintains them. /
  For a brand-new project, a short policy such as \"only the latest release is
  supported\" is usually enough."
- `SECURITY.md:24-25`: "If the project has a documented disclosure timeline, add
  it here. / If not, keep the policy short and avoid inventing guarantees."

Why this is a blocker and not a doc nit:

- The `## Supported Versions` section contains **no statement of which versions
  are supported** — its whole body is instruction addressed to the document's
  author. A user at `0.1.0` asking "is my version supported?" gets an answer
  written for somebody else.
- The file **ships inside the module zip** (`SECURITY.md`, 1039 bytes, confirmed
  in the downloaded `v0.0.0-20260816185632-0b4be41235c1` zip), so the defect is
  distributed with the release artifact, not merely rendered on GitHub.
- The plan's `REL-04` block names this exact text as a failed expected result,
  and the Phase 8 stop rule names "false security instructions" / template
  directives in the published policy. Correctly classified.
- There is no user-side workaround for a policy document that does not state a
  policy.

Also `REL-04-F2` (NON-BLOCKING, same unfinished-document defect, fix in the same
pass): `SECURITY.md:12` hedges the route as conditional — "…through GitHub's
private vulnerability reporting flow **when it is enabled for this repository**"
— while the feature is in fact enabled (`HTTP/2.0 200 OK`, `{"enabled":true}`).
The instruction is not false and the route works, but it makes the reporter
determine feature availability and names no fallback.

Notably, session 004's manual rehearsal did not catch this; it took a scenario
that read `SECURITY.md` **as user instructions rather than as a template**.

### Everything else in Phase 8 passed

- `REL-01` — the pre-v1 guard is intact and the proposal is exactly right.
  `release-type: go`, `include-v-in-tag: true`, `initial-version: 0.1.0`,
  `bump-minor-pre-major: true`, `bump-patch-for-minor-pre-major: true`,
  `draft: true`; manifest is exactly `{".": "0.0.0"}`; PR #9 is OPEN, titled
  `chore(master): release 0.1.0`, not a draft, base `master`, labeled
  `autorelease: pending`, and its diff touches **only**
  `.release-please-manifest.json` (`0.0.0` → `0.1.0`) and `CHANGELOG.md`. Spec is
  `Status: draft, 2026-08-11`. **Zero** releases and **zero** tags — verified
  locally, on the remote, and through the API. `release-please.yml` triggers on
  `master`, has `permissions: {}` at top level with job-scoped
  contents/pull-requests/issues write, uses the `IMGOCI_*` app credentials, names
  both config files, and **all 14 `uses:` across all three workflows are pinned
  to 40-hex SHAs**. No workflow publishes a binary, image, or package.
- `REL-02` — the library is genuinely gettable. A completely clean consumer
  (fresh `GOMODCACHE`/`GOCACHE`/`GOPATH`, no `replace`, no path into `$REPO`)
  resolved `github.com/imgoci/go@v0.0.0-20260816185632-0b4be41235c1` **from the
  public proxy** with `sum.golang.org` verification — the sumdb lookup record
  (tree index 60009551) is in evidence, proving the notary was consulted and
  agreed. `download.json` `Origin.Hash` equals repo HEAD. Build and run exit `0`;
  `ParseIndex` returned a digest equal to the SHA-256 of the copied canonical
  bytes. `go doc -all` from the **published** cache matches the inventory with
  zero delta. The published `go.mod` is byte-identical to the checkout's and says
  `go 1.26.5`. The zip carries root source, `README.md`, and **both** licenses,
  and has **0** `cli/` entries out of 298 files.
- `REL-03` — the CLI is uninstallable **by construction**, and the docs' claim is
  mechanically exact rather than aspirational. Clean
  `go install github.com/imgoci/go/cli@0b4be41` exits `1` with "The go.mod file
  for the module providing named packages contains one or more replace
  directives…", and `GOBIN` is empty before and after. `cli/go.mod` requires the
  root at `v0.0.0` — a version that exists nowhere — resolved solely by
  `replace github.com/imgoci/go => ../`. No CLI module version, tag, release, or
  asset exists; the root zip excludes `cli/`; PR #9 versions only the root
  library; no workflow publishes a CLI artifact; and five independent doc sources
  agree the CLI is private and unreleased.

### Campaign summary — 8 phases, 28 scenarios

| Phase | Scenarios | Outcome |
|---|---|---|
| 1 Consumer + docs | `CM-01`, `DOC-01`, `DOC-02` | 1 PASS, 2 NON-BLOCKING |
| 2 Core library | `LIB-01`..`LIB-04` | 4 PASS |
| 3 Integrity/adversarial | `ADV-01`..`ADV-04` | 3 PASS, 1 NON-BLOCKING |
| 4 Auth/TLS/transport | `NET-01`, `NET-02`, `AUTH-01`..`03` | 3 PASS, 2 NON-BLOCKING |
| 5 BigOCI and scale | `BIG-01`, `BIG-02` | 2 PASS |
| 6 Failure injection | `FAIL-01`, `RACE-01`, `FAIL-02` | 3 PASS |
| 7 CLI binary | `CLI-01`..`CLI-03` | 3 PASS |
| 8 Release machinery | `REL-01`..`REL-04` | 3 PASS, **1 BLOCKER** |

**All eight session-004 coverage gaps are closed.** No correctness, integrity,
confidentiality, or machine-contract defect was found anywhere in the
implementation. The single blocker is a documentation defect in the published
security policy.

Fix list before `0.1.0` (blocker first, then the accumulated non-blocking set):

1. **BLOCKER** — rewrite `SECURITY.md`: state a real supported-versions policy,
   drop both authoring directives, and state the private-reporting route
   unconditionally (the feature is enabled).
2. Name/version grammar on `ReleaseSpec` godoc + `reference/api.md`.
3. Three `testdata/canonical/README.md` fail-row corrections (`unsorted-keys` is
   a duplicate-key fixture; `exponent-1e0`/`1e2` hit the descriptor size type
   check).
4. Tutorial: port-5000 AirPlay caveat and the missing `cmp` prerequisite.
5. Spec commit on `docs/docs/index.md`.
6. Document the retained empty `.imgoci-stage` directory after a BigOCI `ToDir`
   fetch.
7. Add the missing `filename` required-check in `cli/spec.go` (or soften
   `cli/doc.go`), so the adapter's documented required set matches exit `2`.
8. Align the `help` placeholder wording with `cli/doc.go` / `reference/cli.md`.
9. Consider preserving the wrapped cause on the standard blob path (upstream
   `go-oci-blob` renders `registry request failed`), and the bare-401 /
   `ErrUnauthorized` classification.

The plan's exit condition requires the blocker to be fixed **and its scenario
re-run** before the campaign can be signed off, so `REL-04` must be re-executed
after the `SECURITY.md` fix.

Process notes:

- `git -C $REPO status --porcelain` empty before and after all four scenarios;
  HEAD still `0b4be41`. Zero GitHub mutations; zero vulnerability reports
  submitted; no `root:*` gate executed (`REL-04` used `moon query tasks` and
  `mise which`/`ls` to prove the five documented tasks exist and their pinned
  tools resolve).
- `REL-04` derived its own module-zip listing in an isolated scratch cache
  because `REL-02` had not yet produced `zip.txt`; neither peer's cache was read
  or written.
- Verified by hand: `SECURITY.md` lines 1-25 in full (the blocker), plus every
  `result` file and repo cleanliness.
- Registry `imgoci-ft-dist` on 127.0.0.1:5100 is still Up with the campaign's
  test content (~3.3 GiB under `ft/big`); safe to remove now that Phase 8 is
  done.

## 2026-08-16 19:15 — Remediation plan delivered

Planner agent `RemediationPlanner` produced
`.journal/005/REMEDIATION_PLAN.md` (460 lines) from the campaign findings.
Shape: Decision Summary → 3 PRs → Release Sequencing → Declined and Deferred →
Upstream (`go-oci-blob`) → Journal Updates.

Dispositions across all 21 inventory items: **1 FIX NOW** (the blocker, plus its
sibling hedge), **8 FIX SOON**, **4 DEFER**, **1 DECLINE**, **5 CLOSED**, and the
2 test-plan defects routed to the plan document rather than the product.

- **PR 1 `docs: publish the security policy`** (docs-only, the only release
  gate) — full replacement text for `SECURITY.md`. States a durable pre-v1
  policy ("Only the latest release is supported. Before the first release, use
  the latest commit on `master`.") with **no** invented support window, response
  time, or disclosure SLA, and names the private route unconditionally as
  `https://github.com/imgoci/go/security/advisories/new`. Acceptance includes a
  grep for all five authoring phrases and re-deriving the module zip to confirm
  the corrected file ships. Gate: a passing `REL-04` re-run.
- **PR 2 `docs: correct release-readiness contracts`** (docs-only) — grammar
  godoc + `reference/api.md`, the three `testdata/canonical/README.md` row
  corrections, the spec commit on `docs/docs/index.md`, the tutorial port-5000
  and `cmp` fixes, the `.imgoci-stage` documentation, plus interim guidance for
  the flattened blob-path cause and the accepted `0.1.0` classification of bare
  401 and unsupported publish compression.
- **PR 3 `fix(cli): reject a missing publish filename`** (the only code change) —
  add the missing `filename is required` guard to `fileToFileSpec` and extend the
  existing `TestDocumentToReleaseSpecRequiresMembers` table. Moves the case from
  library classification (exit `6`) to adapter usage (exit `2`), matching its six
  siblings, and deliberately keeps grammar-invalid filenames on the library path.

Groundedness checks I ran before accepting the plan:

- `cli/spec.go` `fileToFileSpec` really has six `is required` checks (path plus
  the five selectors) and **no** `filename` check, while
  `docs/docs/reference/cli.md:112` lists `files[].filename | yes` — so PR 3's
  direction (enforce, don't soften) matches the documented contract.
- Every cited moon task exists: root `check`/`test`/`format`/`lint`/`build`,
  `cli:test`, `docs:build`.
- `gh api -i repos/imgoci/go/private-vulnerability-reporting` → `HTTP/2.0 200 OK`
  and the proposed advisory intake URL returns `302` to GitHub login (i.e. the
  route exists and requires only sign-in), so PR 1's unconditional wording is
  accurate.
- Decision Summary covers all 21 items exactly once with disposition, target PR,
  and a blocking-`0.1.0` flag; only `REL-04-F1` is flagged as blocking.

Sequencing the plan recommends: merge PR 1 alone by squash merge, re-run
`REL-04` against the merge commit to flip the verdict to READY, prepare PR 2 and
PR 3 in parallel, keep PR #9 open and let Release Please refresh it, merge PR #9
only after PR 1 plus the passing re-run — and never propose a v1 while the spec
is draft.

Next: owner review of the remediation plan, then execute PR 1.

## 2026-08-16 19:55 — Remediation implemented as three PRs

Orchestrated the remediation plan with programmer/technical-writer agents across
three fresh worktrees created from `origin/master` (`0b4be41`) and reset to the
fetched tip per the worktree race rule.

- **PR #17 `docs: publish the security policy`** — branch `docs/security-policy`,
  commit `79ee972`. The blocker fix; I applied the single-file replacement inline
  rather than dispatching. All five authoring phrases are gone (verified by
  grep), the pre-v1 support policy is stated, and the private route is
  unconditional. CI green.
- **PR #18 `docs: correct release-readiness contracts`** — branch
  `docs/release-readiness-contracts`, commit `4618d8c`. Nine files by three
  parallel agents on disjoint file sets: `PR2GoDoc` (`publish.go`, `cli/doc.go`,
  `testdata/canonical/README.md`), `PR2Reference` (the three `reference/*.md`),
  `PR2Guides` (`index.md`, `first-release.md`, `architecture.md`). CI green.
- **PR #19 `fix(cli): reject a missing publish filename`** — branch
  `fix/cli-publish-filename`, commit `473c195`. The campaign's only code change.
  CI green.

Gates I ran (agents were instructed to run none):

- PR #17 branch: `root:build`, `cli:build`.
- PR #18 branch: `docs:build` (strict), `root:format`, `root:lint`, `root:build`,
  `root:test`, `cli:format`, `cli:lint`, `cli:build`, `cli:test`.
- PR #19 branch: `cli:format`, `cli:lint`, `cli:build`, `cli:test`.

One red-tree iteration, worth recording. Adding the `filename` guard before the
selector guards made two pre-existing table cases fail — `missing architecture`
and `missing compression` omitted `filename` as well as their own member, so the
new guard fired first and they had been passing for the wrong reason all along.
Corrective agent `FixSpecTestTable` supplied the other members instead of
loosening the assertions; `missing path` had the same latent defect and was fixed
too. `cli:format` then rejected the literal layout, so I applied
`golangci-lint fmt` once at the orchestrator level and re-gated to green.

Journal bookkeeping (commit `eccfe73`, scoped pathspec):

- `TECH_NOTES.md` — retired the "Remaining manual coverage gaps" bullet (all
  eight closed) and the session-004 rehearsal summary, replaced by the
  session-005 campaign-context and accepted-non-blocking-behavior bullets. The
  session-004 zstd diagnostic thread is retired; the three closed observations
  (`Retry-After: 0`, write-path gzip, Darwin x509 wording) were deliberately not
  promoted to durable debts.
- `.journal/005/FUNCTIONAL_TEST_PLAN.md` — the four plan-only corrections:
  `LIB-03` now separates semantic from grammar-malformed publish references,
  `BIG-02` writes a distinct marker at each of the twelve part offsets and
  requires `WireBytes` `3221225472` in both directions, `BIG-02` step 4 permits
  an empty `.imgoci-stage/stored/`, and `ADV-03` plus `## Verdict Criteria` no
  longer describe the zstd wording as a known misleading-window finding.

Remaining before `0.1.0`: owner review and squash merge of #17, then re-run
`REL-04` against the merge commit to flip the campaign verdict to READY, then
#18 and #19, then let Release Please refresh PR #9 and merge it. No v1 while the
spec is draft.
