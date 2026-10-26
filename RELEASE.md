# openadr-interop Releases

## How a release is made

1. `make ci` on the commit to release.
2. Update `interop_version` in `versions.yaml`, the `version` constant of the conformance suite, the status line and release link of README.md, the heading in PLAN.md, and the notes below under a heading `## vX.Y.Z` with a `**Date:**` line. `make validate` (`scripts/check-versions.sh`, `scripts/release-notes.sh`) fails when any of these names another version, when RELEASE.md has no notes for `interop_version`, or when the contract version of `versions.yaml` and docs/CONTAINER_CONTRACT.md differ.
3. Tag `vX.Y.Z` and push the tag. The release workflow runs one job on an amd64 runner and one on an arm64 runner: each validates (including the notes of the tag), builds every image natively with `ADAPTER_VERSION` set to the tag, runs the smoke test, the cross-stack self-test and the conformance suite on those images and pushes exactly them to GHCR as `<image>:vX.Y.Z-amd64` and `-arm64`. A last job joins the two into the multi-arch `<image>:vX.Y.Z` and creates the GitHub release with `manifest.json` (upstream pins and the digests of the multi-arch images) and `checksums.txt`.
4. Consumers pin the digests from `manifest.json`.

The notes are the body of the GitHub release, followed by the image digests:
everything after the heading `## vX.Y.Z` up to the next line that is exactly
`---` (`scripts/release-notes.sh vX.Y.Z` prints it). This is the format of
every otfabric repository and the rule of the shared release workflow, so:
one section per version, newest first, each ending with a `---` line, and no
`---` line inside a section.

Tags are immutable. A fix is a new version.

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New operation, behaviour, fixture, adapter, scenario or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

## Upstream releases

The `Upstream candidates` workflow runs every Monday and on demand. For each adapter it looks up the latest stable upstream release (`scripts/latest-upstream.py`: the git tags of OpenLEADR, PyPI for the ElaadNL client). Where the pin is behind, it builds a candidate image on that release and runs the smoke test, the cross-stack self-test against the other stacks at their pins and, for a server, the conformance suite. The result is a pin-versus-latest table in the job summary and a tracking issue, which is closed once every pin is the latest release again. The pins themselves are never changed by a workflow: [CONTRIBUTING.md](CONTRIBUTING.md#moving-an-upstream-pin) says how to move one.

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
