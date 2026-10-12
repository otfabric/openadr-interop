# Conformance suite

A test suite for OpenADR 3.1.0 VTNs that asserts the specification itself:
every response is checked against the OpenAPI document the OpenADR Alliance
publishes, pinned here by checksum, and every scenario names the operations
and the sections of the Definition it checks.

It is independent of the adapters in this repository and of any
implementation: its own HTTP client, requests built as plain JSON, a
third-party OpenAPI validator. In this repository it runs against the
reference servers (`make conformance`), which shows where a reference
implementation and the specification part ways. Anyone can run it against
their own VTN.

**A passing run is not an OpenADR certification.** Certification is a
separate process of the [OpenADR Alliance](https://www.openadr.org/openadr-3-certification),
with its own test assets, none of which are here.

## Running it

Needs Go 1.25 or later and a running VTN with one Business Logic client and
at least one VEN client. Client secrets are read from the environment.

```sh
export OPENADR_INTEROP_BL_SECRET=...
export OPENADR_INTEROP_VEN_SECRET=...
export OPENADR_INTEROP_VEN2_SECRET=...   # optional second VEN

go run ./cmd/openadr-interop \
    -url http://127.0.0.1:8080 \
    -bl-client-id utility -ven-client-id site-1 -ven2-client-id site-2 \
    -target-name "My VTN" -target-version 1.2.3
```

The exit status is 0 when no scenario failed unexpectedly, 1 when one did,
and 2 when the run could not start. `-run '^REPORT-'` selects scenarios by
ID and `-list` prints them. The token endpoint is discovered with
`GET /auth/server` unless `-token-url` names it.

The VEN client should have no ven object of its own: the scenarios register
it and grant it targets, and some VTNs allow one ven object per client.
Objects get names unique to the run and are deleted again, so a run can be
repeated against a VTN that keeps its data. Do not point it at a production
VTN.

`-known FILE` names the scenarios that are known to fail against a version
of the target, with the text of the failure (see [known/](known)). Such a
failure is reported as a known finding. A listed scenario that fails
differently, or passes, fails the run, so the list can neither hide a new
failure nor outlive an old one.

## What a run produces

In `reports/` (or the directory given with `-out`):

| File | Content |
|---|---|
| `results.json` | Everything: suite and specification versions, the implementation under test, deviations and known findings, every scenario with its outcome and all HTTP exchanges |
| `junit.xml` | One test case per scenario, for CI systems |
| `report.md` | A summary for people, with the exchanges of each failed scenario |

Credentials never reach a report: bearer tokens, client secrets and issued
tokens are replaced by `[redacted]` when an exchange is recorded.

## Where the specification cannot be met

[spec/DEVIATIONS.md](spec/DEVIATIONS.md) lists the places where the OpenAPI
document cannot be satisfied as written and what the suite asserts instead.
There are two for 3.1.0: the `ven` and `resource` response schemas are
unsatisfiable, and `createVen` defines no 409 response. Every report repeats
the deviations that were applied.

## Layout

```text
cmd/openadr-interop/   the runner
scenarios/             the scenarios
spec/                  pinned OpenAPI documents, the validator, deviations
internal/probe/        HTTP client that records and redacts exchanges
internal/suite/        scenario framework
internal/report/       JSON, JUnit and Markdown writers
known/                 known findings per reference server
```
