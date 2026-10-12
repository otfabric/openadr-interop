# Third-party notices

The files of this repository are MIT ([LICENSE](LICENSE)). The images built
from it contain third-party software under its own licences.

## What is licensed how

| What | Licence |
|------|---------|
| Adapters, scripts, fixtures, schemas, documentation, the conformance suite | MIT |
| `conformance/spec/3.1.0/openadr3.yaml` | The work of the OpenADR Alliance; its `info.license` declares Apache-2.0. See [PROVENANCE.md](conformance/spec/3.1.0/PROVENANCE.md) |
| Go modules the conformance suite uses (not in any image) | See `conformance/go.mod`; kin-openapi is MIT, regexp2 is MIT |

Every image has the licence texts of what it contains under `/licenses`.

## Image `openadr-interop-openleadr`

| Component | Licence | In the image |
|-----------|---------|--------------|
| [OpenLEADR openleadr-rs](https://github.com/OpenLEADR/openleadr-rs) v0.2.7, the `openleadr-vtn` binary | Apache-2.0 OR MIT | `/licenses/openleadr/` |
| The Rust crates compiled into it (281) | Mostly MIT or Apache-2.0; each is listed with the licence it declares in `/licenses/crates/CRATES.txt` | `/licenses/crates/<crate>-<version>/`. Three crates ship no licence text and are listed in `NO-TEXT.txt` |
| The Rust standard library | MIT OR Apache-2.0, with the notices of what it bundles | `/licenses/rust-std/` |
| PostgreSQL 17 (Alpine package), the VTN's database | PostgreSQL licence | Alpine's package database; versions in `/licenses/alpine/PACKAGES.txt` |
| Python 3 (Alpine package), runs the adapter | PSF-2.0 | as above |
| argon2 (Alpine package), hashes the fixture's client secrets | CC0-1.0 OR Apache-2.0 | as above |
| Eclipse Mosquitto 2.0 (Alpine package), the MQTT broker | EPL-2.0 OR BSD-3-Clause (EDL-1.0) | as above |
| Alpine Linux base image, musl, OpenSSL and the rest of its packages | Various; see Alpine's package database | as above |
| The adapter | MIT | `/licenses/openadr-interop/LICENSE` |

## Image `openadr-interop-elaadnl`

| Component | Licence | In the image |
|-----------|---------|--------------|
| [ElaadNL openadr3-client](https://github.com/ElaadNL/openadr3-client) 2.4.0 | Apache-2.0 | `/licenses/packages/openadr3_client-2.4.0/` |
| Its Python dependencies, as locked in [adapters/elaadnl/requirements.txt](adapters/elaadnl/requirements.txt): pydantic, pydantic-core, pydantic-extra-types, annotated-types, typing-extensions, typing-inspection (MIT); requests, requests-oauthlib (Apache-2.0, ISC); oauthlib, idna, pycparser (BSD-3-Clause); urllib3, charset-normalizer, cffi, pyjwt (MIT); certifi (MPL-2.0); cryptography (Apache-2.0 OR BSD-3-Clause); pycountry (LGPL-2.1) | As declared by each package: `/licenses/packages/PACKAGES.txt` | `/licenses/packages/<package>-<version>/` |
| Python 3.14 and the Debian base image | PSF-2.0; various | `/licenses/python/LICENSE.txt`; Debian's `/usr/share/doc/*/copyright` |
| The adapter | MIT | `/licenses/openadr-interop/LICENSE` |

The licences named for the Python dependencies are a summary for
orientation; the authority is the metadata and the licence text each package
ships, which the image carries.

## Corresponding source

Every component is built or installed from the public source or package
named in [versions.yaml](versions.yaml), at the version pinned there, without
modification. pycountry (LGPL-2.1) is installed as the unmodified wheel from
PyPI and can be replaced in a derived image.
