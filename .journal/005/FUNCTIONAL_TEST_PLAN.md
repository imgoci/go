# Functional Test Plan — Release Readiness

This campaign proves the user-visible promises of `github.com/imgoci/go` by using the project as an external consumer would: from a separate Go module, through the private `cli/` binary built from source, against real registry processes, real files, and real HTTP/TLS/authentication boundaries. The system under test is `/Users/josh/code/imgoci/go`, branch `master`, commit `0b4be41`; the root module is `github.com/imgoci/go`, the implementation targets the imgoci v1 draft dated 2026-08-11 at spec commit `5b957102eeda16498fdcb80a738431b83abd4197`, and no repository test, e2e test, `root:check`, project-wide lint, or project-wide build is part of this manual campaign.

## Verdict Criteria

A **release blocker** is any result that makes the `0.1.0` library unsafe or materially different from its public contract: the root module cannot be acquired or compiled from a clean external module; valid documented use fails; invalid or non-canonical content is accepted; a digest/size/content-coding check can be bypassed; a failed fetch commits an unverified or torn final file contrary to the documented commit contract; credentials cross a host boundary or appear in diagnostics; a promised public sentinel is absent where the contract says it must match; the CLI returns the wrong documented exit status on a reachable path or mixes machine data with diagnostics; the release proposal is v1 while the spec is draft; the root module package contains or publishes the private CLI; licensing or security-reporting instructions are false; or the tutorial cannot produce a verified release when followed literally on a supported setup.

A **non-blocking finding** is a defect that preserves correctness, integrity, confidentiality, and the documented machine contract, and has an explicit safe workaround. Re-check the known Session 004 findings under that rule:

- the zstd single-segment decoded-size case is conforming only if it reports `zstd: decode: decompressed size exceeds configured limit`, matches `imgoci.ErrDecode`, and rejects before a codec-sized allocation;
- unsupported publish compression remaining unclassified is non-blocking only if publication fails closed before any registry write;
- a bare `401` remaining unclassified is non-blocking only if the request fails closed and no credential is disclosed;
- a misnamed canonical fixture is non-blocking only if its bytes still exercise the intended rejection and `ParseIndex` rejects them with `imgoci.ErrInvalidIndex`;
- sparse `ReleaseSpec.Name`/`Version` grammar documentation is non-blocking only if runtime validation enforces the normative grammar; and
- the tutorial's use of host port `5000` is a non-blocking portability issue when macOS AirPlay Receiver owns that port, provided the same literal walkthrough passes after substituting one free host port consistently.

The campaign ends when every mandatory scenario has a recorded pass, every release blocker has been fixed and re-run, all non-blocking findings have an owner-visible record, and the evidence index contains the exact commit, tool/container identities, commands, exit statuses, stdout/stderr, digests, and resulting filesystem state. A skipped mandatory scenario is not a pass. The only allowed exception is `BIG-02` when the host cannot provide the stated disk/time budget; that exception requires the explicit residual-risk entry and repository-owner acceptance before release.

## Promise Inventory

| Promise ID | Promise | Source | Scenario IDs |
|---|---|---|---|
| `P-MOD-01` | The released unit is one pre-v1 root package, imported from `github.com/imgoci/go` as `imgoci`, and it can be consumed without repository-local wiring. | `README.md` — Status; `doc.go` — package comment; `go.mod` — module and Go directives; `docs/docs/reference/api.md` — opening and Client sections | `CM-01`, `LIB-01`, `REL-02` |
| `P-IDX-01` | `ParseIndex` rejects invalid UTF-8, duplicate keys, rules 1–10 violations, wrong descriptor order, and non-RFC-8785 bytes with `ErrInvalidIndex`, while identity is the SHA-256 of the original canonical bytes. | `parse.go` — `ParseIndex`; `index.go` — `Index`; `errors.go` — `ErrInvalidIndex`; `docs/docs/reference/api.md` — Offline index; spec §§6, 9 | `LIB-01`, `ADV-01` |
| `P-IDX-02` | `Index`, `Resolved`, and `Release` are immutable views; slices and maps returned to callers are fresh copies, and a release/selection is bound by canonical index digest rather than pointer identity. | `index.go`, `resolve.go`, `release.go`; `docs/docs/reference/api.md` — Offline index, Selection, Client; `docs/docs/explanation/architecture.md` — Canonical bytes and Binding selection | `LIB-01`, `LIB-02`, `ADV-02` |
| `P-SEL-01` | `List` is broad, exact, sorted, capability-independent, and permits an empty result; `Resolve` is exact and atomic, applies default roles, capability filtering, then compression preference, and uses `ErrUnsupportedType` only for the capability step. | `list.go`, `resolve.go`, `capabilities.go`; `docs/docs/how-to/resolve-deliverables.md`; `docs/docs/reference/api.md` — Selection; spec §7 | `LIB-02` |
| `P-CAP-01` | Capability sets require the standard media type, reject parameters and ASCII-folded duplicates, use ASCII-only media-type comparison, default to standard offline, and default to standard plus BigOCI through `Client.Resolve`. | `capabilities.go`, `mediatype.go`, `client.go` — `Capabilities`; `docs/docs/reference/capabilities.md`; spec §§4, 7 | `LIB-02`, `BIG-01` |
| `P-PUB-01` | `Publish` accepts a tag-only reference, validates producer input before I/O, hashes and strictly decodes real source files, publishes manifests after blobs and the canonical index last, returns the index digest, and never compresses on the caller's behalf. | `publish.go`, `source.go`, `reference.go`; `docs/docs/reference/api.md` — Publishing; `docs/docs/explanation/architecture.md` — Standard and BigOCI forms; spec §§3, 5, 6, 9, 10 | `LIB-03`, `ADV-04`, `BIG-01`, `FAIL-01`, `RACE-01` |
| `P-FETCH-01` | `Fetch` validates response type and original bytes, honors a SHA-256 pin, pins a tag to its digest, and later manifest retrieval cannot be redirected by a tag mutation. | `fetch.go`, `reference.go`, `release.go`; `docs/docs/how-to/verify-a-release.md` — Pin the reference; spec §7.1 | `LIB-03`, `ADV-02` |
| `P-FILES-01` | `FetchFiles` checks selection binding, capabilities, and destination planning before network I/O; verifies all selected content in private staging; commits in entry order; leaves zero final files on pre-commit failure; and reports the committed prefix on a commit-phase failure. | `fetchfiles.go`, `dest.go`; `docs/docs/how-to/verify-a-release.md`; `docs/docs/explanation/architecture.md` — Stage-then-commit; spec §8 | `LIB-03`, `ADV-02`, `FAIL-02` |
| `P-OPT-01` | `WithWorkers` is positive and defaults to four when omitted; `WithProgress` supplies serialized absolute monotone snapshots with exact directions/phases, retry/fallback accounting, and one successful terminal snapshot; `Release` is safe for concurrent use. | `fetchfiles.go`, `progress.go`, `release.go`; `docs/docs/reference/api.md` — Fetching files and Progress | `LIB-04`, `BIG-01`, `BIG-02`, `FAIL-01` |
| `P-COMP-01` | `none`, one gzip member, one xz stream, and one non-skippable dictionary-free zstd frame are supported; concatenation, padding, skippable/trailing data, and decode working sets above the 8 MiB zstd-window/xz-dictionary ceilings fail strictly with `ErrDecode`. | `resolve.go` — compression set; `errors.go` — `ErrDecode`; `docs/docs/reference/errors.md` — `ErrDecode` and Decode working-set limits; spec §5.4 | `ADV-03` |
| `P-ERR-01` | The public matchable errors are exactly `ErrNotFound`, `ErrUnauthorized`, `ErrInvalidIndex`, `ErrInvalidSpec`, `ErrInvalidDest`, `ErrDigestMismatch`, `ErrUnsupportedType`, `ErrSelectionMismatch`, and `ErrDecode`; documented unclassified cases remain ordinary errors. | `errors.go`; `docs/docs/reference/errors.md` | `ADV-01`, `ADV-02`, `ADV-03`, `AUTH-03`, `CLI-03` |
| `P-TLS-01` | HTTPS is the default, `WithPlainHTTP` is explicit and unencrypted, `WithHTTPClient` supplies TLS/proxy/transport policy, and `WithUnverifiedExternalTransport` authorizes an opaque storage transport but never disables TLS verification. | `client.go` — client options; `docs/docs/reference/api.md` — Client and options | `NET-01`, `NET-02` |
| `P-WIRE-01` | Manifest and blob GETs request identity coding, reject non-identity `Content-Encoding`, preserve that policy across cross-host blob redirects, and strip credentials off-origin; token-realm traffic is outside identity enforcement. | `client.go` — external transport option; `docs/docs/explanation/architecture.md` — Identity content coding; spec §§7.1, 8 | `NET-02`, `AUTH-02` |
| `P-DOCKER-01` | Docker credentials are opt-in for the library and always enabled by the CLI; the config is read once, helpers run only when named, helper calls honor caller cancellation and a 10-second ceiling, no credential is written, Docker Hub's legacy key is mapped, and unsupported identity-token-only entries fail without leaking the token. | `client.go` — `WithDockerCredentials`; `docs/docs/how-to/use-docker-credentials.md`; `cli/run.go` — `newClient` | `AUTH-01`, `CLI-02` |
| `P-BEARER-01` | Anonymous and static-credential clients perform a real Bearer/OAuth token exchange after a challenge, pass `service`/`scope`, accept `token` or `access_token`, cache the result, and send static Basic credentials to the realm rather than cross-host storage. | `client.go` — `New` and `WithCredentials`; `docs/docs/how-to/use-docker-credentials.md` — anonymous/static behavior; `docs/docs/explanation/architecture.md` — Identity content coding | `AUTH-02`, `AUTH-03` |
| `P-BIG-01` | BigOCI is advertised by the client, is explicit per file, requires at least two parts, falls back to the standard form for a one-part plan, caps a plan at 4096 parts, writes no file-manifest tag, and verifies the assembled stored digest/size before decoding. | `publish.go` — `MultipartSpec`; `client.go` — `Capabilities`; `docs/docs/reference/api.md` — Publishing; `docs/docs/reference/capabilities.md`; `docs/docs/explanation/architecture.md` — Standard and BigOCI forms; spec §§3, 8 | `BIG-01`, `BIG-02`, `NET-02` |
| `P-RETRY-01` | Standard manifest/blob operations use one four-attempt retry domain, BigOCI uses its own non-nested domain, and progress counts attempts after the first that actually begin. | `docs/docs/explanation/architecture.md` — Two retry domains; `progress.go`; `docs/docs/reference/api.md` — Progress | `FAIL-01`, `LIB-04` |
| `P-CLI-01` | The private CLI has exactly `publish`, `list`, `resolve`, `fetch`, `help`, and `version`; its documented flags map to the public library; its publish JSON rejects unknown members and resolves file paths relative to the spec; and machine output is isolated on stdout. | `cli/doc.go`; `cli/run.go`; `cli/spec.go`; `docs/docs/reference/cli.md` | `CLI-01`, `CLI-02` |
| `P-CLI-02` | CLI exit codes are `0`, `1`, `2`, sentinels `3`–`11`, `130` for SIGINT, and `143` for SIGTERM; progress has the documented fixed line shape and failures are terminal-safe. | `cli/run.go` — exit constants, `sentinelExits`, signals; `cli/progress.go`; `cli/doc.go`; `docs/docs/reference/cli.md` — Output, Signals, Exit codes | `CLI-03`, `CLI-02` |
| `P-DOC-01` | A reader with only the first-release tutorial can build the CLI, start zot v2.1.20, publish/list/resolve/fetch, and obtain byte-identical verified output; the rest of the rendered documentation accurately describes the same public surface and spec pin. | `docs/docs/tutorials/first-release.md`; all `docs/docs/how-to/**`, `docs/docs/reference/**`, and `docs/docs/explanation/architecture.md`; `docs/docs/index.md` | `DOC-01`, `DOC-02` |
| `P-REL-01` | The first release is `0.1.0`; the manifest remains `0.0.0` before release; Release Please PR #9 proposes `0.1.0`; and no v1 release is created while the normative spec says `Status: draft`. | `release-please-config.json`; `.release-please-manifest.json`; `CONTRIBUTING.md` — Release Changes; PR #9; `../spec/spec.md` — header | `REL-01` |
| `P-REL-02` | The private `cli/` module is replace-wired to the checkout, is never versioned/published/released, is absent from the root module zip, and is not an installable promise of the root release. | `cli/go.mod`; `cli/doc.go`; `docs/docs/reference/cli.md`; `docs/docs/explanation/architecture.md` — CLI/release boundaries; `CONTRIBUTING.md` — Release Changes | `REL-03`, `DOC-01` |
| `P-META-01` | README status and spec claims, dual Apache-2.0/MIT licensing, private vulnerability reporting, public bug-reporting instructions, pinned tool versions, and named contributor task commands are accurate at release time. | `README.md`; `LICENSE-APACHE`; `LICENSE-MIT`; `SECURITY.md`; `CONTRIBUTING.md`; `mise.toml`; `moon.yml`; `cli/moon.yml`; `docs/moon.yml` | `REL-04`, `REL-01` |
| `P-QA-01` | Canonical fixture names/catalog descriptions identify the bytes and behavior they actually exercise. | `testdata/canonical/README.md` and `testdata/canonical/{pass,fail}/**` | `ADV-04` |

