# RFC 8785 test-vector corpus

This directory holds input/expected pairs for `internal/jcs`.

## Provenance

Official JCS vectors are copied from the [cyberphone/json-canonicalization](https://github.com/cyberphone/json-canonicalization)
`testdata` tree (master, retrieved 2026-08-15) and from the same files as
vendored by [gowebpki/jcs v1.0.1](https://github.com/gowebpki/jcs/tree/v1.0.1/testdata).
cyberphone's README: input files are non-canonical JSON; the matching file in
`output/` is the RFC 8785 result. gowebpki adds top-level literal and string
cases (`true.json`, `false.json`, `null.json`, `simpleString.json`).

| File | Source | What it covers |
|---|---|---|
| `values.json` | cyberphone (= RFC 8785 §3.2.2 sample) | number canonicalization including `1E30` / `4.50` / `2e-3` / `1e-27`, literal array, escape minimization |
| `weird.json` | cyberphone (RFC 8785 §3.2.3 key-sort, plus extras) | UTF-16 code-unit key sort, including `\r`, `\u0080`, euro, emoji surrogate pair, U+FB33 |
| `rfc8785-sorting.json` | reconstructed from RFC 8785 §3.2.3 | the exact appendix/section sorting example without cyberphone's extra keys |
| `rfc8785-exponent-1e2.json` | reconstructed from RFC 8785 §3.2.2 / ARCHITECTURE.md §6.2 | `1e2` → `100` |
| `unicode.json` | cyberphone | unnormalized combining mark preserved as-is |
| `arrays.json`, `structures.json`, `french.json` | cyberphone | nested objects/arrays, locale-independent sort |
| `true.json`, `false.json`, `null.json`, `simpleString.json` | gowebpki v1.0.1 | top-level literals and strings |

## Numbers

`numbers/rfc8785-appendix-b.txt` is the IEEE-754 double edge list from RFC 8785
Appendix B (hex bits, expected JSON). NaN and Infinity rows are omitted because
JCS must error on them; those cases live in `audit_test.go`.

The cyberphone 100-million-line ES6 file
(`es6testfile100m.txt.gz`, SHA-256
`0f7dda6b0837dde083c5d6b896f7d62340c8a2415b0c7121d83145e08a755272` at 100M
lines) is **not** vendored here: it is about 4 GiB uncompressed. Appendix B is
the documented edge list this package ships.

## Layout

```
input/<name>.json    non-canonical or already-canonical input
output/<name>.json   expected RFC 8785 bytes
numbers/rfc8785-appendix-b.txt
```
