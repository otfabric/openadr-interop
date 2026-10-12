# Contributing to openadr-interop

## Ground rules

- This repository knows no library under test ([REQUIREMENTS.md](REQUIREMENTS.md) R1). Nothing in `adapters`, `fixtures`, `schemas`, `scripts`, `tests` or `conformance` may name or depend on a consumer; CI checks it.
- [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) is the interface. A change that a consumer could notice is a contract change: update the document, the schemas and the version.
- An adapter adds nothing to the protocol. If a reference does something unexpected, that is a finding for [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), not something to smooth over in the adapter.
- A conformance scenario asserts the specification and says where: every scenario names operations of the OpenAPI document or sections of the Definition.

## Setup

Docker, `jq`, GNU make and bash; Go 1.25 or later for the conformance suite. Python 3 with `jsonschema` and `shellcheck` are used when installed and run in containers otherwise.

```sh
make ci
```

runs everything CI runs. The first build compiles OpenLEADR and takes several minutes; later builds use the cache.

## Changing an adapter

Edit, then `make image-<adapter> smoke-<adapter> interop`. The two Python adapters share `openadr_interop/fixture.py` byte for byte; `make validate` fails when they differ. Change both.

## Moving an upstream pin

1. Try it without changing anything: `make candidate-<adapter> CANDIDATE_VERSION=<version>` (and `CANDIDATE_SHA=<commit>` for `openleadr`).
2. A case that fails because of an upstream bug goes into `tests/known-bugs.tsv` or `conformance/known/<adapter>.tsv` for that version, and into docs/COMPATIBILITY.md. A capability that changed goes into the adapter's `print-capabilities`.
3. Move the pin in the Dockerfile (`ARG UPSTREAM_VERSION`, `ARG UPSTREAM_SHA`) and in `versions.yaml`; for `elaadnl`, regenerate `requirements.txt` as its header says. `make validate` checks that the two agree.
4. Remove the entries the new version no longer needs: a known bug that does not show fails the run.

## Registries and retries

Base images are pinned by digest, so any mirror of Docker Hub serves the same bytes. `BASE_REGISTRY` and `TOOLS_REGISTRY` select one; CI uses Google's mirror because anonymous pulls from Docker Hub are rate limited. Everything that talks to a registry goes through `scripts/retry.sh`.

## Adding a client operation or a server behaviour

Describe it in the contract first. Add it to the adapters that can do it and to `clientOperations` or `features` of their capabilities, to the schemas, and to `tests/cases.tsv` with the feature it needs. An adapter that cannot do it says so and is skipped, not failed.

## Adding a fixture

See [docs/FIXTURES.md](docs/FIXTURES.md#adding-a-fixture).

## Adding an adapter

1. `adapters/<name>/` with a Dockerfile that follows the others: base images by digest as `ARG <NAME>_DIGEST`, the upstream pin as `ARG UPSTREAM_VERSION`, a non-root user 10001, the fixtures at `/fixtures`, licence texts at `/licenses`.
2. The contract: `server` and/or `client`, `print-capabilities`, `print-fixture`, `version`, the exit codes.
3. `versions.yaml`, `ADAPTERS` (and `SERVER_ADAPTERS`) in the Makefile, the tables in README.md, docs/CAPABILITIES.md, docs/COMPATIBILITY.md and THIRD_PARTY_NOTICES.md.
4. `make ci`.

## Adding a conformance scenario

In `conformance/scenarios`, with an ID, a title that states the requirement, and references. Derive the expectation from the specification, never from what an implementation does. Where the specification cannot be met as written, that is a deviation: `conformance/spec/DEVIATIONS.md`, applied in `spec.go` and covered by a test.

## Style

Shell: bash, `set -uo pipefail`, shellcheck clean. Python: standard library only in adapters, apart from the wrapped implementation. Go: gofmt, `go vet`, golangci-lint with the configuration in `conformance/`. Comments say why.

## License

By contributing you agree that your contributions are licensed under the MIT licence of this repository.