## Already Proven (Session 004)

Session 004 used 24 throwaway external probe programs—not the repository test suite—to execute 101 manual scenarios against disposable zot v2.1.20 registries. It already proved the broad offline parsing/selection surface; standard and BigOCI publish/fetch round trips; `none`, `gzip`, `xz`, and `zstd`; multiple representations and destinations; integrity and spec rejection; progress, retry, cancellation, Basic/Docker-config authentication, documentation examples, and CLI interoperability. It found no release-blocking correctness or safety defect. PR #15 then documented registry-dependent digest retention and the 8 MiB zstd/xz decode ceilings. This plan repeats only the load-bearing tutorial, one standard round trip, one BigOCI round trip, canonical-fixture rejection, and the CLI command chain; its weight is the eight untested transport/scale/failure/concurrency gaps and the release/package/document claims. Session 004's deferred diagnostic and documentation findings are re-checked in `ADV-03`, `ADV-04`, `AUTH-03`, and `DOC-01`.

## Environment

### Exact checkout and evidence root

Use a normal unprivileged account. Do not edit the repository. Do not run `go test`, `moon run root:check`, `root:test`, `root:test-e2e`, or any project-wide build/lint task.

```sh
export REPO=/Users/josh/code/imgoci/go
export FT=/tmp/imgoci-functional-0b4be41
export EVIDENCE="$FT/evidence"
mkdir -p "$EVIDENCE" "$FT/bin" "$FT/work"
cd "$REPO"
test "$(git branch --show-current)" = master
test "$(git rev-parse --short=7 HEAD)" = 0b4be41
test -z "$(git status --porcelain)"
printf 'repo=%s\nbranch=%s\ncommit=%s\n' \
  "$REPO" "$(git branch --show-current)" "$(git rev-parse HEAD)" \
  | tee "$EVIDENCE/state.txt"
```

If the working tree is not clean, do not clean, reset, stash, or delete anything; use a separate read-only checkout at `0b4be41` and record its path.

For each scenario create `$EVIDENCE/<ID>/` and save:

- the exact shell commands or Go probe source;
- stdout, stderr, and the numeric exit status separately;
- SHA-256 and byte size of every source/output file;
- the probe's assertion summary;
- relevant proxy/realm/helper JSON-line logs with authorization values redacted;
- `docker inspect` and image repo digest for each container involved; and
- a one-line `PASS`, `BLOCKER`, or `NON-BLOCKING FINDING` result.

Use only disposable credentials such as `ft-user` / `ft-secret`. Never capture a real Docker config, helper output, `Authorization` header, token, or private key in evidence.

### Prerequisites and pins

The repository pins Go `1.26.5`, Python `3.14.3`, golangci-lint `2.12.2`, mockery `3.7.2`, uv `0.11.0`, Moon `2.3.5`, melange `0.54.0`, apko `1.2.19`, Cosign `3.1.1`, and CUE `0.17.1` in `mise.toml`; only Go, Python/uv, and Moon are needed here. `go.mod`, `cli/go.mod`, and `mise.toml` all require Go `1.26.5`, with `GOTOOLCHAIN=local`.

```sh
cd "$REPO"
mise install
mise exec -- go version | tee "$EVIDENCE/go-version.txt"
mise exec -- moon --version | tee "$EVIDENCE/moon-version.txt"
mise exec -- uv --version | tee "$EVIDENCE/uv-version.txt"
docker version | tee "$EVIDENCE/docker-version.txt"
for x in git curl jq openssl shasum cmp unzip mkfile; do
  command -v "$x" || { echo "missing $x" >&2; exit 1; }
done
```

Docker/Engine, Git, curl, jq, OpenSSL, `shasum`, `cmp`, `unzip`, and `mkfile` are not repository-pinned. Record their versions. Pull and record the two registry images actually named by the repository harness:

```sh
docker pull ghcr.io/project-zot/zot:v2.1.20
docker pull registry:2
docker image inspect ghcr.io/project-zot/zot:v2.1.20 registry:2 \
  > "$EVIDENCE/registry-images.json"
```

`registry:2` is a floating tag; the `RepoDigests` value captured above is the identity used by this run.

### Throwaway external modules and binaries

Create one consumer module wired to the exact checkout for early work. It must be outside `$REPO`.

```sh
mkdir -p "$FT/consumer-local"
cd "$FT/consumer-local"
mise exec -- go mod init ft.local/imgoci-consumer
mise exec -- go mod edit -require=github.com/imgoci/go@v0.0.0
mise exec -- go mod edit -replace=github.com/imgoci/go="$REPO"
```

Every manual Go probe lives under this module, imports only public `github.com/imgoci/go` symbols, and runs with `go run ./cmd/<probe>` or as a built binary. Helper servers may use the standard library plus explicitly pinned codec modules; they must not import `github.com/imgoci/go/internal/...` or copy test assertions into the repository.

Build the CLI exactly as the tutorial does, but into the evidence workspace:

```sh
mkdir -p "$FT/bin"
mise exec -- go build -C "$REPO/cli" -o "$FT/bin/imgoci" .
"$FT/bin/imgoci" version
```

The expected line is exactly `imgoci (private reference CLI)`.

### Verified public surface under test

The root package exports these symbols and no others at `0b4be41`:

- Types: `Capabilities`, `Client`, `Deliverable`, `DeliverableRole`, `Dest`, `FetchOption`, `FileEntry`, `FileSpec`, `Index`, `ListQuery`, `MultipartSpec`, `Option`, `Progress`, `PublishOption`, `Reference`, `Release`, `ReleaseSpec`, `ResolveQuery`, `Resolved`, `Selector`, `Source`, and `TransportAlternative`.
- Constructors/functions/options: `EqualMediaType`, `FromFile`, `New`, `NewCapabilities`, `ParseIndex`, `StandardCapabilities`, `ToDir`, `ToFiles`, `WithCredentials`, `WithDockerCredentials`, `WithHTTPClient`, `WithPlainHTTP`, `WithProgress`, `WithUnverifiedExternalTransport`, and `WithWorkers`.
- `Client` methods: `Capabilities`, `Fetch`, `FetchFiles`, `Publish`, and `Resolve`.
- `Index` methods: `Annotations`, `Digest`, `Entries`, `List`, `Name`, `Resolve`, and `Version`.
- `Release` methods: `Digest` and `Index`.
- `Resolved` methods: `Entries` and `IndexDigest`.
- Sentinels: `ErrNotFound`, `ErrUnauthorized`, `ErrInvalidIndex`, `ErrInvalidSpec`, `ErrInvalidDest`, `ErrDigestMismatch`, `ErrUnsupportedType`, `ErrSelectionMismatch`, and `ErrDecode`.

`Option`, `FetchOption`, and `PublishOption` are sealed. `WithProgress` and `WithWorkers` return unexported concrete option types that satisfy the public option interfaces.

The CLI surface is:

| Command | Operands | Actual flags |
|---|---|---|
| `publish` | `<spec> <ref>` | `-plain-http`, `-timeout`, `-workers`, `-progress` |
| `list` | `<ref>` | `-plain-http`, `-timeout`, `-architecture`, `-target`, `-representation`, repeatable `-role` |
| `resolve` | `<ref>` | `-plain-http`, `-timeout`, required `-architecture`, `-target`, `-representation`, repeatable required `-compression`, repeatable `-role`, repeatable `-capability` |
| `fetch` | `<ref> <dest>` | every `resolve` flag plus `-workers`, `-progress` |
| `help` | optional `publish|list|resolve|fetch|version` | none |
| `version` | none | none |

