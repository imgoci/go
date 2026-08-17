# imgoci/go

Go library implementing the [imgoci release format](https://github.com/imgoci/spec): it publishes, fetches, validates, and resolves OS-image releases stored as OCI artifacts in container registries.

The library is pre-v1. The API is not yet stable.

## Features

- Offline index validation: JSON decode with duplicate-key rejection, the ten consumer rules of spec section 6, and canonical-bytes verification, without a network.
- Selection: `List` and `Resolve` pick deliverables by architecture, target, representation, usage set, roles, and compression preference per spec section 7.
- Fetching: every file is digest-verified, staged privately, and committed only after all selected roles verify.
- Publishing: standard single-manifest files and BigOCI multipart files from one `ReleaseSpec`.
- Compression: `none`, `gzip`, `xz`, and `zstd`, with a configurable decoder memory ceiling.
- Authentication: anonymous by default; static credentials or the Docker credential store by option.

## Installation

```sh
go get github.com/imgoci/go
```

Requires Go 1.26.5 or later.

## Usage

Fetch a release, resolve one deliverable, and download its files:

```go
package main

import (
	"context"
	"log"

	imgoci "github.com/imgoci/go"
)

func main() {
	ctx := context.Background()

	client, err := imgoci.New(imgoci.WithDockerCredentials())
	if err != nil {
		log.Fatal(err)
	}

	rel, err := client.Fetch(ctx, imgoci.Reference("registry.example.com/os/example:42.1"))
	if err != nil {
		log.Fatal(err)
	}

	sel, err := client.Resolve(rel, imgoci.ResolveQuery{
		Architecture:   "amd64",
		Target:         "metal",
		Representation: "iso",
		Compressions:   []string{"zstd", "gzip", "none"},
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := client.FetchFiles(ctx, rel, sel, imgoci.ToDir("./images")); err != nil {
		log.Fatal(err)
	}
}
```

The registry and release in this example are illustrative. To publish a release and fetch it back from a local registry, follow the [first-release tutorial](https://imgoci.github.io/go/tutorials/first-release/).

## Documentation

Full documentation is at [imgoci.github.io/go](https://imgoci.github.io/go/):

- [Publish and fetch your first release](https://imgoci.github.io/go/tutorials/first-release/) — tutorial against a local registry.
- [How-to guides](https://imgoci.github.io/go/how-to/resolve-deliverables/) — resolve deliverables, verify a release, use Docker credentials.
- [API reference](https://imgoci.github.io/go/reference/api/) — every public function, type, and option; also on [pkg.go.dev](https://pkg.go.dev/github.com/imgoci/go).
- [Error reference](https://imgoci.github.io/go/reference/errors/) — the public sentinel errors and how to respond to each.
- [About the architecture](https://imgoci.github.io/go/explanation/architecture/) — design, boundaries, and trade-offs.

## Spec Compatibility

The package version tracks this library's Go API. Format compatibility is
carried by the imgoci media types (`.v1`), not by version numbers.

| imgoci/go | Implements | Format |
|-----------|------------|--------|
| v0.1.x | [imgoci spec v0.1.0](https://github.com/imgoci/spec/releases/tag/v0.1.0) | `.v1` |

The pinned spec revision is recorded in [`testdata/conformance/SPEC_COMMIT`](testdata/conformance/SPEC_COMMIT).

## Related Projects

- [imgoci/spec](https://github.com/imgoci/spec) — the imgoci release format specification.
- [bigoci](https://github.com/imgoci/bigoci) — the BigOCI multipart artifact format this library uses for large files.
- [go-oci-blob](https://github.com/imgoci/go-oci-blob) — the OCI blob transfer library underneath both.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, the task runner, and commit conventions. Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## License

Licensed under either of Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE)) or MIT ([LICENSE-MIT](LICENSE-MIT)), at your option.
