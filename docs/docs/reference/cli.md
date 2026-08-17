---
title: CLI reference
description: Commands, flags, streams, and exit codes of the private reference CLI.
---

# CLI reference

`imgoci` is a **private reference command-line interface**: verification
tooling, never published, never released, never versioned. It exists so a
human can watch the library work against a real registry. Nothing in the
repository publishes it, and the local `replace` directive in `cli/go.mod`
makes installing it from a module proxy impossible on purpose. Build it from
the `cli/` directory of a checkout.

Transfer and selection flags map onto public library options or query
fields (see the [API reference](api.md)). `-timeout` is not a library
option: it is the CLI command-context deadline. There is no
transfer logic in the CLI, no retry, resume, or authentication logic of
its own. This page describes the implemented spec revision: imgoci v1
draft, 2026-08-11 (`imgoci/spec` commit
`5b957102eeda16498fdcb80a738431b83abd4197`).

## Commands

```
imgoci publish [flags] <spec> <ref>
imgoci list    [flags] <ref>
imgoci resolve [flags] <ref>
imgoci fetch   [flags] <ref> <dest>
imgoci help    [publish|list|resolve|fetch|version]
imgoci version
```

Flags come before operands. A flag written after an operand is rejected with a
usage error; an operand that really begins with a dash goes after `--`.
Running `imgoci` with no arguments is a usage error: it writes
`no command given; run "imgoci help" for the commands` and the top-level
usage block to stderr, writes nothing to stdout, and exits `2`.
`imgoci help` and `imgoci version` asked for by name write to stdout and
exit `0`. `imgoci version` prints `imgoci (private reference CLI)` — the CLI
is unreleased, so the line names the tool rather than a version number.

A flag that was not set passes nothing to the library, so the library's own
default applies and the CLI never restates it.

## Credentials

Docker credentials are always on. There is no flag for them: log in with
`docker login`, then run the tool. A run that must not use credentials points
`DOCKER_CONFIG` at an empty directory. See
[Use Docker credentials](../how-to/use-docker-credentials.md).

## Shared flags

| Flag | Type | Default when unset | Meaning |
|---|---|---|---|
| `-plain-http` | bool | `https://` | Talk `http://` to the registry. For local registries. |
| `-timeout` | duration (`30s`, `5m`) | no limit | Give up after this long. Negative values are usage errors. |

`publish` and `fetch` — the commands that move files — additionally declare:

| Flag | Type | Default when unset | Meaning |
|---|---|---|---|
| `-workers` | int | library default, 4 | How many files to move at once. An explicit nonpositive value is rejected before any network I/O as a usage error (exit `2`): `<cmd>: -workers must be positive, got %d`. |
| `-progress` | duration (`5s`, `500ms`) | no progress output | Print a progress line this often. Negative values are usage errors. |

## imgoci publish

```
imgoci publish [flags] <spec> <ref>
```

Reads the JSON publish spec at `<spec>`, publishes it at the tag-only
reference `<ref>`, and writes the canonical index digest to stdout on a line
of its own. On failure it writes nothing to stdout. Flags: the shared set plus
`-workers` and `-progress`.

### Publish spec

`<spec>` is a JSON document that maps losslessly onto `imgoci.ReleaseSpec`.
Unknown members are rejected so a typo cannot drop a field. Relative file
paths are resolved against the directory that contains `<spec>`.

```json
{
  "name": "example",
  "version": "1",
  "annotations": {"note": "root"},
  "files": [
    {
      "path": "disk.qcow2",
      "filename": "disk.qcow2",
      "architecture": "amd64",
      "target": "qemu",
      "representation": "qcow2",
      "role": "disk",
      "compression": "none",
      "annotations": {"note": "file"},
      "multipart": {"partSize": 16777216}
    }
  ]
}
```

| Member | Required | Meaning |
|---|---|---|
| `name` | yes | `io.imgoci.name`. A basic token: 1 to 128 ASCII bytes matching `^[a-z0-9]+([._-][a-z0-9]+)*$`. |
| `version` | yes | `org.opencontainers.image.version`. 1 to 128 printable ASCII characters; no whitespace or control characters. |
| `annotations` | no | Extra root annotations. `io.imgoci.*` keys are reserved by the library. |
| `files` | yes | The stored files to publish. |
| `files[].path` | yes | Filesystem path of the stored file; relative paths resolve against the spec's directory. |
| `files[].filename` | yes | `io.imgoci.filename`. 1–255 bytes, ASCII alphanumeric first and last, ASCII alphanumerics plus `.`, `_`, `+`, `-` internally. |
| `files[].architecture`, `target`, `representation`, `role`, `compression` | yes | The five selector fields. `compression` declares what `path` already is. |
| `files[].annotations` | no | Extra descriptor annotations. `io.imgoci.*` keys are reserved. |
| `files[].multipart` | no | Omitted or `null` selects the standard form; a present object requests BigOCI publication. |
| `files[].multipart.partSize` | no | Part size in bytes. Must not be negative. `0` uses the library default (512 MiB) as the effective part size. A multipart plan must satisfy `ceil(storedSize/effectivePartSize) <= 4096`. |

## imgoci list

```
imgoci list [flags] <ref>
```