Top-level `-h`, `-help`, `--help`, `-version`, and `--version` are accepted aliases. Flags must precede operands unless `--` terminates flag parsing. The CLI declares no imgoci-specific environment variable. Its credential path observes `DOCKER_CONFIG`; otherwise `os.UserHomeDir` uses the platform home (`HOME` on Unix, `USERPROFILE` on Windows). Named Docker credential helpers are found through `PATH` and inherit the process environment. Standard Go HTTP proxy environment behavior is not documented by this project and is not asserted here.

The exact exit mapping is `0` success, `1` unclassified failure, `2` usage, `3` `ErrNotFound`, `4` `ErrUnauthorized`, `5` `ErrInvalidIndex`, `6` `ErrInvalidSpec`, `7` `ErrInvalidDest`, `8` `ErrDigestMismatch`, `9` `ErrUnsupportedType`, `10` `ErrSelectionMismatch`, `11` `ErrDecode`, `130` SIGINT, and `143` SIGTERM.

### Registries and network shims

Use fixed ports below for readable evidence; if one is occupied, record the replacement and substitute it consistently. Do not reuse tutorial port `5000` for the later registry.

```sh
docker run --rm -d --name imgoci-ft-dist \
  -p 127.0.0.1:5100:5000 registry:2
until curl -sf -o /dev/null http://127.0.0.1:5100/v2/; do sleep 1; done
```

Build a disposable standard-library `netshim` under `$FT/consumer-local/cmd/netshim`. It must log one JSON object per request with timestamp, listener, method, escaped path, status, `Accept`, `Accept-Encoding`, authorization **scheme only**, request/response byte counts, and injected action. Implement and smoke-check these explicit modes, following the same `httputil.NewSingleHostReverseProxy` patterns used in `e2e_proxy_test.go`:

1. `passthrough`: reverse proxy to `http://127.0.0.1:5100`.
2. `redirect-front`: proxy manifests/uploads, but return `307 Temporary Redirect` for non-upload blob GET/HEAD to `http://127.0.0.1:5201<request-uri>`.
3. `storage`: proxy blob traffic; an option changes successful blob responses to `Content-Encoding: gzip` while preserving status and replacing length/body.
4. `basic-front`: return `401` plus `WWW-Authenticate: Basic realm="ft"` until the expected disposable Basic credential is present, then proxy.
5. `bearer-front`: return `401` plus `WWW-Authenticate: Bearer realm="http://127.0.0.1:5301/token",service="ft-registry",scope="repository:ft/auth:pull,push"` until `Bearer ft-token` is present, then proxy.
6. `token-realm`: log query parameters and the authorization scheme; optionally require the disposable Basic credential; return gzip-coded JSON containing `{"access_token":"ft-token","expires_in":300}`.
7. `retry-put`: return `503 Service Unavailable` with `Retry-After: 0` for the configured first N PUTs whose path contains `/manifests/`, then proxy; log a SHA-256 of every request body so retried bytes can be compared.
8. `bare-401`: always return `401` with no `WWW-Authenticate`.
9. `invalid-index`: return `200`, `Content-Type: application/vnd.oci.image.index.v1+json`, and `{}` for a manifest GET.
10. `corrupt-blob`: proxy a blob response, flip one byte without changing its length, remove `Docker-Content-Digest`, and return the altered body.
11. `stall`: accept the request and block until the request context is cancelled.

Smoke each listener with curl before using it, and save the shim source and logs. The shim is test equipment, not product code; a shim failure invalidates the scenario rather than becoming a product defect.

### Local certificate authority

Create a one-day local CA and a server certificate for `localhost` and `127.0.0.1`:

```sh
mkdir -p "$FT/pki"
cd "$FT/pki"
openssl genrsa -out ca.key 3072
openssl req -x509 -new -sha256 -days 1 -key ca.key \
  -subj '/CN=imgoci functional test CA' -out ca.crt
openssl genrsa -out server.key 2048
openssl req -new -key server.key -subj '/CN=localhost' -out server.csr
cat > server.ext <<'EOF'
subjectAltName=DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
keyUsage=digitalSignature,keyEncipherment
EOF
openssl x509 -req -sha256 -days 1 -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -extfile server.ext -out server.crt
openssl verify -CAfile ca.crt server.crt
```

Start the TLS registry only for `NET-01`:

```sh
docker run --rm -d --name imgoci-ft-tls \
  -p 127.0.0.1:5443:5000 \
  -v "$FT/pki:/certs:ro" \
  -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/server.crt \
  -e REGISTRY_HTTP_TLS_KEY=/certs/server.key \
  registry:2
until curl --cacert "$FT/pki/ca.crt" -sf -o /dev/null https://localhost:5443/v2/; do sleep 1; done
```

### Codec and large-file scaffolding

A disposable `codecgen` may import the repository-pinned `github.com/klauspost/compress/zstd v1.18.6` and `github.com/ulikunitz/xz v0.5.16`. It must emit and self-inspect: a valid unit of each compression; two concatenated gzip members; xz padding/trailing data; xz with `xz.WriterConfig{DictCap: 64 << 20}`; concatenated and skippable zstd frames; a zstd frame with a declared window above 8 MiB; and a single-segment zstd frame from `Encoder.EncodeAll` whose decoded size exceeds 8 MiB. For zstd cases, decode the header with `zstd.Header.Decode` and refuse to label the fixture unless `SingleSegment`, `WindowSize`, and `DictionaryID` have the intended values. This prevents the diagnostic re-check from relying on a guessed encoder mode.

For `BIG-02`, require at least 15 GiB free in both the host test volume and Docker storage. Create the 3 GiB sparse source with:

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
stat -f '%z' "$FT/work/bigoci-3g.img"
shasum -a 256 "$FT/work/bigoci-3g.img" | tee "$EVIDENCE/BIG-02/source.sha256"
```

The required size is exactly `3221225472` bytes.

## Phases

### Phase 1 — Consumer-module smoke and documentation-literal walkthrough

**Objective:** obtain an early external-consumer success and prove that the primary documentation gets a reader to verified bytes without hidden repository knowledge.

**Stop rule:** stop immediately for an import/build failure, a tutorial command that is wrong as written, a digest/byte mismatch, or machine data polluted by diagnostics. Record the port-5000 conflict as non-blocking only under the verdict rule above.

#### **ID** `CM-01`

*Promise*: `P-MOD-01`.

*Setup*: Use `$FT/consumer-local`, with the local replace shown in Environment.

*Steps*:

1. Create `main.go` that imports `imgoci "github.com/imgoci/go"`, calls `imgoci.New()`, `imgoci.StandardCapabilities()`, `imgoci.NewCapabilities("application/vnd.imgoci.file.v1", "application/vnd.bigoci.file.v1")`, `imgoci.EqualMediaType("Application/Vnd.Imgoci.File.V1", "application/vnd.imgoci.file.v1")`, and constructs `imgoci.FromFile`, `imgoci.ToDir`, `imgoci.ToFiles`, `imgoci.WithWorkers(1)`, and `imgoci.WithProgress(nil)`.
2. Include compile-time assignments of `WithPlainHTTP()` to `imgoci.Option`, `WithWorkers(1)` to both `imgoci.FetchOption` and `imgoci.PublishOption`, and references to all nine sentinels.
3. Print exactly `consumer-smoke ok` after every check succeeds.
4. Run:

   ```sh
   cd "$FT/consumer-local"
   mise exec -- go mod tidy
   mise exec -- go run . > "$EVIDENCE/CM-01/stdout" 2> "$EVIDENCE/CM-01/stderr"
   printf '%s\n' "$?" > "$EVIDENCE/CM-01/exit"
   ```

*Expected*: exit `0`; stdout exactly `consumer-smoke ok\n`; stderr empty; the import identifier is `imgoci`; no `cli/` package is imported.

*Evidence to capture*: `go.mod`, `go.sum`, `main.go`, streams, exit status, and `go list -deps` output.

*Blocker if*: the root package cannot be compiled from outside the repository, any listed symbol is absent, or CLI packages enter the dependency list.

#### **ID** `DOC-01`

*Promise*: `P-DOC-01`, `P-REL-02`.

*Setup*: Use a fresh `$FT/doc-literal` directory. Stop any existing `imgoci-zot`. First try host port `5000` exactly as documented. The checkout control `git -C imgoci-go checkout 0b4be41` is evidence control, not a replacement for a tutorial step.

*Steps*: Execute the tutorial's commands in order, literally:

```sh
cd "$FT/doc-literal"
git clone https://github.com/imgoci/go imgoci-go
git -C imgoci-go checkout 0b4be41
mkdir imgoci-tutorial
go build -C imgoci-go/cli -o "$PWD/imgoci-tutorial/imgoci" .
cd imgoci-tutorial
./imgoci version

docker run --rm -d --name imgoci-zot -p 5000:5000 ghcr.io/project-zot/zot:v2.1.20
curl -sf -o /dev/null -w '%{http_code}\n' http://localhost:5000/v2/

head -c 1048576 /dev/urandom > disk.img
shasum -a 256 disk.img
cat > release.json <<'EOF'
{
  "name": "tutorial",
  "version": "1.0.0",
  "files": [
    {
      "path": "disk.img",
      "filename": "disk.img",
      "architecture": "amd64",
      "target": "qemu",
      "representation": "raw",
      "role": "disk",
      "compression": "none"
    }
  ]
}
EOF
./imgoci publish -plain-http release.json localhost:5000/tutorial/example:v1
./imgoci list -plain-http localhost:5000/tutorial/example:v1
./imgoci resolve -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5000/tutorial/example:v1
./imgoci fetch -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5000/tutorial/example:v1 out
cmp disk.img out/disk.img && echo identical

