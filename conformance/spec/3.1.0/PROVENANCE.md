# OpenADR 3.1.0 OpenAPI document

`openadr3.yaml` is the OpenAPI definition of OpenADR 3.1.0, the work of the
[OpenADR Alliance](https://www.openadr.org/). Its own `info.license` block
declares the Apache License, Version 2.0.

| | |
|---|---|
| Taken from | <https://github.com/grid-coordination/openadr3-specification>, `3.1.0/openadr3.yaml` |
| Commit of that repository | `ca9696d324b8ab6a41b34aaefa48fc98c0a775f5` (2026-09-30) |
| Upstream commit it republishes | `oadr3-org/specification@ddd2cc58589bdc31ba1dd19fbdf90ed7e10b31dc` (2025-08-07, document 20250807-X) |
| SHA-256 | `135bc74af2f733338b55afa95df3cb3a1185867e8e41a7d16cdb2dbfaeb3b475` |

The file is not the byte-for-byte upstream file: the publisher of that
repository reformatted it for `yamllint` and corrected two YAML faults (a
stray `-` after a `null` default, and a `|` that was meant to start a block
scalar). Both are listed in that repository's README, next to the unmodified
upstream bytes. Nothing was changed here.

The package `spec` embeds this file and refuses to load it if the checksum
above no longer matches. The authoritative copy is the one the OpenADR
Alliance distributes to registered users.

This repository is not affiliated with or endorsed by the OpenADR Alliance,
and a passing run of its tests is not an OpenADR certification.
