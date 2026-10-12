# openadr-interop Releases

## How a release is made

1. `make ci` on the commit to release.
2. Update `interop_version` in `versions.yaml`, the `version` constant of the conformance suite (`scripts/check-versions.sh` requires the two to agree) and the notes below.
3. Tag `vX.Y.Z` and push the tag. The release workflow runs one job on an amd64 runner and one on an arm64 runner: each validates, builds every image natively with `ADAPTER_VERSION` set to the tag, runs the smoke test, the cross-stack self-test and the conformance suite on those images and pushes exactly them to GHCR as `<image>:vX.Y.Z-amd64` and `-arm64`. A last job joins the two into the multi-arch `<image>:vX.Y.Z` and creates the GitHub release with `manifest.json` (upstream pins and the digests of the multi-arch images) and `checksums.txt`.
4. Consumers pin the digests from `manifest.json`.

Tags are immutable. A fix is a new version.

| What changed | Version |
|--------------|---------|
| Contract or fixture format changed incompatibly | major |
| New operation, behaviour, fixture, adapter, scenario or document field | minor |
| Upstream pin, base image or bug fix with unchanged contract | patch |

## Upstream releases

The `Upstream candidates` workflow runs every Monday and on demand. For each adapter it looks up the latest stable upstream release (`scripts/latest-upstream.py`: the git tags of OpenLEADR, PyPI for the ElaadNL client). Where the pin is behind, it builds a candidate image on that release and runs the smoke test, the cross-stack self-test against the other stacks at their pins and, for a server, the conformance suite. The result is a pin-versus-latest table in the job summary and a tracking issue, which is closed once every pin is the latest release again. The pins themselves are never changed by a workflow: [CONTRIBUTING.md](CONTRIBUTING.md#moving-an-upstream-pin) says how to move one.

---

## Unreleased (planned v0.1.0)

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