docker stop imgoci-zot
cd ..
rm -rf imgoci-tutorial imgoci-go
```

Capture each command separately rather than piping stderr into stdout. Save the publish digest as `DIGEST`, require `^sha256:[0-9a-f]{64}$`, and before cleanup additionally run a digest-pinned list at `localhost:5000/tutorial/example@$DIGEST` to confirm the tutorial's retention-qualified claim while the object is still retained.

If Docker reports that host port `5000` is already allocated on macOS, capture the failure and check whether AirPlay Receiver owns it. Record the known non-blocking documentation finding, restart zot with `-p 5500:5000`, and repeat the walkthrough with every `localhost:5000` changed to `localhost:5500`. Do not pretend that substitution was literal.

*Expected*: version exactly `imgoci (private reference CLI)`; `/v2/` returns `200`; publish stdout is exactly one digest line; list stdout is exactly `amd64\tqemu\traw\tdisk\tnone\tapplication/vnd.imgoci.file.v1\n`; resolve has nine tab-separated fields, content digest equal to `shasum`, and content size `1048576`; fetch stdout is empty; `cmp` prints `identical`; the digest-pinned list succeeds immediately; cleanup removes the `--rm` container.

*Evidence to capture*: transcript with split streams/statuses, source/output hashes and sizes, publish digest, container inspect/image digest, and whether port substitution was needed.

*Blocker if*: any result above differs, except the classified host-port conflict; the CLI must not require a released binary or undocumented credential/config step.

#### **ID** `DOC-02`

*Promise*: `P-DOC-01`.

*Setup*: Use the exact checkout and a disposable copy of each Go example under `$FT/consumer-local/cmd/docs-*`.

*Steps*:

1. Build the rendered documentation only:

   ```sh
   cd "$REPO"
   mise exec -- moon run docs:build
   mise exec -- python -m http.server 8000 -d "$REPO/docs/build" \
     > "$EVIDENCE/DOC-02/http.log" 2>&1 &
   DOC_PID=$!
   ```

2. Fetch `/`, `/tutorials/first-release/`, every `/how-to/.../`, every `/reference/.../`, and `/explanation/architecture/`; require HTTP `200`, the expected page title, and working internal links.
3. Copy the Go snippets from `resolve-deliverables.md` into complete programs by adding only package/import/context setup. Run them against the tutorial release.
4. Copy the complete program from `verify-a-release.md`, replacing only the documented placeholder reference/digest with `DOC-01`'s real reference and selectors. Run it and require `verified against <DIGEST>`.
5. Follow `use-docker-credentials.md` later with the real helper in `AUTH-01`; cross-reference that evidence rather than simulating login here.
6. Compare every page's spec date/commit, API names, CLI flags, sentinels, decode limits, and output shapes with the inventory above. Stop the HTTP server.

*Expected*: strict docs build exits `0`; every navigation target renders; copied examples compile outside the repository and produce the documented selection/verified digest; all pages name draft 2026-08-11 and spec commit `5b957102eeda16498fdcb80a738431b83abd4197`; no page claims the CLI is released.

*Evidence to capture*: docs build output, HTTP status/title/link report, copied example sources, streams/statuses, and screenshots or saved rendered HTML for the tutorial, API, CLI, errors, capabilities, and architecture pages.

*Blocker if*: a rendered page/link is absent, a literal example cannot compile/run with only its stated substitutions, or the rendered contract differs from the implementation. Sparse name/version grammar remains the classified `ADV-04` finding rather than an automatic blocker.

### Phase 2 — Core library contracts

**Objective:** verify the complete root-package model through public calls, then perform one representative standard-form real-registry round trip.

**Stop rule:** stop for any accepted invalid index, mutable supposedly immutable view, wrong selection, wrong content, or preflight that sends network traffic.

#### **ID** `LIB-01`

*Promise*: `P-MOD-01`, `P-IDX-01`, `P-IDX-02`.

*Setup*: Copy `testdata/canonical/pass/additional-members.json` into the consumer workspace; do not import repository internals.

*Steps*:

1. Run `go doc -all github.com/imgoci/go` from `$FT/consumer-local` and compare it to the public-surface inventory in Environment.
2. In a probe, read the canonical file, call `imgoci.ParseIndex(raw)`, and independently compute `sha256.Sum256(raw)`.
3. Assert `Digest`, `Name`, `Version`, entry order/fields, root unknown annotations, descriptor unknown annotations, and original mixed-case media values where present.
4. Mutate the slice and maps returned by `Entries()` and `Annotations()`, call the methods again, and require the original values. Repeat for `Resolved.Entries()` after a valid resolution.
5. Parse an independent copy of the same bytes and require equal digests, not pointer identity.

*Expected*: `go doc -all` exposes exactly the inventoried types/functions/methods/sentinels with godoc; `Index.Digest()` equals the SHA-256 of the exact input bytes; every fresh accessor call is unaffected by caller mutations; independently parsed canonical copies have equal digest identity.

*Evidence to capture*: `go doc -all`, probe source/output, raw file hash, and mutation-before/after JSON.

*Blocker if*: the export set is different without corresponding docs, identity is based on re-encoding, or returned data aliases internal state.

#### **ID** `LIB-02`

*Promise*: `P-SEL-01`, `P-CAP-01`, `P-IDX-02`.

*Setup*: Copy canonical `multiple-transport-alternatives.json`, `incus-vm.json`, `linux-netboot-complete.json`, and `bigoci-manifest-type.json` into the external module.

*Steps*:

1. Call `Index.List` with an empty query, each scalar filter, repeated role filters, and a no-match filter. Check the three-level sorting and that BigOCI/unknown alternatives are still listed.
2. Call `Index.Resolve` with ordered compressions `zstd,gzip,none`, nil roles, explicit roles, zero capabilities, `StandardCapabilities`, and a validated standard+BigOCI capability set.
3. On `incus-vm`, require default roles `disk,metadata`; on `linux-netboot`, require every present role; for an unknown representation, require every role.
4. Verify no deliverable, absent role, and no accepted compression return `nil` selection and no public sentinel; verify capability exhaustion returns `nil` and `errors.Is(err, imgoci.ErrUnsupportedType)`.
5. Require `NewCapabilities` to reject a parameter, malformed type/subtype, ASCII-folded duplicate, and missing standard type. Verify `EqualMediaType` folds ASCII case but not U+017F/U+212A look-alikes.

*Expected*: results exactly follow spec §7 ordering; list no-match is empty with nil error; resolution never returns partial roles; zero offline capabilities select standard only; explicit BigOCI capability can select BigOCI; only capability exhaustion matches `ErrUnsupportedType`.

*Evidence to capture*: input hashes, query/result JSON, exact errors, and an `errors.Is` matrix over all nine sentinels.

*Blocker if*: filtering/order/default roles differ, a failure returns a partial `Resolved`, or Unicode folding admits a type look-alike.

#### **ID** `LIB-03`

*Promise*: `P-PUB-01`, `P-FETCH-01`, `P-FILES-01`.

*Setup*: Use `registry:2` at `127.0.0.1:5100`. Create a `linux-netboot` release with real `kernel` and `initramfs` files, standard form, `compression=none`, distinct filenames, and harmless unknown annotations.

*Steps*:

1. Build `imgoci.New(imgoci.WithPlainHTTP())`, publish `127.0.0.1:5100/ft/core:v1`, and retain the returned digest.
2. Fetch by tag and by `repo@<digest>`; require both release digests to equal the returned digest and the source file hashes recorded in entries.
3. Resolve with nil roles and `Compressions: []string{"none"}`; require both roles.
4. Fetch with `ToDir`; verify exact bytes/modes and that a pre-existing unrelated file remains untouched.
5. Fetch the same selection with `ToFiles` into two different parent directories; mutate the original map after `ToFiles` construction and require the constructed destination to retain the original paths.
6. Exercise zero `Dest`, missing/extra role maps, two roles resolving to one path, an existing directory at a final path, and a symlinked-parent alias. Record the netshim request count before/after each call.
7. Try Publish with digest-only, tag+digest, and name-only references; malformed/lowercase-invalid references; a zero `Source`; reserved `io.imgoci.*` annotations; and inconsistent shared source declarations.

*Expected*: publish returns `sha256:<64 lowercase hex>`; tag/digest fetches agree; both destination forms contain byte-identical files; invalid destinations match `ErrInvalidDest` and cause zero request-count change; digest-only, tag+digest, and name-only publish references match `ErrInvalidSpec`; grammar-malformed publish references fail before I/O without a public sentinel; invalid producer specs match `ErrInvalidSpec`; every rejected publication causes zero request-count change and writes no release tag.

*Evidence to capture*: probe source, registry/proxy logs, index digest, source/output hashes and modes, tag listing, and sentinel matrix.

*Blocker if*: invalid preconditions reach the registry, a tag is written on failed validation, an unrelated output is changed, or fetched bytes differ.

#### **ID** `LIB-04`

*Promise*: `P-OPT-01`, `P-RETRY-01`.

*Setup*: Reuse the two-role release. Use `WithWorkers(2)` and a callback that appends snapshots under a mutex and detects concurrent callback entry with an atomic guard.

*Steps*:

1. Publish and fetch with progress, then assert every snapshot field and transition.
2. Run four concurrent `FetchFiles` calls from the same `Client`, `Release`, and `Resolved` into four distinct directories.
3. Run `WithProgress(nil)` and omitted `WithWorkers`; then call with `WithWorkers(0)` and `WithWorkers(-1)` while recording proxy counts.
4. Pass `WithHTTPClient(nil)` to `New` and perform a normal fetch.

*Expected*: callback entry is serialized; counts never decrease; totals are fixed; successful fetch has exactly one final `Direction="fetch", Phase="commit"` snapshot; successful publish has exactly one final `Direction="publish", Phase="index"` snapshot; all four concurrent outputs match; omitted workers use the documented default four; nonpositive workers fail before I/O without a public sentinel; nil progress/client options are ignored.

*Evidence to capture*: snapshot JSON, concurrency guard result, all output hashes, proxy counts, and exact nonpositive-worker errors.

*Blocker if*: progress regresses/interleaves, a success lacks or duplicates its terminal snapshot, concurrent use corrupts state, or invalid workers reach the network.

### Phase 3 — Integrity and adversarial behavior

**Objective:** prove that hostile bytes fail closed, no alternative is silently substituted after retrieval failure, and the known diagnostic/classification findings remain non-safety issues.

**Stop rule:** stop for any invalid input accepted, wrong final byte committed, post-failure fallback, large decoder allocation before rejection, or failed producer input that writes a tag.

#### **ID** `ADV-01`

*Promise*: `P-IDX-01`, `P-ERR-01`.

*Setup*: Copy all files under `testdata/canonical/pass/` and `testdata/canonical/fail/` to the external workspace. Treat them as public input bytes, not as a repository test invocation.

*Steps*:

1. A throwaway probe calls only `imgoci.ParseIndex` once per file and prints filename, input SHA-256, result digest or error, and all `errors.Is` results.
2. Manually inspect at least one accepted unknown-member fixture and each rejection class: pretty whitespace, non-canonical exponent, key order, nonminimal escape, raw/decoded duplicate keys, invalid UTF-8 key/value, lone surrogate, and canonical JSON with wrong descriptor-array order.
3. For rule failures require the detail to name the rule where the source promises it; for decode/canonical failures require descriptive check detail.

*Expected*: every `pass/` file succeeds and records its exact-byte digest; every `fail/` file returns no index and matches only `ErrInvalidIndex`; wrong descriptor order is distinguishable as rule 9, while non-canonical JSON is the canonical-bytes check.

*Evidence to capture*: per-file result table, input hashes, and exact error strings.

*Blocker if*: any pass/fail classification is wrong, a failure returns an index, or a noncanonical input's identity is silently normalized.

#### **ID** `ADV-02`

*Promise*: `P-FETCH-01`, `P-FILES-01`, `P-IDX-02`, `P-ERR-01`.

*Setup*: Publish release A under `ft/integrity:v1`, fetch its `Release`, then publish different release B under the same tag. Also publish a two-role release and a release whose `disk` role has a preferred compressed alternative plus a valid `none` alternative. Start the `corrupt-blob` shim when instructed.

*Steps*:

1. Resolve and fetch using the already-fetched release A after the tag points to B.
2. Fetch the tag again and require release B; fetch both explicit digest references immediately.
3. Through `corrupt-blob`, fetch a standard `none` blob whose one byte is flipped. Require `ErrDigestMismatch` and no final file.
4. Corrupt the selected preferred compressed alternative while leaving the `none` alternative valid. Require complete failure and prove no request was made for the alternative's manifest/blob after the selected retrieval failed.
5. Corrupt one role in a two-role selection after the other is eligible to finish staging. Pre-create old final files with marker bytes.
6. Pass a `Resolved` from release A with release B to `FetchFiles` and record the proxy count.

*Expected*: the old `Release` still yields A bytes; the new fetch yields B; immediate digest references yield their own bytes subject to the registry retaining them; corruption matches `ErrDigestMismatch` for `none` or `ErrDecode` for a damaged codec, commits no new final file, and does not try another transport; two-role pre-commit failure leaves both marker finals unchanged; selection mismatch matches `ErrSelectionMismatch` before I/O.

*Evidence to capture*: A/B digests and hashes, tag timeline, proxy request sequence, final-directory before/after listing and hashes, and sentinel matrix.

*Blocker if*: tag mutation redirects an existing `Release`, corruption produces a final file, the client falls back after retrieval failure, or selection mismatch reaches the network.

#### **ID** `ADV-03`

*Promise*: `P-COMP-01`, `P-ERR-01`.

*Setup*: Use `codecgen` fixtures and a fresh registry repository. Record `/usr/bin/time -l` around the large-window cases on macOS.

*Steps*:

1. Publish and fetch one valid real file for each `none`, `gzip`, `xz`, and `zstd`; compare decoded bytes.
2. Attempt to publish two-member gzip, padded/trailing xz, concatenated zstd, skippable-first zstd, and dictionary-required zstd.
3. Attempt xz with a 64 MiB LZMA2 dictionary and non-single-segment zstd with a declared window above 8 MiB.
4. Generate a self-confirmed single-segment zstd frame with decoded size above 8 MiB, publish it as zstd, and require the exact diagnostic `zstd: decode: decompressed size exceeds configured limit`, a match on `ErrDecode`, and rejection before a codec-sized allocation; record peak RSS.
5. Publish a syntactically valid private compression token such as `x-ft-brotli` with real bytes, using a request-counting proxy. Check every public sentinel. This is the known unsupported-compression classification re-check.

*Expected*: all four valid files round-trip byte-for-byte; every structural/working-set violation fails before upload and matches `ErrDecode`; xz/zstd limit failures occur before codec-sized allocation and the process does not approach the declared 64 MiB/large window merely to reject the header; the single-segment case reports exactly `zstd: decode: decompressed size exceeds configured limit`, matches `ErrDecode`, and rejects before a codec-sized allocation; unsupported compression fails before any registry request, matches none of the nine public sentinels, and leaves no tag.

*Evidence to capture*: codecgen source/header dumps, source/stored/decoded hashes and sizes, exact errors, `errors.Is` matrix, request counts, `time -l` output, and tag listing.

*Blocker if*: an invalid compression unit is accepted, the decoder allocates the hostile declared working set before rejection, the single-segment diagnostic differs from `zstd: decode: decompressed size exceeds configured limit` or does not match `ErrDecode`, any invalid publish writes, or unsupported compression is published. Missing unsupported-compression sentinel remains non-blocking under the stated expectations.

#### **ID** `ADV-04`

*Promise*: `P-PUB-01`, `P-QA-01`.

*Setup*: Use a request-counting proxy and a one-file valid standard spec as the baseline.

*Steps*:

1. Vary `ReleaseSpec.Name`: empty, uppercase, leading/trailing separator, over 128 bytes, and valid private/basic-token forms. Vary `Version`: empty, whitespace/control-containing, 129 printable bytes, and exactly 128 printable non-whitespace ASCII bytes.
2. Require invalid cases before I/O; publish the valid 128-byte version successfully.
3. Compare `go doc github.com/imgoci/go.ReleaseSpec`, the API/CLI references, and spec §5.1. Record whether name/version grammar is stated where an API user looks. This re-checks the sparse-comment finding.
4. Manually compare every row of `testdata/canonical/README.md` with the named file's raw bytes and actual `ParseIndex` result. Identify the Session 004 misnamed fixture precisely rather than guessing from its name.

*Expected*: runtime enforces name as a 1–128-byte basic token and version as 1–128 printable ASCII characters without whitespace/control; invalid cases match `ErrInvalidSpec`, make zero requests, and create no tag; the boundary-valid version publishes. Any remaining doc omission or fixture-label mismatch is recorded as non-blocking only when runtime behavior and fixture pass/fail remain correct.

*Evidence to capture*: case table with bytes/lengths/errors/request counts, successful boundary digest, relevant godoc/rendered excerpts, and fixture catalog audit.

*Blocker if*: invalid name/version reaches I/O or is published, a boundary-valid value is rejected contrary to spec, or a mislabeled fixture does not exercise the intended failure. Sparse prose or naming alone remains non-blocking.

### Phase 4 — Authentication, TLS, and transport boundaries

**Objective:** close the custom-CA, cross-host redirect, external-helper, and Bearer/OAuth gaps with real HTTPS, real processes, and observable host boundaries.

**Stop rule:** stop for credential/token leakage, off-origin authorization, acceptance of coded manifest/blob bytes, or a custom CA that cannot be supplied through the public option.

#### **ID** `NET-01`

*Promise*: `P-TLS-01`.

*Setup*: Start `imgoci-ft-tls` at `https://localhost:5443` with the local CA. Create a standard and a multipart spec.

