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
| `openadrVersions` | Revisions the adapter speaks, of `3.1.0` and `3.0.1` (contract 1.4). A client and a server are paired only when they share one |
| `upstream` | `name`, `version`, `revision`, `license`, `language` of the wrapped implementation; `independentEngine`: it shares no protocol code with another adapter, and is not OT Fabric's own stack (`own/`). Results from an adapter where it is `false` do not count as an additional independent implementation |
| `roles` | `server`, `client`: booleans |
| `features` | See below; every feature is present in every document |
| `serverObjects` | Object types the server serves, of `programs`, `events`, `reports`, `vens`, `resources` |
| `clientOperations` | `bl` and `ven`: the operations the library offers each role |

## Features

| Feature | Meaning when true |
|---------|-------------------|
| `serverTokenEndpoint` | The VTN issues tokens itself, at `/auth/token`. In 3.0.1, which has no `GET /auth/server`, that is where every client goes |
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
| `clientWebhook` | The client has `listen-webhook` (contract 1.5) |
| `serverTargetsUnlock` | For a VEN, the targets a request names unlock the targeted objects it was granted and narrow nothing: a search that names targets returns the objects without targets too, and a targeted object is read by ID only with its target in the query (contract 1.5). Without it, targets in a VEN's search narrow it to the objects that carry one, and a VEN reads a targeted object it was granted by ID |

A feature that is false is not necessarily a fault: `serverTargetsMustBeNamed`
is a reading of text the specification leaves open. [COMPATIBILITY.md](COMPATIBILITY.md)
says which is which.

## The adapters

| | `openleadr` | `elaadnl` | `openleadr301` | `cbsigma` |
|---|:---:|:---:|:---:|:---:|
| Upstream | OpenLEADR openleadr-rs v0.2.8 | ElaadNL openadr3-client 2.4.0 | OpenLEADR openleadr-rs v0.1.4 | cB-Sigma openadr v0.2.0 |
| OpenADR | 3.1.0 | 3.1.0 | 3.0.1 | 3.1.0 |
| Language | Rust | Python | Rust | Go |
| Server role | yes | no | yes | yes |
| Client role | no | yes | no | yes |
| `serverTokenEndpoint` | yes | | yes | yes |
| `serverVenObjectType` | no | | yes | yes |
| `serverInvalidTokenIs401` | yes | | no | yes |
| `serverTargetsMustBeNamed` | no | | no | yes |
| `serverMqtt` | yes | | no | no |
| `serverMdns` | yes | | no | no |
| `mdnsDiscover` | yes | | no | no |
| `serverNotifiersWebhook` | no | | no | yes |
| `serverResourceGroups` | yes | | no | no |
| `clientTokenDiscovery` | | yes | | yes |
| `clientReportDescriptorsInPrograms` | | no | | yes |
| `clientAnonymous` | | yes | | yes |
| `clientResourceGroups` | | yes | | no |
| `clientMqtt` | | yes | | no |
| `clientSubscriptionOperations310` | | no | | yes |
| `clientNotifiersWithoutMqtt` | | no | | yes |
| `clientTopicsOfOneObject` | | no | | yes |
| `clientWebhook` | | no | | yes |
| `serverTargetsUnlock` | no | | no | yes |

`elaadnl` offers a Business Logic client every operation except writing
reports and subscriptions, and a VEN client every operation except writing
programs, events and resource groups. Both have `get-notifiers`, `get-topics`
and `listen-mqtt`.

`cbsigma` offers a Business Logic client every operation except writing
reports, and a VEN client every operation except writing programs and
events, as the scopes of OpenADR 3.1.0 have it; it has no resource groups.
Both have `get-notifiers`, `get-topics` and `listen-webhook`. Its client is
the library's REST client, which keeps objects as JSON: `response` is the
VTN's answer as decoded JSON, after the library's checks of the request.
The library has no webhook receiver: the receiver of `listen-webhook` is the
adapter's. Not wired, though the library has them: the VTN's MQTT notifier
(it publishes through a broker client the application supplies), DNS-SD
(`oadr3/mdns`) and OpenADR 3.0 (`vtn.Version30`).

`openleadr301` serves OpenADR 3.0.1 only: no broker, no announcement, no
notifiers, no subscriptions, no resource groups, and no `--mqtt-port`,
`--mqtt-public-url`, `--mdns-name` or `discover`. Its server features were
read off the running VTN; see [COMPATIBILITY.md](COMPATIBILITY.md).
