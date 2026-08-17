---
title: Resolve deliverables
description: Inspect what a release stores and select the exact files to fetch, by CLI or Go API.
---

# Resolve deliverables

This guide shows how to pick concrete files out of a release: inspect the stored alternatives, then resolve one deliverable by architecture, target, representation, and complete usage set, with your compression preferences and, when needed, explicit roles or extended capabilities.

Prerequisites:

- a release you can fetch, named by a reference such as `ghcr.io/example/os:v1`
- for the CLI steps, the private reference CLI built from `cli/` ([tutorial](../tutorials/first-release.md#build-the-cli)); for the Go steps, `github.com/imgoci/go` in your module
- credentials, if the registry needs them ([Use Docker credentials](use-docker-credentials.md))

A deliverable is every file entry that shares one `(architecture, target, representation, usage)` key. Resolving selects exactly one deliverable, then one stored file per selected role.

## Inspect the alternatives

Before selecting, see what the release actually stores.

CLI — every filter is optional, and empty filters match everything:

```sh
imgoci list ghcr.io/example/os:v1
```

Output is one tab-separated line per stored transport alternative:

```
<architecture>	<target>	<representation>	<usage>	<role>	<compression>	<artifactType>
```

Narrow it when the release is large:

```sh
imgoci list -architecture arm64 -usage install -role disk ghcr.io/example/os:v1
```

`-usage` and `-role` repeat. Each `-usage` occurrence is a containment filter:
the result may carry additional usage values. Each `-role` occurrence adds a
role that a matching deliverable must contain. The output's `usage` field
always reports the deliverable's exact comma-separated set; the empty set is
an empty fourth field.

Go — `List` on the fetched index:

```go
client, err := imgoci.New()
if err != nil {
	return err
}
rel, err := client.Fetch(ctx, "ghcr.io/example/os:v1")
if err != nil {
	return err
}

deliverables, err := rel.Index().List(imgoci.ListQuery{
	Architecture: "arm64",
	Usage:        []string{"install"},
})
if err != nil {
	return err
}
for _, d := range deliverables {
	for _, role := range d.Roles {
		for _, alt := range role.Alternatives {
			fmt.Printf("%s/%s/%s usage=%s %s %s %s\n",
				d.Architecture, d.Target, d.Representation, d.Usage.String(),
				role.Role, alt.Compression, alt.ArtifactType)
		}
	}
}
```

Listing never filters by consumer capabilities: it shows alternatives your client may not be able to retrieve, so it is the full inventory.

## Select the deliverable

Resolving requires the three scalar deliverable selectors, the complete usage set, and at least one accepted compression. The usage set may be empty.

Read the complete set from the `usage` field in the list output. The examples
below assume that field was `install,install-offline`.

CLI:

```sh
imgoci resolve \
  -architecture arm64 -target qemu -representation qcow2 \
  -usage install -usage install-offline \
  -compression zstd -compression none \
  ghcr.io/example/os:v1
```

Go:

```go
query := imgoci.ResolveQuery{
	Architecture:   "arm64",
	Target:         "qemu",
	Representation: "qcow2",
	Usage:          []string{"install", "install-offline"},
	Compressions:   []string{"zstd", "none"},
}
sel, err := client.Resolve(rel, query)
if err != nil {
	return err
}
for _, entry := range sel.Entries() {
	fmt.Println(entry.Selector.Role, entry.Filename, entry.ContentDigest, entry.ContentSize)
}
```

The CLI prints one tab-separated line per selected role:

```
<architecture>	<target>	<representation>	<usage>	<role>	<compression>	<filename>	<artifactType>	<contentDigest>	<contentSize>
```

When the list output has an empty `usage` field, omit `-usage` in `resolve` and
`fetch`. In Go, nil and empty `ResolveQuery.Usage` both request that empty set.
Omitting usage does not match a deliverable that carries any usage value.

`fetch` takes the same selector flags, so a command line that resolves is a command line that fetches.

## Order your compression preferences

`Compressions` is a preference list, most preferred first, with no duplicates. For every selected role, the first listed compression that the release stores for that role wins. The valid values are the spec's fixed set: `none`, `gzip`, `xz`, `zstd`.

On the CLI, order comes from flag order:

```sh
-compression zstd -compression gzip -compression none
```

selects `zstd` where stored, falls back to `gzip`, then to an uncompressed copy. A role whose stored compressions are all outside your list fails the whole selection — resolution is atomic, never partial.

## Choose roles explicitly (optional)

Leave roles alone and the spec's default-role rule applies per representation: `raw`, `raw-4kn`, `qcow2`, and `iso` select `disk`; `incus-vm` selects `disk` and `metadata`; `linux-netboot` and unknown representations select every role present.

To override, name the roles — the selection is then exactly those, no more:

```sh
imgoci resolve ... -role disk -role metadata ghcr.io/example/os:v1
```

```go
query.Roles = []string{"disk", "metadata"}
```

In Go, nil `Roles` means the default rule; a non-nil slice must be non-empty.

## Extend capabilities (optional)

Resolution drops alternatives whose file-manifest type the consumer cannot retrieve. A zero `Capabilities` in `ResolveQuery` defaults to the client's own set — this build retrieves `application/vnd.imgoci.file.v1` and `application/vnd.bigoci.file.v1`. Override only to restrict or when you have checked what your consumer supports:

```go
caps, err := imgoci.NewCapabilities("application/vnd.imgoci.file.v1")
if err != nil {
	return err
}
query.Capabilities = caps
```

CLI: `-capability` repeats, one type per occurrence. Unset means the client's capabilities. The set must include the standard type `application/vnd.imgoci.file.v1`.

See [capabilities reference](../reference/capabilities.md) for the exact validation rules.

## Empty results and no-match failures

`list` and `resolve` behave differently when nothing matches:

- **`list` with no matches is a valid, empty result.** The Go API returns an empty slice and a nil error; the CLI prints nothing to standard output and exits `0`.
- **`resolve` with no match is a failure.** No deliverable for the four-field key, including exact usage-set equality, a selected role absent from that deliverable, or no accepted compression for a role all return a descriptive error without a matchable sentinel; the CLI exits `1`. One case is distinguishable: when capability filtering leaves a selected role with no retrievable alternative, the error matches `imgoci.ErrUnsupportedType` and the CLI exits `9`.

The CLI rejects a missing required scalar selector before it builds a client.
An unknown compression token, a duplicate role or usage value, or a malformed
usage token fails when `Resolve` validates the query — after the CLI has
already fetched the index. A syntactically valid unknown or private usage value
remains usable. The Go `Resolve` call itself does no network I/O.

## Related pages

- [Verify a release](verify-a-release.md) — what happens to a `Resolved` selection during fetch.
- [CLI reference](../reference/cli.md) — full flag, output, and exit-code contract.
- [API reference](../reference/api.md) — `ListQuery`, `ResolveQuery`, `Resolved`.

Implemented spec revision: imgoci v1 draft, 2026-08-11 ([imgoci/spec](https://github.com/imgoci/spec) commit `5b957102eeda16498fdcb80a738431b83abd4197`).