*Steps*:

1. With plain `imgoci.New()`, try `Fetch` or `Publish` against `localhost:5443/ft/tls:v1` before the repository exists.
2. Repeat with `WithUnverifiedExternalTransport()` but no custom CA.
3. Repeat with `WithPlainHTTP()` against the TLS listener.
4. Load `ca.crt` into a new `x509.CertPool`, clone `http.DefaultTransport`, set `TLSClientConfig.RootCAs`, wrap it in `&http.Client{Transport: tr}`, and call `imgoci.New(imgoci.WithHTTPClient(httpClient))` without `WithPlainHTTP`.
5. Publish/fetch standard and BigOCI releases through that client and compare bytes.

*Expected*: default and “unverified external” clients fail TLS verification with an x509 unknown-authority error and no public sentinel; `WithUnverifiedExternalTransport` does not weaken TLS; plain HTTP to the TLS socket fails; the custom-CA client completes HTTPS publication/fetch for both forms with exact bytes. `curl --cacert` succeeds while curl without the CA fails.

*Evidence to capture*: CA/server certificate fingerprints and SANs, container inspect/logs, all errors/sentinels, HTTPS request log, and output hashes.

*Blocker if*: the CA cannot be supplied with `WithHTTPClient`, the untrusted cert is accepted without it, `WithUnverifiedExternalTransport` disables verification, or any successful transfer uses HTTP.

#### **ID** `NET-02`

*Promise*: `P-TLS-01`, `P-WIRE-01`, `P-BIG-01`.

*Setup*: Publish one standard and one genuine ≥2-part BigOCI release directly to `127.0.0.1:5100`. Run `redirect-front` on `127.0.0.1:5200` and identity `storage` on `127.0.0.1:5201`.

*Steps*:

1. Fetch each release through a reference whose registry is `127.0.0.1:5200`, resolve, and fetch files with a default concrete transport plus `WithPlainHTTP`.
2. Inspect both listeners' logs for headers and authorization scheme.
3. Restart storage in gzip mode and repeat both fetches into empty destinations.
4. Define a public `roundTripFunc`/opaque wrapper around a concrete transport. Use it in `WithHTTPClient` without `WithUnverifiedExternalTransport`, then with the option.
5. With the option present, repeat the gzip storage case to prove identity enforcement remains active.

*Expected*: identity storage follows cross-host `307` redirects and returns exact bytes for standard and BigOCI; every manifest/blob GET carries `Accept-Encoding: identity`; storage sees no `Authorization`; gzip storage fails before commit with an error containing `the response is not identity coded`; the opaque transport is rejected at adapter construction with zero requests unless explicitly authorized; authorization makes identity storage work but does not make gzip-coded storage acceptable.

*Evidence to capture*: paired front/storage logs, redirect `Location`, header/scheme records, output hashes, opaque-transport errors and request counts, and empty failed destinations.

*Blocker if*: either form cannot follow a valid cross-host redirect, credentials reach storage, the identity marker/header is lost, coded bytes are accepted, or the opaque transport is used without opt-in.

#### **ID** `AUTH-01`

*Promise*: `P-DOCKER-01`.

*Setup*: Run `basic-front` at `127.0.0.1:5400`. Create `$FT/docker-helper/config.json` containing `{"credHelpers":{"127.0.0.1:5400":"imgoci-ft"}}`. Put executable `docker-credential-imgoci-ft` alone at the front of `PATH`; on `get`, it records the action and stdin server key, then prints `{"ServerURL":"<input>","Username":"ft-user","Secret":"ft-secret"}`.

*Steps*:

