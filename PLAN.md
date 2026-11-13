# Plan

## Scope of the first release (contract 1.0)

- OpenADR 3.1.0 over REST: programs, events, reports, VENs, resources,
  OAuth2 client credentials with token endpoint discovery.
- Two adapters: `openleadr` (server) and `elaadnl` (client).
- Fixtures `baseline` and `descriptors`.
- Self-tests: contract smoke test, cross-stack cases and workflow,
  conformance suite against the reference server.

Status: **released as v0.1.0.** CI passes on linux/amd64 and linux/arm64.

| Check | Result |
|---|---|
| `make smoke` | openleadr 27 checks, elaadnl 33 checks |
| `make interop` | elaadnl → openleadr: 59 pass, 2 skipped on capabilities |
| `make conformance` | openleadr: 17 of 21, 4 known findings |

## After v0.1.0

v0.1.0 is released: the images are built and self-tested natively on
linux/amd64 and linux/arm64 and published, with the digests in the
release's `manifest.json`.

1. Make the two packages on GHCR public, like those of the other interop
   repositories. They were created private, and a consumer cannot pull them.
2. Report the findings of docs/COMPATIBILITY.md upstream (OpenLEADR,
   ElaadNL) and the two specification faults to the specification's authors.

## v0.2.0 (released)

An MQTT broker in the `openleadr` image, and authorization scenarios in the
conformance suite.

## v0.3.0 (released)

DNS-SD over Multicast DNS in the `openleadr` image, both to be found
(`serverMdns`) and to find (`discover`).

## v0.4.0 (released)

The `elaadnl` client beyond the five object types: subscriptions,
resource groups, notifiers, MQTT topics, listening on the broker, and a
client without a token.

## v0.5.0 (released)

A server of OpenADR 3.0.1: the `openleadr301` image, OpenLEADR v0.1.4, the
last release of OpenADR 3.0. Fixtures of either revision, the
`baseline-301` fixture, and pairings by shared revision.

| Check | Result |
|---|---|
| `make smoke` | openleadr 33, elaadnl 44, openleadr301 32 checks |
| `make interop` | elaadnl → openleadr: 73 pass, 5 skipped on capabilities; elaadnl → openleadr301 not paired (3.1.0 against 3.0.1) |
| `make conformance` | openleadr: 20 of 24, 4 known findings; openleadr301 not run (the suite is of 3.1.0) |

## v0.6.0 (released)

Self-test results as the organisation's interop-result-v1: `reports/results.json`,
stable scenario IDs in `tests/cases.tsv`, and a fifth column in
`tests/known-bugs.tsv`, the evidence URL of a waiver. The adapters, the
fixtures and the container contract do not change.

## v0.6.1 (released)

The `openleadr` adapter moves to OpenLEADR v0.2.8, which answers a forged
bearer token with 401: conformance AUTH-003 passes and is no longer a known
finding, so `make conformance` gives 21 of 24 with 3 known findings. The
container contract and the fixtures do not change.

## v0.6.2 (released)

Policy 1.2 housekeeping: build artefacts move to `bin/` and `release/`, and the
shared workflows move to v3.3.0. The adapters, the container contract and the
fixtures do not change.

## v0.6.3 (released)

The `Release` workflow can be run by hand from the Actions tab with a patch,
minor or major bump. The adapters, the container contract and the fixtures do
not change.

## v0.7.0 (released)

The scripts take an image through `ADAPTER_IMAGE_<adapter>`, and `results.json`
records whether an implementation is independent from what its image says.
`own/go-openadr` and the manual workflow "Own stack (go-openadr)" build
`openadr-interop-otfabric`, OT Fabric's own stack, outside the releases. The
reference adapters, the container contract and the fixtures do not change.

## v0.8.0 (released)

A second independent stack, and the first in both roles: `cbsigma`, cB-Sigma
openadr v0.2.0 (Go), as a VTN and as Business Logic and VEN clients. It
pairs a second client with `openleadr` and a second server with `elaadnl`.
Contract 1.5 adds webhooks (`listen-webhook`, feature `clientWebhook`) and
the server feature `serverTargetsUnlock`, the reading of targets of the
`cbsigma` VTN. `elaadnl` moves to ElaadNL openadr3-client 2.4.2. The
fixtures do not change.

## v0.8.1 (released)

The README describes the `cbsigma` VTN and client and `listen-webhook`; the
contract says that the `status` of `listen-webhook` is the VTN's answer to
`POST /subscriptions`. The reference adapters, the container contract version
and the fixtures do not change.

## v0.8.2 (released)

`cbsigma` moves to cB-Sigma openadr v0.2.1, which fixes the read by ID of a
targeted program or event by a VEN (cB-Sigma/openadr#1); the alpine and
python base images move to their current digests. The container contract and
the fixtures do not change.

## Later

- **A client role for `openleadr`**, wrapping the `openleadr-client` crate:
  a second independent client, and with it cross-client comparison of result
  documents.
- **A third server.** Candidates: the OpenADR Alliance's reference
  implementation of the VTN, if its licence allows redistribution in an
  image.
- **More of `cbsigma`**: its VTN's MQTT notifier (with a broker in the
  image, so that `listen-mqtt` of `elaadnl` has a second broker), DNS-SD
  (`oadr3/mdns`), and OpenADR 3.0 (`vtn.Version30`), which would make it a
  3.0.1 server and, with its generic client, the client of 3.0.1 below.
- **Conformance scenarios** for the lifecycle of events, reports, VENs and
  resources at the depth programs have; token expiry; the request side
  (validating what a reference client sends against the request schemas).
- **Notifications beyond one webhook**: updates and deletes, notifications
  for programs, reports and vens, and MQTT delivery from a VTN other than
  OpenLEADR. v0.8.0 checks one: an event created, at a VEN's webhook.
- **A client of OpenADR 3.0.1**, so that `openleadr301` is in a pairing
  and the cross-stack cases run in 3.0.1 too (with cases of `baseline-301`).
  The pinned ElaadNL client is of 3.1.0; whether an earlier release speaks
  3.0.1 has not been checked.
- **Conformance scenarios of 3.0.1**, asserting its own OpenAPI document,
  so that `make conformance` covers `openleadr301`.
- **Resilience**: restarts and connection failures in the middle of a
  workflow.

## Order of work

Contract and fixtures first, then adapters, then self-tests, then
publishing. A consumer starts using an adapter when its image passes
`make smoke` and `make interop`; a finding about an upstream goes into
docs/COMPATIBILITY.md before it goes into a consumer's tests.
