# testdata/canonical

Byte-level RFC 8785 fixtures owned by the public `ParseIndex` suite. These are
not copies of the spec's parsed-value corpus. Spec `conformance/v1/pass` files
are pretty-printed and are not themselves canonical bytes.

## pass/

Canonical twins of each spec v1 pass fixture, plus one extension-domain
fixture. `ParseIndex` must accept every file. The CUE cross-check vets this
directory against `#ReleaseIndex`.

| File | What it proves |
|---|---|
| `accepted-boundaries.json` | Maximum architecture tokens and content-size bound, with JCS key order (`org.example.note` between `io.imgoci.name` and `org.opencontainers.image.version`). |
| `additional-members.json` | Unknown members (including `platform`, `urls`, nested objects, booleans) survive canonicalization; object keys are UTF-16 sorted (`platform` before `size`). |
| `bigoci-manifest-type.json` | BigOCI file-manifest type is valid on a descriptor. |
| `case-insensitive-media-types.json` | Mixed-case media types are accepted at the value layer and encoded as written. |
| `extension-domain.json` | Unknown members with booleans, null, nesting, negative and fractional numbers, and a canonical exponent (`1e+30`). |
| `incus-vm.json` | Required `disk` and `metadata` roles with `target=incus`. |
| `linux-netboot-complete.json` | Coordinated `kernel`, `initramfs`, and `rootfs` roles. |
| `linux-netboot-kernel-only.json` | `linux-netboot` with only the required `kernel` role. |
| `minimal.json` | Smallest valid release index. |
| `misplaced-annotations.json` | Defined keys at the wrong object location are unknown annotations, not syntax errors. |
| `multiple-transport-alternatives.json` | gzip, none, and zstd alternatives of one file, including a BigOCI zstd alternative. |
| `unknown-annotations.json` | Unknown annotation keys, including `io.imgoci.*`, are preserved. |
| `unknown-manifest-type.json` | Syntactically valid unknown file-manifest type is accepted. |

## fail/

Each file must make `ParseIndex` return an error matching `ErrInvalidIndex`.

| File | What it proves |
|---|---|
| `pretty-printed.json` | Whitespace is non-canonical (rule 10). |
| `exponent-1e2.json` | `1e2` is a non-canonical number spelling of `100` (rule 10). |
| `exponent-1e0.json` | `1e0` is a non-canonical number spelling of `1` (rule 10). |
| `unsorted-keys.json` | Object keys not in RFC 8785 order (rule 10). |
| `nonminimal-escapes.json` | `\u0061` is a non-minimal spelling of `a` (rule 10). |
| `duplicate-keys-raw.json` | Identical object keys (decode / duplicate-key scan). |
| `duplicate-keys-decoded.json` | `"\u0069o.imgoci.name"` decodes equal to `"io.imgoci.name"` (duplicate-key scan). |
| `invalid-utf8-value.json` | Invalid UTF-8 byte `0xff` inside a string value. |
| `invalid-utf8-key.json` | Invalid UTF-8 byte `0xff` inside an object key. |
| `lone-surrogate.json` | JSON lone surrogate `\uD800` in an unknown annotation value. |
| `canonical-wrong-descriptor-order.json` | Object keys and numbers are RFC 8785 canonical, but the `manifests` array is not in section 9 descriptor order. Separates rule 9 (array order) from rule 10 (canonical bytes). Source array order is zstd, none, gzip. |