1. With `DOCKER_CONFIG=$FT/docker-helper`, call `New(WithDockerCredentials())`, then Fetch/FetchFiles through the Basic front.
2. Construct a second client and repeat. Verify helper call records increased and actions were `get`.
3. Point `DOCKER_CONFIG` at an empty directory and prove the helper is never invoked and the Basic-protected request fails.
4. Replace the helper with one that records `get` then `exec sleep 60`. With a background context, measure the built-in ceiling; with a 250 ms caller deadline, measure cancellation.
5. Put an identity-token-only entry containing a recognizable marker in config; require failure and search all diagnostics/evidence for the marker.
6. For Docker Hub mapping, put the helper under config key `https://index.docker.io/v1/`, use reference host `docker.io`, and use a custom transport that rewrites network dialing only to the local Basic front while leaving the logical request host/auth lookup as `docker.io`. Record the helper's stdin key.
7. Run one CLI `list` through the Basic front with the same `DOCKER_CONFIG`; there is no credential flag.

*Expected*: valid helper credentials complete transfers; each real lookup executes the named helper afresh; the key is exactly the named `host:port`, while logical `docker.io` maps to `https://index.docker.io/v1/`; empty config never runs a default platform helper; the wedged lookup fails in approximately 10 seconds with `the credential helper did not answer within 10s`, while caller cancellation returns near 250 ms; identity-token-only storage fails without anonymous downgrade and without the marker in errors; no file/config is written by the library; the CLI uses Docker credentials automatically.

*Evidence to capture*: sanitized config/helper scripts, action/server-key logs, timings, process-tree check after timeout, front logs with auth scheme only, config directory before/after hashes, sentinel matrix, and secret-marker search result.

*Blocker if*: helper credentials are ignored, an unnamed/default helper runs, timeout/cancellation leaves the helper alive, token material appears in diagnostics, identity-token input downgrades to anonymous, the config changes, or CLI needs an undocumented credential flag.

#### **ID** `AUTH-02`

*Promise*: `P-BEARER-01`, `P-WIRE-01`.

*Setup*: Run `bearer-front` on `127.0.0.1:5300` and gzip `token-realm` on `127.0.0.1:5301`; backend repository is `ft/auth`.

*Steps*:

1. With `New(WithPlainHTTP())`, Fetch and FetchFiles a standard release through the bearer front. The realm returns only `access_token`, not `token`.
2. Repeat another manifest/blob operation with the same client before token expiry.
3. Restart the realm requiring Basic `ft-user:ft-secret`; use `New(WithPlainHTTP(), WithCredentials("ft-user", "ft-secret"))` and repeat.
4. Inspect realm query and both hosts' auth-scheme logs.

*Expected*: initial registry request has no authorization; challenge carries the exact realm/service/scope; realm query contains `service=ft-registry` and `scope=repository:ft/auth:pull,push`; gzip-coded OAuth JSON with `access_token` is accepted; registry retry carries `Bearer`, not Basic; static Basic goes to the realm only; subsequent operations reuse the unexpired token rather than exchanging for every request; fetched bytes match. Token-realm gzip is accepted while manifest/blob gzip remains rejected by `NET-02`.

*Evidence to capture*: sanitized challenge, realm query/hit count/content encoding, auth schemes per host, transfer output hashes, and token-marker absence outside sanitized harness logs.

*Blocker if*: the exchange is not attempted anonymously, `access_token` is ignored, service/scope is lost, Basic crosses to storage, token is not reused at all, realm compression is rejected by identity enforcement, or returned content differs.

#### **ID** `AUTH-03`

*Promise*: `P-BEARER-01`, `P-ERR-01`.

*Setup*: Run `bare-401` at `127.0.0.1:5401`.

*Steps*:

1. Call `Client.Fetch` through it and test all nine public sentinels.
2. Run CLI `list -plain-http` through it and record streams/status.
3. Search errors for credentials or peer-supplied control characters.

*Expected*: request fails closed; current known behavior is no public sentinel, detail `the registry refused the request without saying how to authenticate`, and CLI exit `1` with `imgoci: no sentinel matched (exit 1)`; stdout is empty and no secret appears. If implementation now maps it to `ErrUnauthorized`/exit `4`, record the improvement and require docs to agree.

*Evidence to capture*: client sentinel matrix, CLI streams/status, and sanitized request log.

*Blocker if*: the request succeeds, retries indefinitely, leaks a credential, or emits terminal control. The absent `ErrUnauthorized` classification alone remains the known non-blocking finding.

### Phase 5 — BigOCI and scale

**Objective:** confirm the profile boundary cheaply, then prove streaming correctness with a real multi-GiB multipart file.

**Stop rule:** stop for a one-part BigOCI manifest, more than 4096 planned parts accepted, missing graph objects, wrong output, OOM, or a file-manifest tag.

#### **ID** `BIG-01`

*Promise*: `P-BIG-01`, `P-CAP-01`, `P-OPT-01`, `P-PUB-01`.

*Setup*: Create a deterministic 33 MiB file. Use `PartSize: 16 << 20` for a three-part request and `PartSize: 64 << 20` for a one-part fallback request.

*Steps*:

1. Publish the three-part request with progress. Fetch/list/resolve/fetch it through the public client and compare bytes.
2. GET the file manifest by `FileEntry.Digest` from the real registry and inspect its top-level types, `layers` count, whole-file annotations, and canonical bytes without re-encoding it.
3. Query `/v2/<repo>/tags/list` before/after; require only the release tag was added.
4. Publish the 64 MiB-part request and inspect its entry type/progress.
5. Resolve the BigOCI-only release with `StandardCapabilities`, then with `Client.Resolve` zero capabilities.
6. Try a 4097-byte file with `PartSize: 1`.

*Expected*: the first entry has artifact type `application/vnd.bigoci.file.v1`, exactly three ordered parts, matching whole stored digest/size, no file-manifest tag, and exact fetched bytes; the one-part plan publishes standard artifact type `application/vnd.imgoci.file.v1` and increments `Progress.Fallbacks` exactly once; standard-only resolution fails with `ErrUnsupportedType`, client-default resolution succeeds; the 4097-part plan fails with `ErrInvalidSpec` before I/O.

*Evidence to capture*: raw manifest bytes/hash/JSON, tag lists, progress snapshots, source/output hashes, capabilities results, and request counts.

*Blocker if*: any expected profile boundary, type, fallback count, graph reachability, or content result differs.

#### **ID** `BIG-02`

*Promise*: `P-BIG-01`, `P-OPT-01`.

*Setup*: Confirm the stated 15 GiB budgets. Use the exact 3 GiB sparse source and `PartSize: 256 << 20` (expected 12 parts). Configure the probe to assert that the raw BigOCI manifest contains 12 distinct layer digests. Build the external probe once; measure the probe binary rather than `go run` compilation.

*Steps*:

1. Publish `ft/big:3g` through zot or Distribution with public `Client.Publish`, `MultipartSpec{PartSize: 256 << 20}`, `WithWorkers(2)`, and progress; wrap execution with `/usr/bin/time -l`.
2. Inspect the raw BigOCI manifest and require 12 parts with 12 distinct layer digests. Record every part size/digest and the whole stored size/digest.
3. Fetch/resolve/fetch to a fresh volume with `WithWorkers(2)` and progress, also under `/usr/bin/time -l`.
4. Require exact size, `shasum -a 256` equality, and `cmp` success. Confirm that no staged or cache file remains after successful commit. An empty `<dest>/.imgoci-stage/stored/` directory may remain and must contain no files.

*Expected*: publish/fetch complete without OOM; manifest has 12 parts with 12 distinct layer digests and whole size `3221225472`; returned index/file digests are valid SHA-256; output is exactly `3221225472` bytes and byte-identical; publish and fetch progress is monotone, and each terminal `WireBytes` value is exactly `3221225472`; only the release tag exists.

*Evidence to capture*: free-space checks, wall time/peak RSS, all progress, raw manifest, part table, container storage growth, hashes/sizes, `cmp` status, and post-success directory tree.

*Blocker if*: sufficient resources exist and any functional expectation fails. If the host genuinely lacks the disk/time budget, do not substitute a MiB-scale file and call the gap closed; mark `NOT RUN — RESOURCE LIMIT`, retain the multi-GiB residual risk, and obtain owner acceptance.

### Phase 6 — Failure injection and concurrency

**Objective:** close publish retry, concurrent same-tag, and forced partial commit gaps with deterministic injected faults.

**Stop rule:** stop for a broken tag, a retry with changed request bytes, a hybrid final release, a wrong committed prefix, or a retry that trusts the prior final output.

#### **ID** `FAIL-01`

*Promise*: `P-RETRY-01`, `P-PUB-01`, `P-OPT-01`.

*Setup*: Run `retry-put` in front of `registry:2`, configured to fail the first two manifest PUTs, then succeed. Use one real standard source and collect progress.

*Steps*:

1. Publish through the proxy. Group retry log records by manifest path/body SHA-256.
2. Fetch the resulting tag directly and compare bytes.
3. Repeat in a fresh repository with the first four matching PUTs forced to `503` to exhaust the four-attempt domain; query the tag directly afterward.

*Expected*: success case sends exactly three attempts for the injected PUT, with byte-identical bodies; final publish succeeds; terminal `Progress.Retries` is `2`; result fetch is exact. Exhaustion sends exactly four attempts, fails without a success terminal snapshot, records three begun retries, and leaves the release tag absent (`ErrNotFound` on Fetch). BigOCI operations are not duplicated by this outer retry.

*Evidence to capture*: injection configuration, request/body-hash log, timing, progress, returned digest/error, direct tag result, and output hash.

*Blocker if*: fewer/more attempts occur, request bodies change, progress miscounts, an exhausted publish leaves a tag, or a nested whole-BigOCI retry appears.

#### **ID** `RACE-01`

*Promise*: `P-PUB-01`.

*Setup*: Build two valid releases A and B with distinct bytes, names/versions, and digests. Use one `Client` and one tag `ft/race:current`.

*Steps*:

1. In an external probe, prepare both specs, synchronize two goroutines on a start barrier, and call `Publish` concurrently to the same tag.
2. Wait for both results. Fetch the tag, resolve/fetch it, and compare its digest/content to A and B.
3. Fetch both returned digest references immediately.
4. Repeat 20 times with fresh version values while retaining request logs.

*Expected*: each completed Publish returns a valid, distinct canonical digest; final tag digest is exactly A or B, never another/hybrid index; fetched bytes match that winner; both digest references are immediately valid while retained; no tag ever points at missing manifests/blobs. Which publisher wins is deliberately unspecified.

*Evidence to capture*: barrier/probe source, per-iteration start/end times and digests, tag winner, both pinned-fetch results, hashes, and registry logs.

