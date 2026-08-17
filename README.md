# imgoci/go

Go library implementing the imgoci release format for OS-image releases in OCI registries.

## Status

The library is under development and has no stable API yet.

## Spec Compatibility

The package version tracks this library's Go API. Format compatibility is
carried by the imgoci media types (`.v1`), not by version numbers.

| imgoci/go | Implements | Format |
|-----------|------------|--------|
| v0.1.x | [imgoci spec v0.1.0](https://github.com/imgoci/spec/releases/tag/v0.1.0) | `.v1` |

## Related Projects

- [bigoci](https://github.com/imgoci/bigoci)
- [go-oci-blob](https://github.com/imgoci/go-oci-blob)

## License

Licensed under either of Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE)) or MIT ([LICENSE-MIT](LICENSE-MIT)), at your option.
