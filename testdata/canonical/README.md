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

Each file must make `ParseIndex` return an error matching `ErrInvalidIndex`, and
must reach the rule it is named for before being rejected. `canonicalFailPhases`
in `parse_test.go` pins the phase that must reject each file and asserts that
every earlier phase accepts it, so a fixture cannot silently be caught by an
earlier gate than the one it documents. The map is the complete inventory of
this directory: an unnamed fixture, or a named fixture that no longer exists,
fails the suite.

| File | Rejecting phase | What it proves |
|---|---|---|
| `duplicate-keys-raw.json` | `index.Decode` | Identical object keys are rejected by the decoded-duplicate-key scan. |
| `duplicate-keys-decoded.json` | `index.Decode` | `"\u0069o.imgoci.name"` decodes equal to `"io.imgoci.name"`, so keys are compared after string decoding. |
| `invalid-utf8-value.json` | `index.Decode` | Invalid UTF-8 byte `0xff` inside a string value. |
| `invalid-utf8-key.json` | `index.Decode` | Invalid UTF-8 byte `0xff` inside an object key. |
| `canonical-wrong-descriptor-order.json` | `index.Validate` | Rule 9: object keys and numbers are RFC 8785 canonical (`VerifyCanonical` accepts these bytes), but the `manifests` array is not in section 9 descriptor order. Isolates rule 9 (array order) from rule 10 (canonical bytes). Source array order is zstd, none, gzip. |
| `pretty-printed.json` | `index.VerifyCanonical` | Rule 10: insignificant whitespace is non-canonical. |
| `unsorted-keys.json` | `index.VerifyCanonical` | Rule 10: root `schemaVersion` is written first instead of in RFC 8785 key order. It appears exactly once, so the duplicate-key scan does not fire and only key ordering is wrong. |
| `exponent-1e0.json` | `index.VerifyCanonical` | Rule 10: `1e0` is a non-canonical spelling of `1`. The exponent sits in the ignored unknown member `x-exp`, not in `size`: `size` is a known member and `index.Decode` rejects non-integer tokens there before rule 10 runs. |
| `exponent-1e2.json` | `index.VerifyCanonical` | Rule 10: `1e2` is a non-canonical spelling of `100`, again carried by the ignored unknown member `x-exp`. Compare `pass/extension-domain.json`, whose `"x-exp":1e+30` is the canonical spelling. |
| `nonminimal-escapes.json` | `index.VerifyCanonical` | Rule 10: `\u0061` is a non-minimal spelling of `a`. |
| `lone-surrogate.json` | `index.VerifyCanonical` | Rule 10: the lone surrogate `\uD800` in an unknown annotation value is rejected by the RFC 8785 transform itself (`Missing surrogate`), not by the byte comparison. `index.Decode` accepts it because `encoding/json` substitutes U+FFFD. |