Fetches the release index `<ref>` names and writes every matching deliverable
to stdout. Flags: the shared set plus optional filters.

| Flag | Meaning |
|---|---|
| `-architecture` | Exact architecture filter (unset: match every architecture). |
| `-target` | Exact target filter (unset: match every target). |
| `-representation` | Exact representation filter (unset: match every representation). |
| `-role` | Require this role; repeat to require several (unset: no role filter). |

Output: one line per stored transport alternative, tab-separated, in the order
`imgoci.Index.List` already sorts:

```
<architecture>	<target>	<representation>	<role>	<compression>	<artifactType>
```

An empty match prints nothing and exits `0`.

## imgoci resolve

```
imgoci resolve [flags] <ref>
```

Fetches the release index, selects one deliverable, and writes each selected
role to stdout. `-architecture`, `-target`, `-representation`, and at least
one `-compression` are required and are checked before any network I/O.

| Flag | Required | Meaning |
|---|---|---|
| `-architecture` | yes | Exact architecture selector. |
| `-target` | yes | Exact target selector. |
| `-representation` | yes | Exact representation selector. |
| `-compression` | at least one | Accepted compression, most preferred first; repeat to accept several. |
| `-role` | no | Select this role; repeat to select several (unset: the default-role rule). |
| `-capability` | no | Consumer file-manifest type; repeat to accept several (unset: the client's capabilities). A set that is given must include `application/vnd.imgoci.file.v1`. |

Output: one line per selected role, tab-separated, in
`imgoci.Resolved.Entries` order:

```
<architecture>	<target>	<representation>	<role>	<compression>	<filename>	<artifactType>	<contentDigest>	<contentSize>
```

## imgoci fetch

```
imgoci fetch [flags] <ref> <dest>
```

Fetches the release index, selects one deliverable, and writes the verified
files into directory `<dest>`, named by `io.imgoci.filename`. Stdout stays
empty whether the fetch succeeds or fails. Flags: everything `resolve` takes
plus `-workers` and `-progress`. The same selectors are required.

## Output contract

For `publish`, `list`, `resolve`, and `fetch`, standard output carries
machine data only:

- `publish` writes the canonical index digest and a newline; nothing on
  failure. A failed write of that digest is a command failure.
- `list` and `resolve` write the deterministic tab-separated listings above.
- `fetch` writes nothing either way.

`imgoci help` and `imgoci version` asked for by name write to stdout and
exit `0`. A bare `imgoci` invocation writes
`no command given; run "imgoci help" for the commands` and the top-level
usage block to stderr, writes nothing to stdout, and exits `2`.

Diagnostics, progress, failure summaries, and usage complaints go to
standard error, each prefixed `imgoci: `. Usage blocks are unprefixed.
Progress and diagnostics share one serialized writer. There is no terminal
detection, no color, and no line rewriting, so the output is byte-identical
piped and interactive.

## Progress

With `-progress <interval>`, `publish` and `fetch` print one stderr line per
interval, every field present every time:

```
imgoci: progress <direction> <phase> pct=<n> files=<done>/<total> bytes=<done>/<total> wire=<n> retries=<n> fallbacks=<n> elapsed=<d>
```

The library's callback only stores a snapshot; a goroutine on its own clock
renders the latest one, so progress output never slows the transfer. When the
transfer ends, the latest snapshot is printed only if at least one library
snapshot arrived; otherwise none.

## Failure output

A non-usage failure ends with a two-line report on stderr. Earlier
diagnostics and progress lines, and a signal notice if one arrived, may
precede it. The first report line preserves the library's error text, with
every non-graphic rune visibly escaped so peer-controlled detail cannot
create another log record or terminal control. The second takes one of three
forms: the sentinel `errors.Is` matched and the exit code it maps to, the
statement that none matched, or the signal that stopped the run
(`interrupted by SIGINT (exit 130)` or `terminated by SIGTERM (exit 143)`).

A usage error prints its prefixed complaint and the offending command's
unprefixed usage block, and exits `2`.

## Signals

The first `SIGINT` or `SIGTERM` cancels the running transfer, which unwinds on
its own; the signal disposition is then reset, so a second signal kills the
process outright. A recorded signal outranks the error's shape when the exit
code is chosen.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | Failure, no sentinel matched. |
| `2` | Usage error. |
| `3` | `errors.Is(err, imgoci.ErrNotFound)` |
| `4` | `errors.Is(err, imgoci.ErrUnauthorized)` |
| `5` | `errors.Is(err, imgoci.ErrInvalidIndex)` |
| `6` | `errors.Is(err, imgoci.ErrInvalidSpec)` |
| `7` | `errors.Is(err, imgoci.ErrInvalidDest)` |
| `8` | `errors.Is(err, imgoci.ErrDigestMismatch)` |
| `9` | `errors.Is(err, imgoci.ErrUnsupportedType)` |
| `10` | `errors.Is(err, imgoci.ErrSelectionMismatch)` |
| `11` | `errors.Is(err, imgoci.ErrDecode)` |
| `130` | Interrupted by SIGINT. |
| `143` | Terminated by SIGTERM. |

Sentinels are checked in order, first match winning; see the
[error reference](errors.md) for what each one means. For an end-to-end run
against a local registry, see the
[first release tutorial](../tutorials/first-release.md).
