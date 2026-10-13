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

## Later

- **A client role for `openleadr`**, wrapping the `openleadr-client` crate:
  a second independent client, and with it cross-client comparison of result
  documents.
- **A second server.** Candidates: the OpenADR Alliance's reference
  implementation of the VTN, if its licence allows redistribution in an
  image.
- **Conformance scenarios** for the lifecycle of events, reports, VENs and
  resources at the depth programs have; token expiry; the request side
  (validating what a reference client sends against the request schemas).
- **Subscriptions and notifications**: webhook delivery, MQTT topic
  discovery and delivery. OpenLEADR supports MQTT and not webhooks; the
  contract needs a way for a client to wait for a notification.
- **OpenADR 3.0.1**: ElaadNL's client speaks it; OpenLEADR does not. It
  needs a 3.0.1 server before it is worth a fixture.
- **Resilience**: restarts and connection failures in the middle of a
  workflow.

## Order of work

Contract and fixtures first, then adapters, then self-tests, then
publishing. A consumer starts using an adapter when its image passes
`make smoke` and `make interop`; a finding about an upstream goes into
docs/COMPATIBILITY.md before it goes into a consumer's tests.