*Blocker if*: final tag is broken/hybrid, a successful publisher's digest is invalid immediately, data races/panics occur, or content does not match the winning index.

#### **ID** `FAIL-02`

*Promise*: `P-FILES-01`.

*Setup*: Publish an `incus-vm` release whose canonical role order is `disk`, then `metadata`. Map `disk` to writable parent A and `metadata` to writable parent B. Ensure the probe runs as non-root. The progress callback fires synchronously.

*Steps*:

1. Pre-create both final files with distinguishable old marker bytes.
2. Call `FetchFiles(..., ToFiles(...), WithWorkers(2), WithProgress(fn))`. In `fn`, on the first snapshot with `Direction="fetch"`, `Phase="staging"`, and `CompletedFiles==TotalFiles`, chmod parent B to `0500`, then return. This occurs after all staging verification and before `Plan.Commit` begins.
3. Record the returned error and final filesystem before restoring B to `0700`.
4. Corrupt the newly committed disk final deliberately. Retry without chmod fault.

*Expected*: first call returns an error beginning `commit failed; committed roles [disk]; failing role "metadata":` and matches no imgoci sentinel unless the underlying OS error happens to match one (it should not); disk final contains verified new bytes; metadata final remains its old marker because its rename failed; no unverified bytes occupy either final name. After permissions are restored, retry restages and recommits **both** roles: the deliberately corrupted disk is replaced with correct bytes and metadata becomes correct. A successful retry has one terminal commit snapshot.

*Evidence to capture*: callback snapshot/time, permissions, exact error and sentinel matrix, before/after directory trees and hashes, any retained staging after cleanup failure, and retry request/progress logs.

*Blocker if*: the fault occurs before staging, metadata is partially/torn, committed roles/order is wrong, the error omits the prefix/failing role, or retry skips/trusts the corrupted disk.

### Phase 7 — CLI binary

**Objective:** verify the built executable's actual grammar, JSON adapter, streams, progress, reachable exit codes, timeouts, and signals without substituting in-process test seams.

**Stop rule:** stop for stdout contamination, a wrong reachable exit code, unsafe failure text, a command/flag mismatch, or a signal that cannot stop a stalled transfer.

#### **ID** `CLI-01`

*Promise*: `P-CLI-01`.

*Setup*: Use `$FT/bin/imgoci` and an empty `DOCKER_CONFIG`.

*Steps*:

1. Run bare `imgoci`, `help`, `help <each command>`, `version`, top-level help/version aliases, each subcommand `-h`, an unknown command, unknown flag, wrong operand count, negative timeout/progress, zero/negative workers, missing resolve selectors, a flag after an operand, and a dash-leading operand after `--`.
2. Keep stdout/stderr/status separate and compare help flag names with the verified CLI table.

*Expected*: bare command exits `2`, stdout empty, and stderr starts `imgoci: no command given; run "imgoci help" for the commands` followed by unprefixed usage; named help/version exit `0` on stdout; version is exact; usage errors exit `2`, prefix the complaint with `imgoci: `, put usage only on stderr, and never contact the registry; flags after operands are rejected with the documented relocation/`--` guidance.

*Evidence to capture*: command matrix, split streams/statuses, and zero request counts.

*Blocker if*: any documented grammar/flag/stream/status differs.

#### **ID** `CLI-02`

*Promise*: `P-CLI-01`, `P-CLI-02`, `P-DOCKER-01`.

*Setup*: Use a real registry and JSON spec stored in a subdirectory with a relative file path. Include a standard file and a separate BigOCI-only tag. Use `-progress 1ms`, `-workers 2`, and `-timeout 30s` where accepted.

*Steps*:

1. Run publish/list/list filters/list empty/resolve repeated compression/resolve explicit roles/resolve capability/fetch for the standard release; then resolve/fetch BigOCI.
2. Verify all output column counts/order and exact source/output hashes.
3. Add an unknown root member and an unknown file member to separate publish JSON files; add trailing JSON data; omit each required member one at a time.
4. Use the real helper from `AUTH-01` for one `list`, proving automatic Docker credentials.
5. Run a `-timeout 100ms` request through `stall`.

*Expected*: publish stdout is one digest line and nothing on failure; list/resolve are deterministic TSV; list empty exits `0` with empty stdout; fetch stdout is always empty; diagnostics/progress stay on stderr; progress lines match exactly `imgoci: progress <direction> <phase> pct=<n> files=<done>/<total> bytes=<done>/<total> wire=<n> retries=<n> fallbacks=<n> elapsed=<d>` and do not use color or carriage-return rewriting; relative path resolves against the spec directory; unknown/trailing/missing required JSON is usage exit `2` before I/O; helper auth succeeds; timeout fails promptly with `timed out after 100ms:` and exit `1` if no public sentinel is underneath.

*Evidence to capture*: specs, command matrix, TSV field audit, streams/statuses, progress-line parser output, hashes, helper log delta, and timeout timing.

*Blocker if*: data/diagnostics mix, JSON typos are ignored, relative paths use CWD, progress has a different machine shape, credentials are not automatic, or timeout does not cancel.

#### **ID** `CLI-03`

*Promise*: `P-CLI-02`, `P-ERR-01`.

*Setup*: Reuse real releases and netshim modes. Run each command as an OS process and capture `$?` without a pipeline masking it.

*Steps*:

Exercise this reachable matrix:

| Code | Trigger |
|---|---|
| `0` | successful `version` and real list/fetch |
| `1` | malformed reference or `bare-401` |
| `2` | missing required selector |
| `3` | nonexistent tag on a healthy registry |
| `4` | Basic challenge with wrong/anonymous credential |
| `5` | `invalid-index` shim |
| `6` | publish to a digest-only reference or invalid producer spec |
| `7` | fetch where the final `disk.img` path already exists as a directory |
| `8` | `corrupt-blob` on a `none` file |
| `9` | BigOCI-only selection with only standard capability |
| `11` | publish a two-member gzip source declared as gzip |
| `130` | start a stalled transfer, `kill -INT <pid>`, then `wait` |
| `143` | start a stalled transfer, `kill -TERM <pid>`, then `wait` |

For every non-usage failure, require exactly the terminal two-line report after any earlier progress/diagnostic lines. Inject a peer error containing newline, ESC, and invalid UTF-8 through the shim and require visible escapes rather than extra log records/control.

Exit `10` is not externally reachable through the shipped grammar: CLI resolve and fetch always derive the selection from the same fetched `Release`, and no command accepts a serialized `Resolved`. Do not fake this with a modified CLI or repository test. Verify `ErrSelectionMismatch` behavior manually in `ADV-02`, preserve the source-grounded mapping in the evidence, and record the unreachable branch as residual risk.

*Expected*: each reachable row exits exactly as listed; failure stdout follows the command contract; second line names the matched sentinel and exit code, or `no sentinel matched (exit 1)`; first SIGINT/SIGTERM logs cancellation and exits `130`/`143`; a second signal after the first restores the OS default and can force-kill a deliberately wedged shim/client; hostile detail remains one escaped line.

*Evidence to capture*: full trigger/status/streams table, shim logs, signal timestamps, and escaped hostile output bytes (`od -An -tx1`).

*Blocker if*: any reachable code or stream contract differs, signal handling hangs, or hostile text creates a control/log-injection record. Exit `10` remains the explicit manual reachability residual.

### Phase 8 — Release machinery and packaging

**Objective:** prove the actual thing proposed for release is a gettable root library at `0.1.0`, not a CLI or v1 claim, and that repository metadata tells users the truth.

**Stop rule:** stop for a v1 proposal/tag/release, an ungettable root module, a root zip containing `cli/`, a published CLI artifact/version, missing licenses, false security instructions, or a tutorial/status claim inconsistent with the proposed release.

#### **ID** `REL-01`

*Promise*: `P-REL-01`, `P-META-01`.

*Setup*: Authenticated read access to GitHub via `gh`; no mutation.

*Steps*:

```sh
cd "$REPO"
cat release-please-config.json
cat .release-please-manifest.json
sed -n '1,20p' ../spec/spec.md
gh pr view 9 --repo imgoci/go \
  --json number,state,title,isDraft,baseRefName,headRefName,labels,files,url \
  > "$EVIDENCE/REL-01/pr9.json"
gh pr diff 9 --repo imgoci/go > "$EVIDENCE/REL-01/pr9.diff"
gh release list --repo imgoci/go --limit 100 \
  > "$EVIDENCE/REL-01/releases.txt"
git tag --list 'v*' > "$EVIDENCE/REL-01/tags.txt"
```

Inspect `.github/workflows/release-please.yml`: it triggers on `master`, has empty top-level permissions, job-scoped write permissions, pinned action SHAs, uses `IMGOCI_RELEASE_APP_CLIENT_ID` / `IMGOCI_RELEASE_APP_PRIVATE_KEY`, and names both config files.

*Expected*: config has `release-type: go`, `include-v-in-tag: true`, `initial-version: 0.1.0`, pre-major bump guards, and `draft: true`; current manifest is `{ ".": "0.0.0" }`; PR #9 is open, titled `chore(master): release 0.1.0`, labeled `autorelease: pending`, targets `master`, and changes only the release manifest to `0.1.0` plus changelog; spec status is `draft`; there is no GitHub release or `v*` tag, especially no v1.

*Evidence to capture*: files, workflow excerpt, PR JSON/diff, tag/release lists, and spec header.

*Blocker if*: first version/proposal is not `0.1.0`, a v1 exists/proposes while draft, manifest already claims an unreleased version, or release workflow/config targets another unit.

#### **ID** `REL-02`

*Promise*: `P-MOD-01`.

*Setup*: A second, completely clean consumer directory and clean `GOMODCACHE`, `GOCACHE`, and `GOPATH`; no `replace` directive and no reference to `$REPO`.

*Steps*:

```sh
export CLEAN="$FT/consumer-clean"
rm -rf "$CLEAN"
mkdir -p "$CLEAN/home" "$CLEAN/gomodcache" "$CLEAN/gocache" "$CLEAN/app"
cd "$CLEAN/app"
go mod init ft.local/clean-consumer
GOMODCACHE="$CLEAN/gomodcache" GOCACHE="$CLEAN/gocache" GOPATH="$CLEAN/home" \
  GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org \
  go get github.com/imgoci/go@0b4be41
```

Create a small `main.go` importing `github.com/imgoci/go`, calling `New`, `EqualMediaType`, and `ParseIndex` on copied canonical bytes. Then:

