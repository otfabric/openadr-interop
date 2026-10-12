# Requirements

What this repository must do and must not do. [PLAN.md](PLAN.md) says in
which order; [docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md) and
[docs/FIXTURES.md](docs/FIXTURES.md) are the specifications that follow from
it.

## Purpose

Independent, reproducible interoperability validation for OpenADR 3
implementations: established third-party implementations, packaged as
containers that all behave the same way, so that a library can be tested
against real implementation diversity without owning any of it; and a
conformance suite that asserts the specification itself.

It is not another OpenADR implementation, and its results are not an
OpenADR Alliance certification.

## R1 - Repository isolation

- R1.1 This repository does not import, build, link against or depend on any
  library under test, and contains no test logic or expectation specific to
  one. CI checks this.
- R1.2 A consumer depends on published images and the documented contract
  only. It does not need a checkout of this repository to run its tests.
- R1.3 No third-party code from here is copied into a consumer, and the
  contract does not shape a consumer's public API.
- R1.4 The conformance suite asserts the specification, not an
  implementation: its expectations are derived from the pinned OpenAPI
  document and the Definition, and it shares no code with the adapters.

| Responsibility | Here | Consumer |
|----------------|:----:|:--------:|
| Third-party implementations, adapters, Dockerfiles | yes | no |
| Fixtures, fixture schema and validation | yes | no |
| Container contract, capability declarations | yes | no |
| Image builds and publishing, licence compliance | yes | no |
| Self-tests of the reference stacks | yes | no |
| Specification-driven conformance scenarios | yes | may run them |
| Scenarios and assertions about a library | no | yes |
| Compatibility claims and release qualification of a library | no | yes |

## R2 - Reference implementations

- R2.1 In scope, in this order of priority: the OpenLEADR VTN (Rust) and the
  ElaadNL openadr3-client (Python). Further candidates are named in PLAN.md.
- R2.2 An adapter wraps the upstream implementation and adds nothing to the
  protocol: a client hands requests to the library's own models and client,
  a server is the upstream VTN.
- R2.3 An adapter has the roles its upstream has. There must be at least one
  independent server and one independent client.
- R2.4 OpenADR 3.1.0 first. A second revision is added as a capability of
  the adapters that speak it, not as a second contract.

## R3 - One contract

- R3.1 Every image has the same commands, flags, JSON documents and exit
  codes ([docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md)).
- R3.2 The contract is versioned; documents carry `schemaVersion`, and
  JSON Schemas for them are published in `schemas/`.
- R3.3 stdout is machine-readable; diagnostics go to stderr.

## R4 - Honest capabilities

- R4.1 An image declares what it does and how it reads the specification
  where implementations differ (`print-capabilities`). A test skips what a
  pairing does not declare; nothing is skipped silently.
- R4.2 Every difference between a reference and the specification that the
  self-tests or the conformance suite find is written down in
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), with an assessment of
  whether it is a fault or a reading.
- R4.3 A known finding is recorded with the text of the failure, so that it
  can neither hide a new failure nor outlive the old one.

## R5 - Self-tests

- R5.1 `make smoke` checks the contract of each image.
- R5.2 `make interop` runs every client against every server: the cases of
  `tests/cases.tsv` and the basic workflow checked on values.
- R5.3 `make conformance` runs the conformance suite against every
  reference server.
- R5.4 The documents the self-tests produce are validated against the
  schemas.

## R6 - Reproducibility

- R6.1 Upstream versions are pinned exactly, base images by digest, in the
  Dockerfiles and mirrored in `versions.yaml`; `scripts/check-versions.sh`
  fails when they disagree.
- R6.2 Where the ecosystem has lock files or hashes, the build verifies them.
- R6.3 The specification document is pinned by checksum.
- R6.4 Results name the versions involved, and credentials are redacted
  from every report.

## R7 - Safety

- R7.1 Servers and clients are test fixtures: plain HTTP, published
  secrets. Nothing publishes a port beyond loopback, and the self-tests use
  private Docker networks.
- R7.2 Processes run as a non-root user.

## R8 - Licensing

- R8.1 The repository is MIT. Each image carries the licence texts of what
  it contains in `/licenses`, and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
  lists it.
- R8.2 The OpenADR Alliance's licensed certification test assets are not
  used or redistributed here.
