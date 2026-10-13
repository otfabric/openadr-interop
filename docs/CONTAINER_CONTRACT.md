# Container contract

Version: **1.2** (`contract_version` in [versions.yaml](../versions.yaml))

Every adapter image exposes the same commands, flags, JSON documents and exit
codes. Consumers depend on this document, not on any adapter's source. A
change that breaks it bumps the major version; an addition bumps the minor
one. Documents carry `schemaVersion` `"1.0"`: it names the compatible line
and changes only with the major version.

| Version | Change |
|---------|--------|
| 1.0 | First release |
| 1.1 | A server may run an MQTT broker (feature `serverMqtt`): `--mqtt-port`, `--mqtt-public-url`, `mqttUrl` in the `ready` event, 1883/tcp |
| 1.2 | A server may announce itself with DNS-SD over Multicast DNS (feature `serverMdns`): `--mdns-name`, `mdnsName` in the `ready` event. An image may look for VTNs (feature `mdnsDiscover`): the command `discover` |

An adapter has the server role, the client role, or both. What an image
lacks it says in [`print-capabilities`](#print-capabilities); a command or
operation it does not have is a usage error (exit 2).

What a *VTN* holds when it is ready is defined by its fixture: see
[FIXTURES.md](FIXTURES.md).

## Contents

- [Image layout](#image-layout)
- [Commands](#commands)
- [`server`](#server)
- [`client`](#client)
- [`print-capabilities`](#print-capabilities)
- [`print-fixture`](#print-fixture)
- [Exit codes](#exit-codes)
- [Output rules](#output-rules)

## Image layout

| Item | Value |
|------|-------|
| Image | `ghcr.io/otfabric/openadr-interop-<adapter>` |
| Entrypoint | `openadr-interop` (`/usr/local/bin/openadr-interop`) |
| Default command | `server`; `print-capabilities` for an image without the server role |
| Protocol | OpenADR 3.1.0, REST over plain HTTP |
| Port | 8080/tcp; 1883/tcp for the MQTT broker of a server with the feature `serverMqtt` |
| User | non-root, uid 10001 |
| Fixtures | `/fixtures/<name>/fixture.json` (baked in; mount over `/fixtures` to replace) |
| Readiness file | `/run/openadr-interop/ready` |
| Health check | built in for servers: `test -f /run/openadr-interop/ready` |
| Licences | `/licenses/` |

## Commands

```text
openadr-interop server [flags]
openadr-interop client <operation> --url URL --as CLIENT [flags]
openadr-interop print-capabilities
openadr-interop print-fixture [NAME]
openadr-interop version
```

Flags are `--name value`. An unknown flag, and a flag given twice, is a usage
error (exit 2), never ignored.

## `server`

Runs a VTN that serves one fixture until it receives SIGTERM or SIGINT.

| Flag | Default | Meaning |
|------|---------|---------|
| `--fixture PATH` | `/fixtures/baseline/fixture.json` | Fixture to serve |
| `--bind-address ADDR` | `0.0.0.0` | Listen address. An adapter whose VTN cannot choose accepts `0.0.0.0` only |
| `--port N` | 8080 | Listen port |
| `--public-url URL` | `http://<container hostname>:<port>` | The URL of the API as clients reach it |
| `--mqtt-port N` | 1883 | Listen port of the MQTT broker. Only with the feature `serverMqtt` |
| `--mqtt-public-url URL` | `mqtt://<container hostname>:<mqtt port>` | The broker as clients reach it, `mqtt://host:port`. Only with the feature `serverMqtt` |
| `--mdns-name NAME` | `openadr-interop-<adapter>` | The instance name under which the VTN announces itself as `_openadr3._tcp`: 1 to 63 letters, digits, spaces, `_` and `-`. Only with the feature `serverMdns` |
| `--ready-file PATH` | `/run/openadr-interop/ready` | Readiness file |

The API is served at the root of `--public-url`: `<url>/programs`,
`<url>/auth/server`, and so on. A VTN tells its clients where to get a token
(`GET /auth/server`), and that answer is an absolute URL, so **the server has
to be told the URL under which its clients reach it**. The default suits
clients on the same Docker network that use the container's host name; a
server started under another name, or published on a host port, needs
`--public-url`. The token endpoint is `<public-url>/auth/token`.

Lifecycle:

1. The fixture is loaded and validated. An invalid fixture ends the process
   with exit 4 before anything listens. The rules are those of
   `scripts/validate-fixtures.py`, including that `name` is the name of the
   directory the file is in: mount a fixture as `<dir>/<name>/fixture.json`.
2. The VTN is started. Failure, including a port that is taken, ends the
   process with a non-zero exit.
3. The clients of the fixture are registered and its objects are created.
4. The readiness file is written and the `ready` event is printed. From here
   on the VTN serves the fixture. **Readiness is the file (or the event), not
   the open port and not a delay**: the port answers before the fixture is
   loaded.
5. On SIGTERM/SIGINT the readiness file is removed, the VTN is stopped, the
   `stopped` event is printed and the process exits 0.

The state of the VTN lives as long as the process: restart the container to
return to the fixture.

### The MQTT broker

A server with the feature `serverMqtt` runs an MQTT broker in its container
and publishes its notifications there. `GET /notifiers` tells clients where
the broker is, and that answer is `--mqtt-public-url`. The VTN itself
connects to the broker under the same URL, so **the broker has to be
reachable under one URL from inside the container and from the client**: on
a Docker network that is the default; published on a host port, give the
broker the same port number inside and out
(`-p 127.0.0.1:18883:18883 ... --mqtt-port 18883 --mqtt-public-url mqtt://127.0.0.1:18883`).

The broker knows the clients of the fixture: **the user name is the client
ID and the password is the client secret**. (An implementation may announce
OAuth2 bearer token authentication, as OpenLEADR does; a stock broker cannot
check a token, and the reference broker does not try.) A Business Logic
client may read every topic. A VEN client may read the topics of programs,
events and vens, which is more than a deployment would allow: the broker is
there to carry what the VTN publishes, not to demonstrate access control.
No client may publish.

Which topics exist is the VTN's business and is found with its topic
operations; see [COMPATIBILITY.md](COMPATIBILITY.md) for what the references do.

### Server events

One JSON object per line on stdout.

| `event` | Other fields | When |
|---------|--------------|------|
| `ready` | `adapter`, `address` (`ip:port`), `url`, `tokenUrl`, `fixture`; `mqttUrl` with the feature `serverMqtt`; `mdnsName` with the feature `serverMdns` | Once, when the VTN serves the fixture |
| `stopped` | | Once, after a clean stop |

## `client`

Runs one bounded operation as an OpenADR client and prints one result
document.

```text
openadr-interop client <operation> --url URL --as CLIENT [common flags] [operation flags]
```

Every operation: obtain a token with the OAuth2 client-credentials grant,
send one request to the VTN, decode the answer.

### Common flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--url URL` | required | URL of the VTN's API, the one `/programs` is appended to |
| `--as CLIENT` | required | Name of a client of the fixture, such as `bl` or `ven-1`. Its role decides whether the library's Business Logic or VEN client is used |
| `--fixture PATH` | `/fixtures/baseline/fixture.json` | Where the client's ID and secret are taken from |
| `--token-url URL` | discovered | Token endpoint. Without it the client asks the VTN (`GET /auth/server`); only with the feature `clientTokenDiscovery` |
| `--timeout-ms N` | 10000 | Timeout of each HTTP exchange, 1..600000 |

Credentials never appear on a command line: a client is named, and its
secret is read from the fixture. To act as a client the VTN does not know,
or with a wrong secret, mount a fixture that says so.

### Operations

For each of `program`, `event`, `report`, `ven` and `resource`:

| Operation | Request | Flags |
|-----------|---------|-------|
| `list-<object>s` | `GET /<object>s` | search filters, see below |
| `get-<object>` | `GET /<object>s/{id}` | `--id ID` |
| `create-<object>` | `POST /<object>s` | `--body JSON` |
| `update-<object>` | `PUT /<object>s/{id}` | `--id ID`, `--body JSON` |
| `delete-<object>` | `DELETE /<object>s/{id}` | `--id ID` |

Search filters, each optional:

| Operation | Flags |
|-----------|-------|
| `list-programs` | `--targets T,T,...` |
| `list-events` | `--targets T,T,...`, `--program-id ID` |
| `list-reports` | `--program-id ID`, `--event-id ID`, `--client-name NAME` |
| `list-vens` | `--targets T,T,...`, `--ven-name NAME` |
| `list-resources` | `--targets T,T,...`, `--ven-id ID`, `--resource-name NAME` |
| all | `--skip N`, `--limit N`: given one, the other defaults to 0 and 50 |

`--body` is the request object in the JSON form of the specification
(`programRequest`, `eventRequest`, ...), and is handed to the library's own
request model: what reaches the VTN is what an application using the library
would send. For VENs and resources the `objectType` discriminator is the
library's business and need not be given; a Business Logic client sends the
Business Logic request, a VEN client the VEN request.

Which operations a library offers each role is in `clientOperations` of the
capability document. A library that follows the scopes of the specification
has no `create-program` for a VEN, for example: asking for it is a usage
error, and nothing is sent. A value that cannot be right — an ID with
characters an object ID cannot have, a body that is not a JSON object — is a
usage error too.

### Result document

Exactly one JSON object on one line of stdout, for every invocation that got
past argument parsing, including failures.

| Field | Type | Meaning |
|-------|------|---------|
| `schemaVersion` | string | `"1.0"` |
| `adapter` | string | Adapter that ran the operation |
| `operation` | string | As given on the command line |
| `client` | string | The fixture client, as given with `--as` |
| `role` | string | Its role: `bl` or `ven` |
| `ok` | boolean | The VTN answered with success and the library decoded the answer |
| `error` | object or null | `{ "code", "message" }` when `ok` is false |
| `authenticated` | boolean | The token endpoint issued a token |
| `status` | number or null | HTTP status of the VTN's answer to the operation; `null` when no answer was received |
| `response` | object, array or null | What the library decoded, in the JSON form of the specification: an array for a search, an object otherwise, `null` on failure |
| `problem` | object or null | The body of an error answer, when it is a JSON object |
| `elapsedMs` | number | Wall-clock duration |

`response` is the library's reading of the answer, not the bytes of it: a
property the library does not model is absent, and one it fills in with a
default is present.

`error.code`:

| Code | Meaning | Exit |
|------|---------|------|
| `connect-failed` | No connection to the VTN or to its token endpoint | 3 |
| `auth-failed` | No token: the token endpoint could not be found, or refused the client | 1 |
| `http-error` | The VTN answered the operation with an error status; see `status` and `problem` | 1 |
| `timeout` | The VTN did not answer in time | 1 |
| `invalid-request` | The library refused the request body. Nothing was sent | 1 |
| `invalid-response` | The VTN answered with success, and the library could not decode the answer | 1 |
| `failed` | Anything else; see `message` | 1 |

`message` is for people and not part of the contract.

`invalid-response` is the finding this contract exists to surface: two
implementations that each consider themselves correct and do not understand
each other.

## `discover`

Only with the feature `mdnsDiscover`. Looks for VTNs on the local network
with the implementation's own DNS-SD browser, for the service type
`_openadr3._tcp`, and prints one JSON object:

```json
{"schemaVersion":"1.0","adapter":"openleadr","vtns":[{"url":"https://vtn.local:8443/openadr3"}]}
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--timeout SECONDS` | 10 | How long to look, 1 to 300. The command returns earlier once it has found a VTN |

`url` is what the implementation takes as the address of a VTN, which for
OpenLEADR is the `local_url` of the TXT record. Finding none is an empty
list and exit 0. Multicast DNS reaches the containers of one Docker network,
not the host: run the command on the network of what it is to find.

A server with the feature `serverMdns` announces the host name and the
address of its container, and the port it listens on.

## `print-capabilities`

Prints one JSON object on one line and exits 0. See
[CAPABILITIES.md](CAPABILITIES.md) for the fields and the values of each
adapter. Needs no network and no fixture.

## `print-fixture`

`print-fixture [NAME]` (default `baseline`) writes the fixture baked into the
image to stdout, byte for byte, and exits 0. An unknown name exits 1.

A consumer reads client credentials and expected objects from the image it
tests against, so it needs no checkout of this repository and cannot drift
from the image.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success. `server`: clean stop after SIGTERM/SIGINT |
| 1 | The operation failed (see `error.code`); `server`: the VTN could not be started |
| 2 | Usage error: unknown command, operation or flag, missing or invalid value, a role the adapter or the library does not have. Nothing was sent |
| 3 | `client`: the connection could not be established |
| 4 | `server`: the fixture is missing or invalid |

## Output rules

- **stdout carries JSON only**: server events as JSON Lines, one client
  result, one capability document. `print-fixture` and `version` are the
  exceptions and print their own format.
- **stderr carries diagnostics** for people. Nothing on stderr is part of the
  contract. The logging of the VTN and of the libraries goes there too.
- Field names and exit codes are stable within a major contract version.
  Fields may be added in a minor version; consumers ignore fields they do not
  know.
- Every wait of a client is bounded by `--timeout-ms`. No client operation
  runs indefinitely.
