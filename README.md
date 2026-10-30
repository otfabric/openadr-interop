# openadr-interop — OpenADR 3 Interoperability Testing with Docker

A Docker-based interoperability testing framework for OpenADR 3, providing independent VTN and VEN/Business Logic implementations, deterministic fixtures, and automated protocol compatibility testing.
Test OpenADR 3 libraries against established implementations including the OpenLEADR VTN and the ElaadNL client. Reference implementations are independently containerized, enabling reproducible testing across platforms without integrating third-party protocol stacks into production applications.

| Adapter | Implementation | Language | Server (VTN) | Client (VEN, BL) | Image |
|---------|----------------|----------|:------:|:------:|-------|
| `openleadr` | [OpenLEADR openleadr-rs](https://github.com/OpenLEADR/openleadr-rs) v0.2.8 | Rust | yes | no | `ghcr.io/otfabric/openadr-interop-openleadr` |
| `elaadnl` | [ElaadNL openadr3-client](https://github.com/ElaadNL/openadr3-client) 2.4.0 | Python | no | yes | `ghcr.io/otfabric/openadr-interop-elaadnl` |
| `openleadr301` | [OpenLEADR openleadr-rs](https://github.com/OpenLEADR/openleadr-rs) v0.1.4, OpenADR **3.0.1** | Rust | yes | no | `ghcr.io/otfabric/openadr-interop-openleadr301` |

OpenADR 3.1.0: programs, events, reports, VENs and resources, with OAuth2
client credentials. The reference VTN also has MQTT notifications, resource
groups and DNS-SD discovery; the reference client also has subscriptions,
notifiers, MQTT topics and an MQTT listener, resource groups and access
without a token. Neither receives webhooks. The stacks do not all do the
same, and each image says what it supports (`print-capabilities`); see
[docs/CAPABILITIES.md](docs/CAPABILITIES.md).

OpenADR 3.0.1: `openleadr301` is a VTN of the earlier revision, OpenLEADR's
last release of OpenADR 3.0, with the `baseline-301` fixture: tokens from
`/auth/token`, targets as valuesMap objects, resources under their VEN. It
has no notifications. No reference client speaks 3.0.1 yet, so it is checked
by `make smoke` and not paired in `make interop`; the conformance suite is
of 3.1.0 only.

**Status: v0.6.1**, container contract 1.4, for linux/amd64 and linux/arm64.
The digests of its images are in the `manifest.json` of the
[release](https://github.com/otfabric/openadr-interop/releases/tag/v0.6.1).
See [PLAN.md](PLAN.md) and [RELEASE.md](RELEASE.md).

**Docs:** [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) (commands, JSON documents, exit codes) · [docs/FIXTURES.md](docs/FIXTURES.md) (what a reference VTN holds) · [docs/CAPABILITIES.md](docs/CAPABILITIES.md) · [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) (pins, cross-stack matrix, findings) · [conformance/](conformance/README.md) (the specification-driven test suite) · [REQUIREMENTS.md](REQUIREMENTS.md) · [PLAN.md](PLAN.md) · [RELEASE.md](RELEASE.md)

## Table of contents

- [What this repository is, and is not](#what-this-repository-is-and-is-not)
- [Quick start](#quick-start)
- [Using the images from a library's tests](#using-the-images-from-a-librarys-tests)
- [The conformance suite](#the-conformance-suite)
- [Repository layout](#repository-layout)
- [Development](#development)
- [Security](#security)
- [License](#license)

## What this repository is, and is not

It owns the reference implementations, their adapters, the fixtures, the
container contract, the images and the self-tests that show the references
agree with each other. It also holds a conformance suite that asserts the
OpenADR specification against any VTN.

It does not contain, import, build or know about any library under test.
[otfabric/go-openadr](https://github.com/otfabric/go-openadr) is a consumer:
it uses the images and owns its own scenarios, assertions and compatibility
claims. A passing self-test here says the *references* interoperate; it says
nothing about a consumer.

The fixtures describe what a VTN holds; the adapters make each
implementation serve or use that; the consumer decides what to assert.

A passing run of anything here is **not an OpenADR certification**. That is
a separate process of the [OpenADR Alliance](https://www.openadr.org/openadr-3-certification).

## Quick start

Needs Docker and `jq`. The first build compiles OpenLEADR from source and
takes several minutes.

```sh
make images      # build the adapter images for this machine
make smoke       # container contract of each image
make interop     # every client against every server
```

A reference VTN on loopback, and a reference client against it:

```sh
make run-openleadr      # http://127.0.0.1:8080, the baseline fixture; Ctrl-C to stop
                        # make run-openleadr301: OpenADR 3.0.1, the baseline-301 fixture

docker run --rm --network host ghcr.io/otfabric/openadr-interop-elaadnl:dev \
    client list-events --as ven-1 --url http://127.0.0.1:8080
```

(`--network host` reaches the host's loopback on Linux. On Docker Desktop
start the server with `--public-url http://host.docker.internal:8080` and
use that URL.) The client prints one JSON document:

```json
{"schemaVersion":"1.0","adapter":"elaadnl","operation":"list-events","client":"ven-1","role":"ven",
 "ok":true,"error":null,"authenticated":true,"status":200,
 "response":[{"eventName":"day-ahead-prices","programID":"...","intervals":[...]}],
 "problem":null,"elapsedMs":131}
```

`make documents` also writes `reports/results.json`, the organisation's
[interop-result-v1](schemas/interop-result-v1.schema.json), for the cross-stack self-test. Only `pass`
counts as passing: a run with no pass, or with a known bug that no longer reproduces, fails.

## Using the images from a library's tests

**To test a client library**, start a reference server and point the
library at it:

```sh
docker run -d --name vtn -p 127.0.0.1:8080:8080 ghcr.io/otfabric/openadr-interop-openleadr:dev \
    server --public-url http://127.0.0.1:8080
docker exec vtn test -f /run/openadr-interop/ready     # ready when this succeeds
docker run --rm ghcr.io/otfabric/openadr-interop-openleadr:dev print-fixture   # clients and objects
```

The VTN serves the [baseline fixture](docs/FIXTURES.md): its clients' IDs
and secrets, and the objects to expect, are what `print-fixture` prints.
For a client of OpenADR 3.0.1, use `openadr-interop-openleadr301` the same
way; it serves `baseline-301` (`print-fixture baseline-301`), and the token
URL is `<url>/auth/token`.

**To test a VTN**, load the same fixture into it — register the clients,
create the objects — and run reference client operations against it:

```sh
docker run --rm ghcr.io/otfabric/openadr-interop-elaadnl:dev \
    client list-programs --as ven-1 --targets group-a --url http://my-vtn:8080
```

The exit code and the result document say what the reference client made of
the VTN's answer; `error.code` `invalid-response` means it could not read
it. [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) is the whole
interface.

## The conformance suite

[conformance/](conformance/README.md) is a Go test runner that checks a VTN
against the specification's own OpenAPI document, scenario by scenario, and
writes JSON, JUnit and Markdown reports. It runs against the reference
servers here (`make conformance`) and against any other VTN.

Building it surfaced two faults in the OpenADR 3.1.0 OpenAPI document
itself; running it against the references surfaced the differences listed
in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

## Repository layout

```text
adapters/<name>/     one reference implementation each: Dockerfile and adapter
fixtures/            what a reference VTN holds; fixtures/schema/ is the JSON Schema
schemas/             JSON Schemas of the capability and client result documents
scripts/             self-tests (smoke, interop, conformance) and validators
tests/               cross-stack cases and known upstream bugs
conformance/         the specification-driven test suite (Go)
docs/                contract, fixtures, capabilities, compatibility
versions.yaml        every pin: upstream versions, base image digests
compose.yaml         reference servers for local work
```

## Development

```sh
make ci          # everything: validate, lint, build, smoke, interop, document schemas, conformance
make help        # all targets
```

[CONTRIBUTING.md](CONTRIBUTING.md) says how to change an adapter, move an
upstream pin, or add a fixture, an operation or an adapter.

## Security

The images run VTNs over plain HTTP with client secrets that are published
in this repository. They are test fixtures: keep them on loopback or on a
private Docker network. See [SECURITY.md](SECURITY.md).

## License

MIT, see [LICENSE](LICENSE). The images contain third-party software under
its own licences: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). The
OpenAPI document under `conformance/spec/` is the work of the OpenADR
Alliance and declares the Apache License 2.0; see its
[PROVENANCE.md](conformance/spec/3.1.0/PROVENANCE.md).
