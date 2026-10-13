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
| `serverMqtt` | The image runs an MQTT broker and the VTN publishes its notifications there (contract 1.1) |
| `serverMdns` | The VTN announces itself with DNS-SD over Multicast DNS, as `_openadr3._tcp` (contract 1.2). Optional: an image that does not name it has not got it |
| `mdnsDiscover` | The image has the command `discover`, which looks for VTNs with the implementation's own browser (contract 1.2). Optional likewise |
| `clientTokenDiscovery` | Without `--token-url` the client finds the token endpoint with `GET /auth/server` |
| `clientReportDescriptorsInPrograms` | The client reads a program whose `payloadDescriptors` hold a report payload descriptor |
| `serverNotifiersWebhook` | `GET /notifiers` has `WEBHOOK`, as the specification requires (contract 1.3). Optional: only an image that says `false` is known to leave it out |
| `serverResourceGroups` | The VTN serves `/resource_groups`, an extension to OpenADR 3.1.0 (contract 1.3). Optional, as are the features below |
| `clientAnonymous` | The client can run without a token: `--anonymous ROLE` |
| `clientResourceGroups` | The client has the resource group operations |
| `clientMqtt` | The client has `listen-mqtt` |
| `clientSubscriptionOperations310` | The client names the operations of a subscription as OpenADR 3.1.0 does: `CREATE`, `READ`, `UPDATE`, `DELETE` |
| `clientNotifiersWithoutMqtt` | The client reads `GET /notifiers` of a VTN that has no MQTT broker |
| `clientTopicsOfOneObject` | The client reads the topics of one program or ven, which have no `CREATE` topic |

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
| `serverMqtt` | yes | |
| `serverMdns` | yes | |
| `mdnsDiscover` | yes | |
| `serverNotifiersWebhook` | no | |
| `serverResourceGroups` | yes | |
| `clientTokenDiscovery` | | yes |
| `clientReportDescriptorsInPrograms` | | no |
| `clientAnonymous` | | yes |
| `clientResourceGroups` | | yes |
| `clientMqtt` | | yes |
| `clientSubscriptionOperations310` | | no |
| `clientNotifiersWithoutMqtt` | | no |
| `clientTopicsOfOneObject` | | no |

`elaadnl` offers a Business Logic client every operation except writing
reports and subscriptions, and a VEN client every operation except writing
programs, events and resource groups. Both have `get-notifiers`, `get-topics`
and `listen-mqtt`.
