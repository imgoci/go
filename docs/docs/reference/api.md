---
title: API reference
description: Public API of the imgoci Go library, grouped by surface.
---

# API reference

Everything public lives in one package: `github.com/imgoci/go`, imported as
`imgoci`. This page describes the implemented spec revision: imgoci v1 draft,
2026-08-16 (`imgoci/spec` commit `46d18b74cc407ac7d61ded7692fc42b644f4d1e2`).
The library is pre-v1; the API is not yet stable.

Generated documentation is also available on
[pkg.go.dev](https://pkg.go.dev/github.com/imgoci/go). Error classification is
in the [error reference](errors.md); capability semantics are in the
[capabilities reference](capabilities.md).

## Offline index

These functions run without a network. `ParseIndex` is the only way to obtain
a populated, validated `Index`. A zero `Index` is constructible and is not a
validated document.

```go
func ParseIndex(b []byte) (*Index, error)
```

Fully validates release-index bytes: JSON decode with duplicate-key rejection,
the ten consumer rules of spec section 6 including canonical descriptor order
(rule 9), and canonical bytes (rule 10). It never re-encodes for identity: the
returned `Index` records the SHA-256 digest of the input bytes. Any failure
wraps `ErrInvalidIndex`.

```go
type Index struct{ /* unexported */ }

func (x *Index) Digest() digest.Digest
func (x *Index) Name() string
func (x *Index) Version() string
func (x *Index) Entries() []FileEntry
func (x *Index) Annotations() map[string]string
```

An `Index` is an immutable view. `Digest` is the SHA-256 of the canonical
input bytes and is the identity of the encoded release. `Name` returns the
`io.imgoci.name` root annotation; `Version` returns
`org.opencontainers.image.version`. `Entries` returns file entries in
canonical descriptor order; `Annotations` returns the root annotation map
including unknown keys. Both copy the slice and every map freshly on every
call, so mutating a returned value cannot change the index.

```go
type Selector struct {
	Architecture   string // io.imgoci.architecture
	Target         string // io.imgoci.target
	Representation string // io.imgoci.representation
	Usage          Usage  // io.imgoci.usage; zero is the empty set
	Role           string // io.imgoci.role
	Compression    string // io.imgoci.compression
}

type FileEntry struct {
	MediaType     string            // descriptor mediaType, as written
	ArtifactType  string            // file-manifest type, as written; capability metadata
	Digest        digest.Digest     // SHA-256 of the referenced file manifest
	Size          int64             // byte length of the referenced file manifest
	Selector      Selector          // six-field file-entry identity
	ContentDigest digest.Digest     // SHA-256 of the decoded content
	ContentSize   int64             // byte length of the decoded content
	Filename      string            // io.imgoci.filename
	Annotations   map[string]string // every descriptor annotation, including unknown keys
}
```

### Usage sets

```go
type Usage struct{ /* unexported */ }

func NewUsage(values ...string) (Usage, error)
func (u Usage) String() string
func (u Usage) Values() []string
```

`Usage` is an immutable, comparable set of usage tokens. Its zero value and
`NewUsage()` both represent the empty set. Values built with `NewUsage` can be
compared with `==` or used as map keys.

`NewUsage` accepts tokens in any order, sorts them in ascending UTF-8 byte
order, and removes duplicates. Each token must be a spec section 5.3 basic
token, and the canonical comma-separated value must not exceed 4096 bytes. It
rejects a set that contains `install-offline` without `install`. It does not
check registry membership; `Client.Publish` applies that producer rule as
described in the [usage registry](#usage-registry).

`String` returns the canonical comma-separated form, or `""` for the empty
set. `Values` returns a freshly allocated sorted slice, or nil for the empty
set.

Selector values compare exactly and case-sensitively (spec section 5.3).
`MediaType` and `ArtifactType` are preserved as written; compare them with
`EqualMediaType`:

```go
func EqualMediaType(a, b string) bool
```

Reports whether `a` and `b` identify the same parameter-free media type under
spec section 4: ASCII case-insensitive comparison, no allocation.

## Selection

`List` and `Resolve` are pure functions over a validated `Index`.

```go
type ListQuery struct {
	Architecture   string   // exact filter; "" matches every architecture
	Target         string   // exact filter; "" matches every target
	Representation string   // exact filter; "" matches every representation
	Usage          []string // nil: no filter; non-nil: required contained values
	Roles          []string // nil: no role filter; non-nil must be non-empty
}

func (x *Index) List(q ListQuery) ([]Deliverable, error)
```

Returns every deliverable that matches `q`, sorted per spec section 7.2:
deliverables by architecture, target, representation, then exact usage set;
roles and alternatives in ascending UTF-8 byte order. An empty result is
valid. Listing does not filter by consumer capabilities.

A nil `ListQuery.Usage` applies no usage filter and matches every usage set. A
non-nil slice must be non-empty and duplicate-free; order is irrelevant. A
deliverable matches when its usage set contains every requested value. A
non-nil `Roles` names every role a matching deliverable must contain.

`List` validates `q` before it inspects any entry; see [query validation
ordering](#query-validation-ordering).

```go
type Deliverable struct {
	Architecture   string
	Target         string
	Representation string
	Usage          Usage // the deliverable's exact usage set
	Roles          []DeliverableRole
}

type DeliverableRole struct {
	Role         string
	Alternatives []TransportAlternative
}

type TransportAlternative struct {
	Compression  string // io.imgoci.compression
	ArtifactType string // declared file-manifest type
}
```

```go
type ResolveQuery struct {
	Architecture   string       // required exact selector
	Target         string       // required exact selector
	Representation string       // required exact selector
	Usage          []string     // complete exact set; nil and empty mean the empty set
	Roles          []string     // nil: default-role rule; non-nil must be non-empty
	Compressions   []string     // required; most preferred first; no duplicates
	Capabilities   Capabilities // Index.Resolve zero: StandardCapabilities; Client.Resolve zero: Client.Capabilities
}

func (x *Index) Resolve(q ResolveQuery) (*Resolved, error)
```

Selects one deliverable per spec section 7.3. `ResolveQuery.Usage` is the
complete requested usage set and is compared by exact set equality. Order is
irrelevant and duplicates are invalid. Nil and an empty slice both request the
empty usage set. A resolve query with no usage does not match a deliverable
that carries any usage value.

Selection is atomic: each step completes for every selected role before the
next starts, and any failure returns a nil result with no partial entries.
`Compressions` accepts only the v1 spec tokens `none`, `gzip`, `xz`, and
`zstd`. Only the capability filter wraps `ErrUnsupportedType`; the other
selection failures are deliberately sentinel-free (see the [error
reference](errors.md#offline-resolve-failures)).

`Resolve` validates `q` before it inspects any entry; see [query validation
ordering](#query-validation-ordering).

```go
type Resolved struct{ /* unexported */ }

func (r *Resolved) Entries() []FileEntry
func (r *Resolved) IndexDigest() digest.Digest
```

`Entries` returns the selected file entries, freshly copied on every call.
`IndexDigest` is the SHA-256 of the canonical index bytes the selection was
derived from; `Client.FetchFiles` binds by that digest, not by pointer
identity.

### Query validation ordering

Spec section 7.1 requires a consumer to validate the query before fetching the
release. This API deviates deliberately, because it is fetch-once,
query-many: `Client.Fetch` receives only a `Reference` and no query, and a
query first reaches the library at `Index.List`, `Index.Resolve`, or
`Client.Resolve`. Each of those validates the query in full before it inspects
a single index entry — validation happens at the first method that receives
the query, but necessarily after the caller's explicit fetch. `Fetch` performs
no prefetch query validation.

The consequence is one wasted manifest round trip for an invalid query. An
invalid query is still rejected before any entry is examined, so it never
produces a wrong result — only a late one.

## Client and options

```go
type Reference string
```

Names one release: `registry/repo[:tag][@sha256:...]`, parsed with
`github.com/distribution/reference`. The registry is required (no short-name
expansion), the name must be lowercase, a digest must be `sha256`, and a
reference carrying both a tag and a digest is bound to its digest. A malformed
reference is a caller error without a sentinel.

```go
type Client struct{ /* unexported */ }

func New(opts ...Option) (*Client, error)

type Option func(*clientSettings) // clientSettings is unexported; the option set is sealed
```

`New` reports an error in one case: `WithDockerCredentials` was named and a
Docker configuration file exists but cannot be read as a Docker
configuration. That covers a read failure and a parse failure. A missing
file, or a machine with no home directory and no `$DOCKER_CONFIG`, is not
an error; every registry then resolves to the anonymous credential. A
client built with no credential option still performs the full token
exchange, anonymously.

```go
func WithHTTPClient(client *http.Client) Option
func WithPlainHTTP() Option
func WithDockerCredentials() Option
func WithCredentials(username, secret string) Option
func WithUnverifiedExternalTransport() Option
```

- `WithHTTPClient` sends every registry request with `client`. A nil client is
  ignored.
- `WithPlainHTTP` talks `http://` to the registry instead of `https://`.
  Everything, credentials included, rides unencrypted; local registries only.
- `WithDockerCredentials` uses the credentials `docker login` stores:
  `$DOCKER_CONFIG/config.json` where set, `.docker/config.json` under the home
  directory otherwise. The file is read once, by `New`; credential helpers it
  names are asked afresh at every lookup. See
  [Use Docker credentials](../how-to/use-docker-credentials.md).
- `WithCredentials` presents `username` and `secret` to whatever registry a
  transfer dials. Naming both credential options leaves the last one in
  effect.
- `WithUnverifiedExternalTransport` authorizes an opaque `http.RoundTripper`
  as the storage transport for registry-selected cross-host blob traffic. It
  never disables TLS verification.

```go
func (c *Client) Capabilities() Capabilities
```

Reports what the built client can retrieve conformingly: the standard
file-manifest type plus `application/vnd.bigoci.file.v1`. See the
[capabilities reference](capabilities.md).

```go
func (c *Client) Fetch(ctx context.Context, ref Reference) (*Release, error)
```

Retrieves and fully validates the release index `ref` names. The reference
must include a tag, a digest, or both; name-only references are a caller
error. Fetch requires the response `Content-Type` to identify the
release-index type, hashes the original bytes, checks a digest pin when the
reference named one, and runs `ParseIndex`.

`Fetch` takes no query and therefore validates none; see [query validation
ordering](#query-validation-ordering) for the deviation from spec section 7.1
that this fetch-once, query-many shape implies.

```go
type Release struct{ /* unexported */ }

func (r *Release) Digest() digest.Digest // equals r.Index().Digest()
func (r *Release) Index() *Index
```

A `Release` is a fetched, fully validated index pinned to one repository. It
is immutable and safe for concurrent use. Later `FetchFiles` calls address the
same host and repository and name file manifests by digest, so a tag mutation
after `Fetch` cannot redirect retrieval.

```go
func (c *Client) Resolve(rel *Release, q ResolveQuery) (*Resolved, error)
```

Identical to `Index.Resolve` except a zero `q.Capabilities` defaults to
`Client.Capabilities`, so selection can never outrun retrieval.

## Fetching files

```go
type FetchOption interface{ /* sealed */ }

func (c *Client) FetchFiles(
	ctx context.Context,
	rel *Release,
	sel *Resolved,
	dest Dest,
	opts ...FetchOption,
) error
```

Retrieves and verifies every entry in `sel` into `dest`. Preconditions run
before any adapter construction or network I/O:

| Precondition | Failure |
|---|---|
| `sel.IndexDigest() == rel.Digest()` | `ErrSelectionMismatch` |
| every selected entry's `ArtifactType` is in `Client.Capabilities` | `ErrUnsupportedType` |
| `dest` maps onto the selected roles | `ErrInvalidDest` |

Every output is staged and verified privately first; commit runs only after
all roles verify. See [About the architecture](../explanation/architecture.md)
for the stage-then-commit contract.

```go
func WithProgress(fn func(Progress)) progressOption
func WithWorkers(n int) workersOption
```

Both return unexported option types that satisfy `FetchOption` and
`PublishOption`, so the same option works on `FetchFiles` and `Publish`.
`WithProgress` delivers serialized absolute snapshots to `fn`; a nil `fn` is
ignored. `WithWorkers` moves `n` selected files at once; `n` must be positive,
checked before any I/O. Omitted, the orchestrator default is four workers.

## Destinations

```go
type Dest struct{ /* unexported */ }

func ToDir(path string) Dest
func ToFiles(byRole map[string]string) Dest
```

`Dest` is a path-backed destination built only by these two constructors. The
zero value is invalid (`ErrInvalidDest`). `ToDir` names each selected file by
joining `path` with the entry's `io.imgoci.filename`. `ToFiles` names each
file from a map keyed by `io.imgoci.role`; the map must contain every selected
role and nothing else, and it is cloned at construction so later mutation
cannot race preflight.

`ToDir` reserves `.imgoci-stage` beneath each destination parent for private
working state. A standard fetch removes its per-call workspace when cleanup
succeeds. A BigOCI fetch also creates `.imgoci-stage/stored` for its
content-addressed stored-file cache. Successful commit removes the cache
entries and lock files, but the empty `.imgoci-stage/stored/` directory
remains. Treat `.imgoci-stage` as library-owned working state, not as a
fetched release file.

## Publishing

```go
type ReleaseSpec struct {
	Name        string            // 1–128-byte io.imgoci.name basic token
	Version     string            // 1–128 printable ASCII characters; no whitespace or controls
	Annotations map[string]string // extra root annotations; io.imgoci.* keys rejected
	Files       []FileSpec
}

type FileSpec struct {
	Source      Source            // must not change during Publish
	Selector    Selector          // Compression declares what Source already is
	Filename    string            // io.imgoci.filename
	Annotations map[string]string // extra descriptor annotations; io.imgoci.* keys rejected
	Multipart   *MultipartSpec    // nil selects the standard form
}

type MultipartSpec struct {
	PartSize int64 // bytes; 0 means the bigoci default (512 MiB); negative is ErrInvalidSpec
}
```

`ReleaseSpec.Name` is `io.imgoci.name`. It must be a basic token: 1 to 128
ASCII bytes matching `^[a-z0-9]+([._-][a-z0-9]+)*$` (spec sections 5.1 and
5.3). `ReleaseSpec.Version` is `org.opencontainers.image.version`. It must
contain 1 to 128 printable ASCII characters and must not contain whitespace
or control characters (spec section 5.1).

### Usage registry

`Client.Publish` enforces the spec section 5.4 public usage registry for every
token in `FileSpec.Selector.Usage`:

| Value | Meaning |
|---|---|
| `live` | The deliverable can boot and run an OS session without first installing the release on persistent storage. |
| `install` | The deliverable can install the release on persistent storage separate from the source used to run the installer. |
| `install-offline` | The deliverable can complete the producer-defined baseline installation while network connectivity is unavailable. |

A producer-defined usage value must use the private
`x-<owner>-<name>` form. `Publish` rejects a syntactically valid bare value
outside the public registry before network I/O, and the error wraps
`ErrInvalidSpec`.

This producer/consumer asymmetry is deliberate. Spec sections 6 and 12 make
public selector naming a producer-only rule. `Publish` checks registry
membership, while `ParseIndex`, `Fetch`, `List`, and `Resolve` accept every
syntactically valid usage value, including unknown and private values, and
preserve or compare those values exactly. `NewUsage` is also registry-neutral:
it validates a usage set, not producer conformance.

Usage values are producer assertions. Validation and retrieval do not run the
deliverable or prove that it has the asserted behavior.

```go
type PublishOption interface{ /* sealed */ }

func (c *Client) Publish(
	ctx context.Context,
	ref Reference,
	spec ReleaseSpec,
	opts ...PublishOption,
) (digest.Digest, error)
```

Publishes `spec` as an imgoci release at `ref` and returns the canonical index
digest. Publish is tag-only: digest-only, tag+digest, and name-only references
are `ErrInvalidSpec` before any I/O. Spec validation (producer rules 1–8,
`Name` and `Version` grammar, UTF-8 of every caller string, reserved
`io.imgoci.*` keys, selector and filename grammar, public selector registries,
duplicate six-field tuples, required roles, filename collisions, and
shared-source consistency) also runs before any network I/O. A multipart
plan must satisfy `ceil(storedSize/effectivePartSize) <= 4096`, where a
zero `PartSize` uses 512 MiB; a plan above that ceiling is
`ErrInvalidSpec` before any I/O. The index is
written last, so an interrupted publish never leaves a broken artifact behind
a tag.

```go
type Source struct{ /* unexported */ }

func FromFile(path string) Source
```

`Source` is a path-backed stored file built only by `FromFile`. A `Source`
must not change during `Publish`; that is a caller precondition.
Defense-in-depth detects most violations: pass 1 captures size and mtime and
re-checks them before upload, and the standard-path upload reader re-hashes
the bytes actually streamed and fails the push on divergence from pass 1.
Compression is declared on the surrounding `FileSpec.Selector`, never inferred
from the path.

## Capabilities

```go
type Capabilities struct{ /* unexported */ }

func NewCapabilities(types ...string) (Capabilities, error)
func StandardCapabilities() Capabilities
```

A validated set of file-manifest types a consumer can retrieve. A zero value
means `StandardCapabilities` in `Index.Resolve` and `Client.Capabilities()`
in `Client.Resolve`. Validation rules and
comparison semantics are in the [capabilities reference](capabilities.md).

## Progress

```go
type Progress struct {
	Direction      string // "fetch" or "publish"
	Phase          string // fetch: "staging", "commit"; publish: "hashing", "upload", "index"
	TotalFiles     int    // number of selected entries
	CompletedFiles int    // entries fully verified
	TotalBytes     int64  // sum of ContentSize across selected entries
	CompletedBytes int64  // sum of ContentSize of verified entries
	WireBytes      int64  // raw standard-path bytes plus each BigOCI transfer's latest WireBytes
	Retries        int    // standard-path attempts after the first that actually begin, plus each BigOCI transfer's latest Retries
	Fallbacks      int    // unique blobs that requested multipart and used the standard path; zero on fetch
}
```

Snapshots are absolute and serialized; a mutex in the orchestrator orders
every emit. `TotalFiles` and `TotalBytes` are fixed up front (on publish, after
pass 1). The counting fields only increase. `Retries` counts standard-path
attempts after the first that actually begin; cancellation during backoff
does not count. A standard retry change emits immediately. A standard
`WireBytes` change is folded into a later snapshot rather than emitted per
chunk. Repeated snapshots from one BigOCI transfer replace that transfer's
contribution to `WireBytes` and `Retries`; they are never summed. A
successful `FetchFiles` emits exactly one terminal
snapshot with `Phase` `"commit"` after commit. A successful `Publish` emits
exactly one terminal snapshot with `Phase` `"index"` after the tag PUT.
Failure emits no terminal snapshot.

A `WithProgress` callback runs on the transfer's
goroutines and must store or print and return; blocking work belongs in a
render loop outside the callback.
