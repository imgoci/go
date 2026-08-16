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
