# testdata/bigoci

Committed BigOCI File Format v1 artifacts shared by the `internal/transfer`
unit suite and the container-gated root e2e suite. They exist so a BigOCI test
is exercised against a manifest that a real BigOCI reader accepts, instead of
an ad-hoc JSON object that only the imgoci consumer profile happens to
tolerate.

Spec §8 rule 2 makes the consumer validate a BigOCI file manifest against
BigOCI File Format v1 and require at least two parts, so both a conforming
two-part artifact and a conforming one-part artifact are needed: the one-part
artifact must be rejected for its part count and for nothing else.

## Layout

    v1/valid-two-part/manifest.json   BigOCI v1 manifest, canonical bytes
    v1/valid-two-part/part-0.bin      first 20 stored bytes
    v1/valid-two-part/part-1.bin      last 20 stored bytes
    v1/valid-one-part/manifest.json   BigOCI v1 manifest, canonical bytes
    v1/valid-one-part/part-0.bin      all 40 stored bytes

The stored file of an artifact is the concatenation of its `part-N.bin` files
in manifest layer order. Neither directory holds the OCI empty config blob:
it is the two bytes `{}`, which every consumer of these fixtures writes from
`ocispec.DescriptorEmptyJSON`.

## Required members

Each `manifest.json` carries every member `github.com/imgoci/bigoci` v0.2.0
requires of a manifest it decodes (`internal/manifest/decode.go`):

- `schemaVersion`: `2`.
- `mediaType`: `application/vnd.oci.image.manifest.v1+json`.
- `artifactType`: `application/vnd.bigoci.file.v1`.
- `config`: the OCI empty descriptor — media type
  `application/vnd.oci.empty.v1+json`, digest
  `sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a`,
  size `2`, no inline `data`.
- `layers`: part descriptors in file order, each with media type
  `application/vnd.bigoci.file.part.v1` and the true digest and size of the
  matching `part-N.bin`. Part sizes follow the split rule: every part is
  `io.bigoci.part.size` bytes except a shorter final part.
- `annotations."io.bigoci.file.digest"`: sha256 of the concatenated parts.
- `annotations."io.bigoci.file.size"`: byte length of the concatenated parts,
  base 10.
- `annotations."io.bigoci.part.size"`: the split part size, base 10.

## Titles

Both manifests carry `org.opencontainers.image.title`, and both titles differ
from every `io.imgoci.filename` the tests use. Spec §8 says a BigOCI title is
informational and has no imgoci meaning, so a test can only prove that the
decoded output is named from `io.imgoci.filename` when the two names cannot
be confused.

| Artifact         | Title                              |
| ---------------- | ---------------------------------- |
| `valid-two-part` | `bigoci-title-not-imgoci-name.bin` |
| `valid-one-part` | `bigoci-one-part-title.bin`        |

## Digests

    valid-two-part  manifest sha256:ce1fceed2bbb084c8f61bf9fd3f996bcf142a5b4f2b4ea06d8bd20bfdaef5023  812 bytes
                    file     sha256:8b2fbf3812a7bc92e1985e86487a862a538a07be913abd24372579b559d79d33   40 bytes
                    part-0   sha256:9b888af9e2b8b6664cf1d0ef32e2d42990e3a902a46b77e8e88baa1c14b1e332   20 bytes
                    part-1   sha256:40ae8895bddeb4b2ede805507d6f98fe586b5a6282b7fa37598162d6c6cd0c99   20 bytes
    valid-one-part  manifest sha256:c4b58fb4366f3c944c7b91bbf45437af189d739f16cf4044e605b3b117043ccd  660 bytes
                    file     sha256:2f6a90a54075c316e8d5af76e02f3fb577e2693528d5fbe09b99776bfa715e2f   40 bytes
                    part-0   sha256:2f6a90a54075c316e8d5af76e02f3fb577e2693528d5fbe09b99776bfa715e2f   40 bytes

## Production

The stored bytes are fixed ASCII strings, chosen so the files are readable and
so the two-part artifact splits evenly:

    valid-two-part  "imgoci bigoci v1 two-part fixture bytes\n"  40 bytes, part size 20
    valid-one-part  "imgoci bigoci v1 one-part fixture bytes\n"  40 bytes, part size 40

`manifest.json` is the canonical BigOCI v1 encoding of those inputs: an
`ocispec.Manifest` written by `encoding/json` with HTML escaping disabled and
the trailing newline trimmed, which is byte for byte what
`github.com/imgoci/bigoci` v0.2.0 `internal/manifest.Encode` produces. It is
not RFC 8785; §9 of the imgoci spec exempts BigOCI manifests from the imgoci
canonical form and forbids re-encoding them.

The fixtures are regenerated, not hand-edited.
`internal/transfer.TestBigOCIFixturesAreValidBigOCIV1` re-derives both
`manifest.json` files from the committed part blobs with the same encoder and
compares them byte for byte, so an edited fixture fails that test.
