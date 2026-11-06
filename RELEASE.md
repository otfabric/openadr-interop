# openadr-interop Releases

## How a release is made

1. `make ci` on the commit to release.
2. Update `interop_version` in `versions.yaml`, the `version` constant of the conformance suite, the status line and release link of README.md, the heading in PLAN.md, and the notes below under a heading `## vX.Y.Z` with a `**Date:**` line. `make validate` (`scripts/check-versions.sh`, `scripts/release-notes.sh`) fails when any of these names another version, when RELEASE.md has no notes for `interop_version`, or when the contract version of `versions.yaml` and docs/CONTAINER_CONTRACT.md differ.
3. Run the `Release` workflow on `main` from the Actions tab and pick the bump (`patch` by default, `minor` or `major`) that gives `vX.Y.Z`, or push the tag `vX.Y.Z` yourself. The release workflow runs one job on an amd64 runner and one on an arm64 runner: each validates (including the notes of the tag), builds every image natively with `ADAPTER_VERSION` set to the tag, runs the smoke test, the cross-stack self-test and the conformance suite on those images and pushes exactly them to GHCR as `<image>:vX.Y.Z-amd64` and `-arm64`. A last job joins the two into the multi-arch `<image>:vX.Y.Z` and creates the GitHub release with `manifest.json` (upstream pins and the digests of the multi-arch images) and `checksums.txt`.
4. Consumers pin the digests from `manifest.json`.

The notes are the body of the GitHub release, followed by the image digests:
everything after the heading `## vX.Y.Z` up to the next line that is exactly
`---` (`scripts/release-notes.sh vX.Y.Z` prints it). This is the format of
every otfabric repository and the rule of the shared release workflow, so:
one section per version, newest first, each ending with a `---` line, and no
`---` line inside a section.

Tags are immutable. A fix is a new version.

The `openadr-interop-otfabric` image is not part of these releases. It has its own
manual workflow and no releases (`own/go-openadr/README.md`).

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New operation, behaviour, fixture, adapter, scenario or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

## Upstream releases

