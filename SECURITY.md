# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `openadr-interop`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.com**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/openadr-interop/security) tab → **Advisories** → **Report a vulnerability**), with a description, steps to reproduce and the affected image tag or digest.

Vulnerabilities in the bundled implementations themselves (OpenLEADR, the ElaadNL openadr3-client, PostgreSQL and their dependencies) belong upstream; report them there, and tell us so that the pin can be moved.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No; pull the latest images |

## Security Considerations

### These are test fixtures, not products

The server images run **VTNs over plain HTTP whose OAuth2 client secrets are published in this repository** ([fixtures/](fixtures)) and baked into every image. Anyone who can reach one can read and write everything in it. That is their purpose.

- **Never expose a server beyond loopback or a private Docker network**
- **Never point a client or the conformance suite at a production VTN**: the write operations create, change and delete programs, events, reports, VENs and resources
- **Never reuse a fixture's client ID or secret** anywhere that matters
- `compose.yaml` and the `run-*` Makefile targets publish ports on `127.0.0.1` only. Keep it that way; the self-tests use private Docker networks and publish nothing
- There is no TLS. The client adapter turns off its library's HTTPS requirement when it is given an `http://` URL, which a deployment must never do
- The `openleadr` image runs a PostgreSQL inside the container, on loopback, without a password; its data does not outlive the container

### Reports

The conformance suite records every HTTP exchange. Bearer tokens, client secrets and issued tokens are replaced by `[redacted]` before an exchange is stored, and the suite's own tests check that. Request and response bodies are recorded as they are: do not run it against a VTN whose objects are confidential.

### Image hardening

- Processes run as a non-root user (uid 10001)
- Base images are pinned by digest; upstream sources by commit or exact version ([versions.yaml](versions.yaml))
- The runtime images contain the adapter, the upstream implementation, its runtime and the fixtures; build tools stay in the build stages
- The images are rebuilt, not patched: a base image or upstream fix arrives with a new release

### Supply chain

- Upstream sources and packages are fetched during the image build at pinned versions, from GitHub, crates.io, PyPI and the Alpine package repository. OpenLEADR is built with `--locked` against its `Cargo.lock`; the Python adapter installs from a requirements file that pins every dependency with hashes
- The Alpine packages in the `openleadr` image (PostgreSQL, Python, argon2) are whatever the pinned Alpine release serves at build time; the versions that were installed are listed in `/licenses/alpine/PACKAGES.txt` of the image
- [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) lists what each image contains and under which licence; the licence texts are in `/licenses` of every image
