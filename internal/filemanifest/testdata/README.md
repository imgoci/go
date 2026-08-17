# `internal/filemanifest` golden corpus

## `standard-v1.json`

The exact expected compact bytes of the spec §13 standard file-manifest example
(`spec/spec.md:889-907`): layer digest `sha256:` followed by 64 `b` characters,
layer size `123456789`.

`internal/filemanifest.TestBuildStandardGoldenBytes` asserts that
`BuildStandard` reproduces these bytes exactly. The file is an **independent
oracle**: its whole purpose is to fail when this repository's producer member
set or encoder changes, even when the package's own `ValidateStandard` and
`internal/jcs` change identically.

### Provenance

Generated **outside this repository** by CPython 3, from the pretty-printed
spec example piped verbatim on stdin:

```sh
python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin), sort_keys=True, separators=(",",":"), ensure_ascii=False), end="")' \
  < spec-standard-example.json > standard-v1.json
```

`json.dumps(sort_keys=True, separators=(",",":"))` is **not** an RFC 8785
implementation in general: it sorts keys by Unicode code point rather than by
UTF-16 code unit, and it serializes floats with `repr` rather than the
ECMAScript `Number.prototype.toString` algorithm. The two agree here only
because this particular object is **pure ASCII with integer-valued numbers
only**, a subset on which both rules coincide. Do not reuse this command to
generate a golden containing non-BMP characters or non-integer numbers.

### Rules

- The file is stored with **no trailing newline**; the last byte is `}`. The
  test asserts byte equality against the raw file contents.
- This file must **never** be regenerated from `BuildStandard` or from
  `internal/jcs`. Doing so destroys its value as an independent oracle: a
  producer defect would simply be copied into the golden. Regenerate it only
  from the spec text through the command above.
- Its length is 427 bytes, which is also the descriptor `size` the spec §13
  release-index example (`spec/spec.md:879`) reports for this manifest.