The `Upstream` workflow (`.github/workflows/upstream.yml`) runs daily and on demand. For each adapter it looks up the latest stable upstream release (`scripts/latest-upstream.py`: the git tags of OpenLEADR, of v0.1.x only for `openleadr301`, PyPI for the ElaadNL client). Where the pin is behind, it builds a candidate image on that release and runs the smoke test, the cross-stack self-test against the other stacks at their pins and, for a server, the conformance suite (once per pin and release; later runs reuse that result). It also checks the digest of every pinned base image. The result is a pin-versus-latest table in the job summary and the tracking issue "Upstream releases: pins behind the latest", which is closed once every pin is the latest release again. The pins themselves are never changed by a workflow: [CONTRIBUTING.md](CONTRIBUTING.md#moving-an-upstream-pin) says how to move one.

---

## v0.8.0

**Date:** 2026-11-06
**Previous release:** v0.7.0

### Added

- **`cbsigma`**: [cB-Sigma openadr](https://github.com/cB-Sigma/openadr) v0.2.0 (Go, Apache-2.0), an independent OpenADR 3.1.0 stack in both roles, published as `ghcr.io/otfabric/openadr-interop-cbsigma`. The server is its VTN (`oadr3/vtn`) with its own token endpoint and webhooks; the clients are its REST client (`oadr3.APIClient`), which checks a request body with the library's `oadr3.CheckRequest` before it is sent. It is a second client for `openleadr` and a second server for `elaadnl`, and its VTN passes the 24 conformance scenarios. A bug of its VTN found on the way, a VEN that cannot read a targeted object by ID, is [cB-Sigma/openadr#1](https://github.com/cB-Sigma/openadr/issues/1), tracked in [#19](https://github.com/otfabric/openadr-interop/issues/19). `go.mod` and `go.sum` of `adapters/cbsigma` hold the pin; `scripts/check-versions.sh` checks that they and the Dockerfile agree, and the Upstream workflow follows the tags of cB-Sigma/openadr.
- **Contract 1.5, webhooks:** the client operation `listen-webhook` (feature `clientWebhook`) serves a receiver, subscribes with the library's client and waits for notifications. `scripts/interop.sh` has a webhook scenario: a VEN subscribes, Business Logic creates an event, and the VTN notifies the VEN's webhook with the subscription's bearer token. `cbsigma` has it, and so does `own/go-openadr` (with go-openadr's `webhook.Handler`) from its next build.
- **Contract 1.5, `serverTargetsUnlock`:** the reading of targets of the `cbsigma` VTN. For a VEN, the targets a request names unlock the targeted objects it was granted and narrow nothing: a search naming targets returns the untargeted objects too, and a targeted object is not found by ID, as `GET /programs/{programID}` has no parameter to name its target. Cases of either reading run against the servers that have it.
- A case for `GET /notifiers` of a VTN without MQTT.

### Changed

- The workflow check of the event's start time accepts `+00:00` as well as `Z`: OpenLEADR writes the former, and a client that hands over the VTN's JSON as it is (`cbsigma`) passes it on.
- **`elaadnl`** moves to ElaadNL openadr3-client 2.4.2 (tag `134ff74e`), the latest release. Between 2.4.0 and 2.4.2 the package `openadr3_client` is unchanged. Its dependencies moved: oauthlib 3.3.1 to 4.0.1 (the library now requires 4), paho-mqtt 2.2.0, pydantic 2.14.3, certifi 2026.10.23, and the transitive security updates of 2.4.1. `requirements.txt` was regenerated with its hashes. The set of packages is the same, and the cross-stack results are those of 2.4.0.
- `needs` in `tests/cases.tsv` takes `server:!<feature>` and `client:!<feature>`: the case runs only where the feature is not declared.
- The Own stack workflow runs the cross-stack self-test against the adapters of the harness release's Makefile, so a release that adds an adapter adds it there too.

The fixtures do not change.

---

## v0.7.0

**Date:** 2026-11-06
**Previous release:** v0.6.3

### Added

- **`own/go-openadr/`** and the manual workflow **Own stack (go-openadr)**: go-openadr, OT Fabric's own stack, behind the container contract as a VTN and as its Business Logic and VEN clients, published as `ghcr.io/otfabric/openadr-interop-otfabric:<go-openadr version>` and `:latest` for others to test against. It is not an independent implementation (`upstream.independentEngine: false`) and not part of this or any release: `manifest.json` does not list it.
- **`ADAPTER_IMAGE_<adapter>`**: `scripts/smoke.sh`, `scripts/interop.sh` and `scripts/conformance.sh` take an image from outside the release set.

### Changed

- `results.json` takes `independent`, and for an image outside `versions.yaml` its revision, licence and upstream name, from what the image says of itself.
- The R1 check of CI also names `openadrctl`.

### Fixed

- `scripts/interop.sh` with more than one client adapter: a case that starts its own server read the object IDs of the previous server of the same name, so every client after the first was sent IDs that its server did not have (404, 409). The release set has one client and did not show it.

The reference adapters, the container contract and the fixtures do not change.

---

## v0.6.3

**Date:** 2026-11-01
**Previous release:** v0.6.2

### Changed

- The `Release` workflow can be run by hand from the Actions tab with a patch, minor or major bump, and checks that `versions.yaml` and RELEASE.md name that version before anything is built. The adapters, the container contract and the fixtures do not change.

---

## v0.6.2

**Date:** 2026-10-31
**Previous release:** v0.6.1

### Changed

- Policy 1.2: build output in `bin/` and `release/`, and shared CI at v3.3.0.

---

## v0.6.1

**Date:** 2026-10-30

**Previous release:** v0.5.0 (v0.6.0 was never tagged; its notes are below and ship in this release)

## Summary

The `openleadr` adapter moves from OpenLEADR v0.2.7 to v0.2.8. The container contract, the fixtures and `openleadr301` do not change.

## Changes

### Changed

- **`openleadr`**: OpenLEADR v0.2.8, commit `d1523514`.
- **`serverInvalidTokenIs401`** of `openleadr` is now true: a bearer token the VTN did not issue is answered 401 ([openleadr-rs#498](https://github.com/OpenLEADR/openleadr-rs/pull/498)). Conformance AUTH-003 passes and is no longer a known finding: 21 of 24 scenarios pass, with 3 known findings.
- **`conformance/known/openleadr.tsv`** is of v0.2.8, its permalinks at the commit of that release.

---

## v0.6.0

**Date:** 2026-10-30

**Previous release:** v0.5.0

## Summary

Self-test results as the organisation's interop-result-v1.

## Changes

### Added

- **`reports/results.json`** ([interop-result-v1](schemas/interop-result-v1.schema.json)): `scripts/interop.sh` writes it when `REPORT_DIR` is set, with stable scenario IDs (a new column of `tests/cases.tsv`) and the digests of the images. Only `pass` counts as passing; a run with no pass, or with a waiver that no longer reproduces, fails. Existing reports keep their shape.
- **`tests/known-bugs.tsv`** has a fifth column, the evidence URL of the waiver.

---

## v0.5.0

**Date:** 2026-10-29

## Summary

A reference server of OpenADR 3.0.1: `openleadr301`, OpenLEADR openleadr-rs
v0.1.4, the last release of OpenADR 3.0. Fixtures can be of either
revision, a new fixture `baseline-301` is the baseline in 3.0.1, and a
client and a server are paired only when they share a revision. Contract
1.4 and fixture schema 1.1: additions, nothing a consumer of 1.3 has to
change.

## Changes

### Added

- **The `openleadr301` image**, `ghcr.io/otfabric/openadr-interop-openleadr301`, for linux/amd64 and linux/arm64: OpenLEADR v0.1.4 (commit `92f4ba12d34cee0458af5846f170996d0b31978f`) with a private PostgreSQL, server role only, `openadrVersions` `["3.0.1"]`. No MQTT, no Multicast DNS, no notifiers. The VTN listens on port 3000 whatever it is told; the adapter forwards `--port` (default 8080) to it and refuses `--port 3000`.
- **The `baseline-301` fixture**: the clients of `baseline` with the same IDs and secrets, its ven, resource, programs and events, with targets as valuesMap objects. The targeted program and event name `VEN_NAME` `site-1`.
- **`"3.0.1"`** in `openadrVersions` of the capability schema, and in `openadrVersion` of the fixture schema, whose targets are strings in 3.1.0 and valuesMap objects in 3.0.1.
- `make smoke` checks of a 3.0.1 server (a token from `/auth/token`, valuesMap targets, a targeted program seen only by its VEN), and of every adapter refusing a fixture of a revision it does not serve or speak.

### Changed

- Every adapter refuses a fixture of a revision it does not serve: exit 4 for `server`, a usage error (exit 2) for `client`. The `openleadr` and `elaadnl` images serve and speak 3.1.0 only, as before.
- `make interop` pairs a client with a server only when they share an OpenADR version, and logs each pairing and each pair it leaves out. `elaadnl` → `openleadr301` is left out: 3.1.0 against 3.0.1.
- The conformance suite stays of 3.1.0 and runs against `openleadr` only: it asserts the 3.1.0 OpenAPI document, which 3.0.1 differs from in the shape of every targeted object.
- The weekly candidate probe covers `openleadr301`, following v0.1.x tags only.

### Fixed

- docs/COMPATIBILITY.md and the `elaadnl` adapter said that ElaadNL openadr3-client 2.4.0 names the operations of a subscription "as OpenADR 3.0.1 did". It does not: 3.0.1 names them `READ`, `CREATE`, `UPDATE` and `DELETE`, as 3.1.0 does. The library names them with HTTP methods, which 3.0.1 uses only for the `operation` of a notification. The v0.4.0 notes below say the same wrong thing.

### Upstream notes

Observed by running v0.1.4 with `baseline-301`; details in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

- No `/subscriptions` (404), which 3.0.1 defines.
- A VEN cannot delete its own report (403); only Business Logic can. While a report exists, its event and program cannot be deleted (409).
- `GET /programs?targetType=VEN_NAME&targetValues=…` finds nothing: program targets of that type are kept apart, as the list of VENs that may see the program. A VEN is shown such a program without its targets.
- A VEN client without a ven sees no programs, not even untargeted ones.
- A bearer token the VTN did not issue is answered 403, which the 3.0.1 OpenAPI document allows.

### Verification

On arm64: `make validate lint images smoke interop conformance-test documents conformance`. Smoke 33 (`openleadr`), 44 (`elaadnl`) and 32 (`openleadr301`) checks; self-test 73 checks passed and 5 skipped on declared capabilities, `elaadnl` → `openleadr301` not paired; conformance of `openleadr` 20 of 24 scenarios, 4 known findings; 3 capability documents and 65 client results valid against their schemas.

---

## v0.4.0

**Date:** 2026-10-13

## Summary

The ElaadNL reference client does more than read and write the five object
types: it manages subscriptions and resource groups, asks a VTN for its
notifiers and MQTT topics, listens on the broker the VTN names, and can run
without a token. Contract 1.3: additions, nothing a consumer of 1.2 has to
change.

## Changes

### Added

- **Subscriptions**: `list-subscriptions` and `get-subscription` for both roles, `create-`, `update-` and `delete-subscription` for a VEN, which is what the library offers.
- **Resource groups**, the extension to OpenADR 3.1.0 that OpenLEADR serves: the five operations for Business Logic, the two reads for a VEN, through the library's own resource group client.
- **`get-notifiers`** and **`get-topics --scope SCOPE [--id ID]`**: what the VTN says about its notifiers, and the MQTT topics it names for each of the twelve scopes of the specification.
- **`listen-mqtt`**: connects the library's MQTT client to the broker the VTN names, subscribes to a topic the VTN names, and reports the notifications it received.
- **`--anonymous ROLE`** in place of `--as`: the library's client without a token, for a VTN that asks for none.
- Features `clientAnonymous`, `clientResourceGroups`, `clientMqtt`, `clientSubscriptionOperations310`, `clientNotifiersWithoutMqtt`, `clientTopicsOfOneObject` of `elaadnl`; `serverResourceGroups` and `serverNotifiersWebhook` of `openleadr`. All optional: an image that does not name one is an older image.
- 17 cases in the cross-stack self-test for the above.

### Changed

- The lock of the `elaadnl` image is made from `openadr3-client[mqtt]`: it gains `paho-mqtt` 2.1.0 (EPL-2.0 OR BSD-3-Clause), and no other pin moved.

### Upstream notes

Details in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

- ElaadNL openadr3-client 2.4.0 names the operations of a subscription as OpenADR 3.0.1 did (`GET`, `POST`, `PUT`, `DELETE`). Against a 3.1.0 VTN only a subscription to `DELETE` can be made and read.
- It requires `MQTT` in `GET /notifiers` and `CREATE` and `ALL` in every topics answer, none of which the specification requires.
- It cannot read `GET /notifiers` of OpenLEADR v0.2.7, which leaves out `WEBHOOK`, and has no way to log in to an MQTT broker with a user name and a password: `listen-mqtt` of `elaadnl` does not work against the `openleadr` image.

### Verification

`make validate lint smoke interop` on arm64: smoke 31 and 42 checks, self-test 73 checks passed and 5 skipped on declared capabilities.

---

## v0.3.0

**Date:** 2026-10-13

## Summary

DNS-SD over Multicast DNS in the OpenLEADR reference image: its VTN
announces itself, and the image can look for VTNs with OpenLEADR's client
library. Contract 1.2: an addition, nothing a consumer of 1.1 has to change.

## Changes

### Added

- **The OpenLEADR VTN announces itself** as `_openadr3._tcp` (the feature `serverMdns`): OpenLEADR is now built with its `mdns` feature, and the adapter gives it the host name and the address of its container. `--mdns-name` sets the instance name, and the `ready` event carries it as `mdnsName`.
- **`discover`** (the feature `mdnsDiscover`): looks for VTNs on the local network with `discover_local_vtns` of `openleadr-client`, built as its own example program, and prints what it found as JSON.

### Upstream notes

Details in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

- OpenLEADR v0.2.7 announces `version=3.1` and a `local_url` with the trailing dot of the host name, and neither `requires_auth` nor `program_names`.
- Its mDNS responder answers by multicast only, also where RFC 6762 asks for a direct answer.

---

## v0.2.0

**Date:** 2026-10-13

## Summary

MQTT in the OpenLEADR reference server, and authorization scenarios in the
conformance suite. Contract 1.1: an addition, nothing a consumer of 1.0 has
to change.

## Changes

### Added

- **An MQTT broker in the `openleadr` image** (Mosquitto 2.0), to which the OpenLEADR VTN publishes its notifications: `--mqtt-port`, `--mqtt-public-url`, `mqttUrl` in the `ready` event, the feature `serverMqtt`. The broker knows the fixture's clients by client ID and secret.
- **Conformance scenarios AUTHZ-001 to AUTHZ-003**: every core operation without a token, a VEN on another VEN's vens and resources, and a VEN trying to grant itself targets. 24 scenarios in all.

### Upstream notes

Details in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

- OpenLEADR v0.2.7 names per-VEN MQTT topics in its topic operations that differ from the ones it publishes on, and its `GET /notifiers` does not follow the schema.
- OpenLEADR v0.2.7 creates a VEN's resource on the VEN's own ven whatever `venID` the request names.

---

## v0.1.0

**Date:** 2026-10-12

## Summary

The first version: two reference implementations of OpenADR 3.1.0 behind one container contract, fixtures, self-tests, and a conformance suite that asserts the specification.

## Changes

### Added

- Container contract 1.0 ([docs/CONTAINER_CONTRACT.md](docs/CONTAINER_CONTRACT.md)) with JSON Schemas for the capability and client result documents.
- Adapter `openleadr`: the OpenLEADR VTN v0.2.7 as a reference server, in one image with its PostgreSQL.
- Adapter `elaadnl`: the ElaadNL openadr3-client 2.4.0 as a reference VEN and Business Logic client.
- Fixture schema 1.0 and the fixtures `baseline` and `descriptors` ([docs/FIXTURES.md](docs/FIXTURES.md)).
- Self-tests: `make smoke`, `make interop` (48 cases and the workflow), `make conformance`.
- The conformance suite ([conformance/](conformance/README.md)): 21 scenarios against the pinned OpenAPI document, with JSON, JUnit and Markdown reports.
- CI on linux/amd64 and linux/arm64, the weekly upstream-candidate probe and the release workflow.

### Upstream notes

Details in [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

- OpenLEADR v0.2.7 answers a forged bearer token with 403, returns ven objects without `objectType`, shows a VEN targeted objects without the target being named, and allows one ven object per client.
- ElaadNL openadr3-client 2.4.0 cannot read a program that has a report payload descriptor.
- The OpenADR 3.1.0 OpenAPI document has two faults the conformance suite works around and declares: the `ven` and `resource` schemas are unsatisfiable, and `createVen` defines no 409.

### Not in this release

- A client role for `openleadr`; a second server.
- Subscriptions, webhooks, MQTT; OpenADR 3.0.x.

---