```sh
GOMODCACHE="$CLEAN/gomodcache" GOCACHE="$CLEAN/gocache" GOPATH="$CLEAN/home" go build ./...
GOMODCACHE="$CLEAN/gomodcache" GOCACHE="$CLEAN/gocache" GOPATH="$CLEAN/home" go doc -all github.com/imgoci/go \
  > "$EVIDENCE/REL-02/godoc.txt"
GOMODCACHE="$CLEAN/gomodcache" GOPATH="$CLEAN/home" go mod download -json github.com/imgoci/go@0b4be41 \
  > "$EVIDENCE/REL-02/download.json"
ZIP=$(jq -r .Zip "$EVIDENCE/REL-02/download.json")
unzip -l "$ZIP" > "$EVIDENCE/REL-02/zip.txt"
```

Manually verify the downloaded module path/version/sums, public godoc inventory, license files, and zip contents.

*Expected*: `go get` resolves the exact commit to a Go pseudo-version whose suffix begins with `0b4be41`; checksum verification succeeds; build exits `0`; module path is `github.com/imgoci/go`; Go directive is `1.26.5`; `go doc -all` matches the inventoried API; the zip contains root source, README, and both license files, but no nested `cli/` module.

*Evidence to capture*: environment values, initial/final directory listing, `go.mod`, `go.sum`, `go env`, build streams/status, download JSON, zip listing, and godoc.

*Blocker if*: acquisition requires the local checkout, uses a different module path/commit, fails checksum/build, omits licenses, or includes `cli/`.

#### **ID** `REL-03`

*Promise*: `P-REL-02`.

*Setup*: Reuse only the clean network/cache environment; use an empty `GOBIN`.

*Steps*:

```sh
mkdir -p "$FT/no-cli-bin"
GOBIN="$FT/no-cli-bin" GOMODCACHE="$FT/consumer-clean/gomodcache-cli" \
  GOPROXY=https://proxy.golang.org,direct \
  go install github.com/imgoci/go/cli@0b4be41 \
  > "$EVIDENCE/REL-03/install.stdout" 2> "$EVIDENCE/REL-03/install.stderr"
printf '%s\n' "$?" > "$EVIDENCE/REL-03/install.exit"
go list -m -versions github.com/imgoci/go/cli \
  > "$EVIDENCE/REL-03/versions.txt" 2>&1 || true
```

Inspect `cli/go.mod`, the root module zip from `REL-02`, PR #9's file list, `.github/workflows`, and GitHub release assets.

*Expected*: `cli/go.mod` is module `github.com/imgoci/go/cli`, requires root `v0.0.0`, and has `replace github.com/imgoci/go => ../`; clean `go install .../cli@0b4be41` is nonzero and creates no `imgoci` binary; no CLI module version or release asset exists; root zip excludes `cli/`; PR #9 versions only the root library. Building from an exact checkout remains successful (`DOC-01`/`CLI-01`).

*Evidence to capture*: module file, install streams/status, empty `GOBIN`, version query, zip/PR/workflow/release-asset excerpts.

*Blocker if*: the CLI is installable/versioned/published as part of this release, appears in the root zip, or release machinery creates a CLI artifact.

#### **ID** `REL-04`

*Promise*: `P-META-01`.

*Setup*: Read-only repository and GitHub access. The repository owner must confirm the legal copyright holder/year.

*Steps*:

1. Compare README status/spec/license links with `doc.go`, docs landing page, `go.mod`, the spec header, and the proposed version.
2. Confirm `LICENSE-APACHE` is the complete Apache License 2.0 text and `LICENSE-MIT` contains `Copyright (c) 2026 Joshua Gilman`, the MIT grant, notice condition, and warranty disclaimer. Confirm README offers either license at the user's option and both ship in the module zip.
3. Check GitHub facilities without mutation:

   ```sh
   gh repo view imgoci/go --json hasIssuesEnabled,url > "$EVIDENCE/REL-04/repo.json"
   gh api -i repos/imgoci/go/private-vulnerability-reporting \
     > "$EVIDENCE/REL-04/private-vuln-reporting.txt" 2>&1 || true
   ```

4. Read `SECURITY.md` as user instructions, not as a template. Follow its private-reporting route up to (but not including) submitting a report. Confirm no public channel is needed.
5. Compare `CONTRIBUTING.md` task names with actual `moon.yml`, `cli/moon.yml`, `docs/moon.yml`, and the tool pins in `mise.toml`. Do **not** execute `root:check`; automated gates are assumed green.
6. Confirm the contributing release guard and private-CLI statement match `REL-01` and `REL-03`.

*Expected*: README accurately says pre-v1/draft and links both complete licenses; owner/year are correct; issues are enabled for non-security bugs; private vulnerability reporting is enabled and reachable; `SECURITY.md` contains final user-facing policy, not authoring instructions or placeholders; contributor commands `moon run root:check`, `root:format`, `root:lint`, `root:build`, and `root:test` exist, and `mise install` provisions their pinned tools; release guard and CLI status agree everywhere.

At commit `0b4be41`, text such as “Do not claim support windows…”, “If the project has… add it here”, or similar template directives in `SECURITY.md` must be treated as a failed expected result, not as policy. The file's first sentence also requires the GitHub feature actually to be enabled.

*Evidence to capture*: README/security/contributing/license excerpts and hashes, owner confirmation, GitHub responses/statuses, task/pin cross-check table, and module-zip license listing.

*Blocker if*: vulnerability reporting is disabled/unreachable while claimed, template instructions remain in the published security policy, bug issues are disabled while promised, license choice/files/owner are inaccurate, or contributor/release commands and pins do not exist as documented.

## Residual Risk

- **CLI exit `10`:** `ErrSelectionMismatch` is proven through the root API in `ADV-02`, and the source-grounded CLI table maps it to `10`, but the shipped command grammar cannot construct a mismatched `Release`/`Resolved` pair. Altering the binary to reach the branch would cease to be a public-surface manual test. For `0.1.0`, the residual is limited to that otherwise unreachable mapping branch; codes `0`–`9`, `11`, `130`, and `143` are exercised through the real binary.
- **Multi-GiB resource exception:** `BIG-02` is designed to run on a developer laptop with at least 15 GiB free. If that real budget is unavailable, the plan records the gap rather than substituting a small file. The residual is multipart streaming behavior above the MiB-scale already proven by Session 004/`BIG-01`; owner acceptance is required.
- **Power loss and hardware/filesystem durability:** `FAIL-02` forces a real rename permission failure at the commit boundary and proves committed-prefix/retry behavior. It does not pull power between fsync and rename, exhaust an APFS volume mid-syscall, or prove behavior on every network filesystem. Per-file atomicity—not cross-file transactionality—is the stated `0.1.0` promise.
- **Windows runtime:** the manual campaign runs on Darwin arm64. It does not execute Windows no-follow, locking, rename, or home-directory behavior; the assumed automated gate only cross-compiles Windows. This remains acceptable for a pre-v1 first release if no README/docs claim a separately validated Windows support tier.
- **Public cloud registry matrix:** local zot v2.1.20 and the captured `registry:2` digest cover two real Distribution implementations plus real custom auth/TLS/proxy boundaries. The campaign does not spend real credentials on GHCR, Docker Hub, ECR, GCR, or cloud-specific OAuth quirks. Docker Hub legacy-key mapping is exercised with logical-host/dial separation in `AUTH-01`.
- **Real OS keychains:** the external helper protocol, fresh lookup, timeout, cancellation, Docker Hub key, and redaction are tested with executable helper processes. The campaign does not read a developer's macOS Keychain or other real credential store, by design.
- **Registry garbage collection:** digest identity and immediate digest retrieval are tested, but long-term retention after tag movement is registry policy, not a library guarantee. The tutorial now states that qualification.
- **Concurrency semantics:** `RACE-01` requires a complete A-or-B winner. It does not promise which publisher wins, fairness, compare-and-swap, or tag locking; the public API makes no such promise.
- **Scale extremes outside the promise:** the plan does not allocate a 2^63−1 content, a 2^53−1 manifest, or 4096 full-size parts. It tests the practical multi-GiB path and the 4097-part rejection boundary without pretending to materialize impossible laptop-scale data.
- **Spec non-goals:** signatures, attestation/trust, tag discovery, version ordering, catalogs, deltas, sparse restoration, image conversion/import/boot, revocation, and general OCI image tooling are explicitly outside spec §1 and the project documentation; omitting them is not a coverage gap.

## Execution Notes

Start with `CM-01` and `DOC-01`; each yields a useful release-readiness signal within minutes. `DOC-02` can build/render in parallel with `CM-01` after the exact checkout and tool pins are recorded. Phase 2 should run sequentially because its published fixtures become inputs to Phase 3. The local CA, helper scripts, token realm, and netshim modes are independent scaffolding and may be prepared in parallel, but run each network scenario against unique ports/logs so evidence is attributable. Do not overlap `BIG-02` with other disk/network-heavy scenarios. Run `RACE-01` only after the retry proxy is disabled so the intended race is the only fault. Run CLI exit scenarios last among behavioral work so all needed hostile endpoints/releases already exist. Release/package checks are independent of registry state and can run while large-file hashes are being computed, except do not change the checkout or module caches used for their evidence.

Expected hands-on runtime after images/tools are present is about 20–30 minutes for Phase 1, 30–45 minutes for Phases 2–3, 35–60 minutes for TLS/auth/redirect work (including the deliberate 10-second helper timeout), 15–25 minutes for `BIG-01`, 30–90 minutes for `BIG-02` depending on Docker storage, 25–40 minutes for failure/concurrency work, 25–40 minutes for the CLI matrix, and 15–25 minutes for release/package review. First-time image/module pulls add network time.

After evidence is complete:

```sh
docker rm -f imgoci-zot imgoci-ft-dist imgoci-ft-tls 2>/dev/null || true
pkill -f "$FT/bin/netshim" 2>/dev/null || true
# Preserve $EVIDENCE until owner review; remove only generated payloads/caches.
rm -rf "$FT/work" "$FT/consumer-local" "$FT/consumer-clean" "$FT/no-cli-bin" "$FT/pki"
cd "$REPO"
test "$(git rev-parse --short=7 HEAD)" = 0b4be41
test -z "$(git status --porcelain)"
```

Do not delete `$EVIDENCE` until the owner has signed the verdict. The final execution report should contain only: the exact state/tool/image identities, a scenario result table, blocker details with reproduction evidence, the six known non-blocking re-check outcomes, accepted residual risks, and the final `READY` or `NOT READY` verdict.
