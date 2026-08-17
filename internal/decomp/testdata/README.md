# decomp test fixtures

Stored files produced by the reference command-line compressors, committed so
this package's decoders are tested against what mainstream producers actually
emit rather than against streams this repository encoded itself.

Decoded length and digest below are an **independent oracle**: they were
computed with `xz -dc` and `zstd -dc` piped to `shasum -a 256`, never with this
package's decoders. A change that makes a decoder produce different bytes — a
dictionary or window configured smaller than the stream declares, for
instance — has to disagree with these numbers.

## Tool versions

| Tool | Version |
| --- | --- |
| `xz` | XZ Utils 5.8.3 (liblzma 5.8.3) |
| `zstd` | Zstandard CLI v1.5.7 |
| `shasum` | macOS `shasum -a 256` |

## `xz-9.xz`

Plain `xz -9`, which declares a **64 MiB LZMA2 dictionary** — eight times the
8 MiB that `ulikunitz/xz` substitutes for a zero `ReaderConfig.DictCap`, and
the reason the decoder must configure `DictCap` from the declared value.

```sh
printf 'imgoci xz -9 fixture\n' | xz -9 --stdout > internal/decomp/testdata/xz-9.xz
```

| Property | Value |
| --- | --- |
| Stored size | 88 bytes |
| Stored SHA-256 | `75fd0b68e256b9278cc1f171791d610381523ffe41f91864e3facc6aeca09d6a` |
| Declared LZMA2 dictionary | 67108864 (64 MiB), from `xz -lvv` |
| Decoded size | 21 bytes |
| Decoded SHA-256 | `626f31a02a7566ac80c1b2752775ab4e84382385fc11bbbee85312e628218aca` |

```sh
xz -dc internal/decomp/testdata/xz-9.xz | wc -c
xz -dc internal/decomp/testdata/xz-9.xz | shasum -a 256
```

## `zstd-long-27.zst`

`zstd --long=27`, which declares a **128 MiB window** — exactly
`DefaultDecoderMaxWindow`, and exactly the zstd CLI's own default decode limit
(`ZSTD_WINDOWLOG_LIMIT_DEFAULT`, windowLog 27). The frame is the boundary case:
accepted at the default, refused by anything lower.

```sh
dd if=/dev/zero bs=1048576 count=32 2>/dev/null | zstd -3 --long=27 --stdout > internal/decomp/testdata/zstd-long-27.zst
```

| Property | Value |
| --- | --- |
| Stored size | 1191 bytes |
| Stored SHA-256 | `b851d5dac31fec0d38075577286a0d6ea782df9e62732909c97cacfc0376ad25` |
| Declared window | 134217728 (128 MiB), from `zstd -lv` |
| Decoded size | 33554432 bytes (32 MiB of zeros) |
| Decoded SHA-256 | `83ee47245398adee79bd9c0a8bc57b821e92aba10f5f9ade8a5d1fae4d8c4302` |

```sh
zstd -dc internal/decomp/testdata/zstd-long-27.zst | wc -c
zstd -dc internal/decomp/testdata/zstd-long-27.zst | shasum -a 256
```
