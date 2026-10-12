# Capabilities

Reference implementations do not all do the same, and some read the
specification differently. An image says what it is and what it does with
`print-capabilities`, so that a test can skip what a pairing cannot do
instead of failing on it, and so that no difference is silent.

Schema: [schemas/capabilities.schema.json](../schemas/capabilities.schema.json).

## Document

| Field | Meaning |
|-------|---------|
| `schemaVersion` | `"1.0"` |
| `adapter` | Name of the adapter |
| `adapterVersion` | Version of this repository the image was built from |
| `protocol` | `"openadr3"` |
| `openadrVersions` | Revisions the adapter speaks: `["3.1.0"]` |
| `upstream` | `name`, `version`, `revision`, `license`, `language` of the wrapped implementation; `independentEngine`: it shares no protocol code with another adapter |
| `roles` | `server`, `client`: booleans |
| `features` | See below; every feature is present in every document |
| `serverObjects` | Object types the server serves, of `programs`, `events`, `reports`, `vens`, `resources` |
| `clientOperations` | `bl` and `ven`: the operations the library offers each role |

## Features

| Feature | Meaning when true |
|---------|-------------------|
| `serverTokenEndpoint` | The VTN issues tokens itself, at `/auth/token` |
| `serverVenObjectType` | ven objects carry `objectType: "VEN"` |
| `serverInvalidTokenIs401` | A bearer token the VTN did not issue is answered 401 |
| `serverTargetsMustBeNamed` | A VEN sees a program or event that has targets only when its search names a target it was granted |
| `clientTokenDiscovery` | Without `--token-url` the client finds the token endpoint with `GET /auth/server` |
| `clientReportDescriptorsInPrograms` | The client reads a program whose `payloadDescriptors` hold a report payload descriptor |

A feature that is false is not necessarily a fault: `serverTargetsMustBeNamed`
is a reading of text the specification leaves open. [COMPATIBILITY.md](COMPATIBILITY.md)
says which is which.

## The adapters

| | `openleadr` | `elaadnl` |
|---|:---:|:---:|
| Upstream | OpenLEADR openleadr-rs v0.2.7 | ElaadNL openadr3-client 2.4.0 |
| Language | Rust | Python |
| Server role | yes | no |
| Client role | no | yes |
| `serverTokenEndpoint` | yes | |
| `serverVenObjectType` | no | |
| `serverInvalidTokenIs401` | no | |
| `serverTargetsMustBeNamed` | no | |
| `clientTokenDiscovery` | | yes |
| `clientReportDescriptorsInPrograms` | | no |

`elaadnl` offers a Business Logic client every operation except writing
reports, and a VEN client every operation except writing programs and events.
