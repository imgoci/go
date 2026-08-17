---
title: Publish and fetch your first release
description: Build the reference CLI, publish an imgoci release to a local zot registry, and fetch it back verified.
---

# Publish and fetch your first release

In this tutorial we publish an imgoci release — a set of files stored under one tag in an OCI registry — to a registry running on your machine, then fetch it back and confirm the bytes are identical. Along the way you use all four commands of the reference CLI: `publish`, `list`, `resolve`, and `fetch`.

The CLI is a private reference tool inside this repository. It is not released or published anywhere; you build it from source.

## What you need

- Go 1.26.5 or later
- Docker
- `git`, `curl`, `shasum`, and `cmp`
- about fifteen minutes

## Build the CLI

Clone the repository and build the CLI into a fresh working directory:

```sh
git clone https://github.com/imgoci/go imgoci-go
mkdir imgoci-tutorial
go build -C imgoci-go/cli -o "$PWD/imgoci-tutorial/imgoci" .
cd imgoci-tutorial
```

Check that the binary runs:

```sh
./imgoci version
```

You see:

```
imgoci (private reference CLI)
```

## Start a local registry

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

## Create a file to release

A release stores ordinary files. Create one megabyte of random bytes to stand in for a disk image, and note its SHA-256 — you will see this digest again later:

```sh
head -c 1048576 /dev/urandom > disk.img
shasum -a 256 disk.img
```

You see a 64-character hex digest followed by the filename. Yours differs from anyone else's because the bytes are random.

## Describe the release

`publish` reads a JSON spec that names the release and its files. Create it:

```sh
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
```

Each file has a six-field selector: `architecture`, `target`, `representation`, `usage`, `role`, and `compression`. This sample supplies the five required string fields and omits optional `usage`, so the deliverable has the empty usage set. `compression` declares what the file already is; the library never compresses on your behalf, and `none` says `disk.img` is stored as-is.

## Publish

Publish the release under the tag `v1`:

```sh
./imgoci publish -plain-http release.json localhost:5500/tutorial/example:v1
```

Standard output is exactly one line — the canonical digest of the release index the command just published:

```
sha256:...64 hex characters...
```

Everything else (what the command is doing, how long it took) goes to standard error. That digest identifies the exact release index. A digest-pinned fetch cannot be redirected if the `v1` tag moves, but it succeeds only while the registry retains the untagged index. Registry retention and garbage-collection policies determine how long old digests remain available.

## See what the registry holds

List every stored alternative in the release:

```sh
./imgoci list -plain-http localhost:5500/tutorial/example:v1
```

You see one tab-separated line, matching the spec you published:

```
amd64	qemu	raw		disk	none	application/vnd.imgoci.file.v1
```

## Select one deliverable

`resolve` picks concrete files the way a consumer would: you name the architecture, target, representation, and complete usage set you need, plus the compressions you accept:

```sh
./imgoci resolve -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5500/tutorial/example:v1
```

The publish spec omitted `usage`, so this tutorial passes no `-usage` to
`resolve` or `fetch`; for those commands, an unset flag requests the exact
empty usage set.

You see one tab-separated line per selected file:

```
amd64	qemu	raw		disk	none	disk.img	application/vnd.imgoci.file.v1	sha256:...	1048576
```

Compare the second-to-last column with the `shasum` output from earlier: it is the SHA-256 of your `disk.img`, recorded in the release at publish time. The last column is its size in bytes.

Notice you never named the `disk` role. For the `raw` representation the spec's default-role rule selects `disk` on its own.

## Fetch the files

Fetch the same selection into a directory. The directory is created if it does not exist:

```sh
./imgoci fetch -plain-http \
  -architecture amd64 -target qemu -representation raw \
  -compression none \
  localhost:5500/tutorial/example:v1 out
```

Standard output stays empty; standard error reports `imgoci: fetched in ...`. The fetch verified every byte against the digests in the release index before placing `out/disk.img`.

## Compare the bytes

```sh
cmp disk.img out/disk.img && echo identical
```

You see:

```
identical
```

The file made a full round trip: published to a registry, fetched back, and verified byte-for-byte.

## Clean up

```sh
docker stop imgoci-zot
cd ..
rm -rf imgoci-tutorial imgoci-go
```

`docker stop` also removes the container, because it was started with `--rm`.

## Where next

- [Resolve deliverables](../how-to/resolve-deliverables.md) — pick files from releases with several architectures, roles, and compressions.
- [Verify a release](../how-to/verify-a-release.md) — what the fetch path verifies, and how to pin a release by digest.
- [Use Docker credentials](../how-to/use-docker-credentials.md) — authenticate against real registries.
- [CLI reference](../reference/cli.md) — every flag, output column, and exit code.

Implemented spec revision: imgoci v1 draft, 2026-08-16 ([imgoci/spec](https://github.com/imgoci/spec) commit `46d18b74cc407ac7d61ded7692fc42b644f4d1e2`).
