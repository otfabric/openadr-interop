# Compatibility

What is pinned, what the reference stacks do with each other, and what was
found on the way. Everything here was observed with the versions named, and
is what CI and the release workflow reproduce on linux/amd64 and linux/arm64.

## Pinned upstreams

| Adapter | Upstream | Version | Roles | Licence |
|---------|----------|---------|-------|---------|
| `openleadr` | [OpenLEADR openleadr-rs](https://github.com/OpenLEADR/openleadr-rs) | v0.2.7, commit `f586cc3c` | server | Apache-2.0 OR MIT |
| `elaadnl` | [ElaadNL openadr3-client](https://github.com/ElaadNL/openadr3-client) | 2.4.0 (PyPI wheel) | client | Apache-2.0 |

The full pins, with base image digests, are in [versions.yaml](../versions.yaml).

## Cross-stack matrix

`make interop`: every case of [tests/cases.tsv](../tests/cases.tsv) and the
program → event → report workflow, for every client against every server.

| Client | Server | Result |
|--------|--------|--------|
| `elaadnl` 2.4.0 | `openleadr` v0.2.7 | 59 checks pass, 2 cases skipped on declared capabilities |

The two skipped cases are the ones the capabilities below explain.

## Conformance of the reference servers

`make conformance`: the scenarios of [conformance/](../conformance), which
assert the specification's own OpenAPI document, against each reference
server.

| Server | Result |
|--------|--------|
| `openleadr` v0.2.7 | 20 of 24 scenarios pass; 4 known findings |

A consumer that compares itself with a reference has to know where the
reference and the specification part ways. That is what the findings are.

## Findings

A finding is not automatically a bug: some are places where the
specification can be read more than one way. Each says which it is. None has
been reported upstream yet.

### OpenLEADR v0.2.7

**A bearer token the VTN did not issue is answered 403** (conformance
AUTH-003, feature `serverInvalidTokenIs401`). A request without a token gets
401; one with a forged token gets 403 with the detail "Invalid authentication
token provided". The Definition says that the VTN "returns an Unauthorized
response if invalid or expired". A client that fetches a new token on 401
does not do so here.

**ven objects have no `objectType`** (conformance VEN-001 and TARGET-001,
feature `serverVenObjectType`). Resource objects have `"RESOURCE"`; ven
objects leave the property out, which `objectMetadata` requires. The schema
of `ven` cannot be met as written (deviation SPEC-310-001 in
[conformance/spec/DEVIATIONS.md](../conformance/spec/DEVIATIONS.md)), which
may be why. A client should not require the property.

**A VEN sees targeted objects without naming the target** (conformance
TARGET-002, feature `serverTargetsMustBeNamed`). A VEN that was granted a
target is shown the programs and events that carry it in any search. The
Definition asks for more: reads of objects with targets "must include
matching targets in the query". Privacy holds either way: a VEN without the
grant sees nothing. This is a reading of unclear text more than a fault, and
the more convenient one for a VEN.

**The MQTT topics it names for a ven are not the ones it publishes on.**
`GET /notifiers/mqtt/topics/vens/{venID}/events` answers
`events/vens/{venID}/create` (and so on, likewise for programs and
resources), while the notifications for that VEN are published on
`vens/{venID}/events/create`. A client that subscribes to what it was told
hears nothing. The topics for all objects of a kind (`events/create`, ...)
agree with what is published. Found with a subscriber on the reference
broker; a fault of the implementation.

**`GET /notifiers` does not follow the schema.** `WEBHOOK`, which the schema
requires, is absent; the authentication method is written
`OAUTH2BEARERTOKEN` where the schema has `OAUTH2_BEARER_TOKEN`; and there
are members the schema does not have (`WEBSOCKET`, `PUSH-MQTT`). A client
that reads the binding leniently is not affected.

**The MQTT topic operations for all programs and all events are open to
VENs**, where the document asks for `read_all`. OpenLEADR publishes there
only what every client may see, so this is a different, and coherent,
division of topics rather than a leak.

**A VEN's resource goes on its own ven, whatever `venID` it names.** A
`VEN_RESOURCE_REQUEST` that names another client's ven is answered 201, and
the resource is created on the requester's ven: the `venID` of the request
is ignored, and the answer carries the one that was used. Nothing of the
other client is touched. A client that relies on the `venID` it sent, rather
than the one in the answer, is misled; a VEN with several ven objects cannot
choose between them (OpenLEADR allows one per client).

**One ven object per client.** A second ven object for a client ID that has
one is answered 409, whether the VEN or Business Logic asks. The
specification neither requires nor forbids this. The baseline fixture has a
client without a ven object (`ven-3`) so that registration can be tested.

**Durations are written in full.** `PT1H` comes back as `P0Y0M0DT1H0M0S`,
which is the same duration and matches the schema's pattern. Compare
durations, not strings.

**Scopes are split by role.** `write_vens_bl` and `write_vens_ven`,
`write_reports_ven`, and so on, with the names of the specification kept as
aliases for the VEN variants. The adapter gives the fixture's clients the
scopes OpenLEADR's own test fixture gives each role.

### ElaadNL openadr3-client 2.4.0

**A program with a report payload descriptor is an invalid response**
(feature `clientReportDescriptorsInPrograms`, fixture `descriptors`).
`programRequest.payloadDescriptors` may hold `EVENT_PAYLOAD_DESCRIPTOR` and
`REPORT_PAYLOAD_DESCRIPTOR` objects. The library's program model accepts the
first only, so reading such a program fails validation — and with it any
search whose result contains one. This is a fault of the library against the
3.1.0 schema.

**The token endpoint has to be an absolute URL.** The library hands the
`tokenURL` of `GET /auth/server` to its OAuth2 library as it is. The schema
says `format: uri`, so this is correct; a VTN that answers with a relative
reference such as `auth/token` — the form the specification's own
`securitySchemes` uses — cannot be used with discovery.

**Plain HTTP is refused unless asked for.** The library enforces HTTPS, as
OpenADR 3.1 requires of deployments. The adapter passes
`allow_insecure_http` and sets `OAUTHLIB_INSECURE_TRANSPORT` when it is given
an `http://` URL, because reference servers on a test network have no
certificate.

**What the library offers each role follows the scopes of the
specification.** There is no way to ask it to create a program as a VEN, so
a consumer cannot use this client to test that its VTN refuses that.

## Platforms

Built and self-tested natively on linux/amd64 and linux/arm64, in CI and for
every release.
