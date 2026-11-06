# openadr-interop-otfabric

[go-openadr](https://github.com/otfabric/go-openadr), OT Fabric's own OpenADR 3
stack, behind the [container contract](../../docs/CONTAINER_CONTRACT.md), so that
other stacks can test against it. It has both roles: the server is the library's
VTN (`server.Server` with `auth.Issuer` as its token endpoint), and the client is
its Business Logic and VEN clients (`client.BL`, `client.VEN`). Anything that uses
an `openadr-interop-<adapter>` image can use this one in its place. Unlike those,
it is not a reference: its tags are go-openadr versions, and no harness release
or `manifest.json` contains it.

**For testing only.** It runs a VTN over plain HTTP with the client secrets of
the published fixtures. Do not expose it to a network you do not control. Old
images stay available, so check
[go-openadr advisories](https://github.com/otfabric/go-openadr/security/advisories)
for the version you pull.

**Not an independent implementation.** The image declares
`upstream.independentEngine: false`, and `results.json` records
`independent: false`. A result against it says nothing independent about
go-openadr when OT Fabric produces it. A result you publish, naming the
implementation and version, is independent evidence, and we would like to hear
about it.

## Use

```sh
docker run -d --name go-openadr -p 127.0.0.1:8080:8080 ghcr.io/otfabric/openadr-interop-otfabric:<tag> \
    server --public-url http://127.0.0.1:8080
until docker exec go-openadr test -f /run/openadr-interop/ready; do sleep 0.1; done
docker run --rm ghcr.io/otfabric/openadr-interop-otfabric:<tag> print-fixture    # the clients and their secrets
```

`<tag>` is a go-openadr version such as `v0.6.0`, or `latest`, the newest one
published. A rebuild of a version replaces its tag, so pin the digest if you need
an exact build. The digest is in the summary of the
[workflow run](https://github.com/otfabric/openadr-interop/actions/workflows/own-go-openadr.yml),
and `docker run --rm <image> version` prints the versions.

| | |
|---|---|
| Upstream | go-openadr, the latest release when the image was built |
| Licence | MIT; every module's licence is in the image under `/licenses` |
| Adapter name | `otfabric` (the name in documents, as the schema allows) |
| Language | Go |
| Protocol engine | OT Fabric's own |
| OpenADR | 3.1.0 |

The VTN keeps its objects in memory (`storage.NewMemory`), issues its own tokens
at `/auth/token`, serves subscriptions with `webhook.Sender` (plain http and
private addresses allowed, for a test network) and serves resource groups. It
answers no request without a token. The fixture is loaded through the VTN's own
API, as its Business Logic client.

`listen-webhook` (contract 1.5) serves `webhook.Handler` as the receiver and
writes the subscription with the library's client for the role.

A request body (`--body`) is read with the library's own codec
(`storage.Decode`) and the answer written with it (`storage.Encode`), so what
reaches the VTN is what an application using go-openadr would send.

## Why it is apart from the other adapters

`REQUIREMENTS.md` R1: nothing under `adapters/`, `scripts/`, `tests/`,
`conformance/` or the top-level `Makefile` knows a library under test, and CI
enforces that. This directory and `.github/workflows/own-go-openadr.yml` are the
one exception. The scripts take the image through `ADAPTER_IMAGE_otfabric`.

No release loop can form (`otfabric/engineering` ADR-0012). This image is built
by hand, from the latest go-openadr release, and nothing is committed: this
repository has no pin of go-openadr to bump. No release or required check of
go-openadr needs the image, and go-openadr commits no pin of it. It is not in a
harness release or `manifest.json`.

## Publishing

Run the **Own stack (go-openadr)** workflow by hand, from `main`. It finds the
latest go-openadr release and the latest harness release, and prints them. It
builds the image, runs the smoke test, the cross-stack self-test against
the reference adapters of that harness release (`ADAPTERS` of its Makefile), and the conformance suite against its
VTN, on both architectures, then publishes `:<go-openadr version>` and
`:latest`. Run from a branch, it stops before publishing. Nothing runs it
automatically, so after a go-openadr release the image stays on the old version
until someone does.

```sh
make -f own/go-openadr/Makefile image smoke interop    # from the repository root
```

## What it cannot do

Declared `false`:

- `serverMqtt`: go-openadr publishes notifications to an MQTT broker
  (`notification/mqtt`) and has none of its own. The image runs no broker, so the
  VTN has no MQTT binding and its topic operations answer 501.
- `serverMdns`, `mdnsDiscover`: package `discovery` can announce a VTN and look
  for one, but no self-test of this repository exercises DNS-SD, so the image
  has neither `--mdns-name` nor `discover`.
- `clientMqtt`: the library's MQTT subscriber hands over decoded notifications,
  not the payloads `listen-mqtt` reports, and no self-test exercises it. The
  client has no `listen-mqtt`.

`openadrVersions` is `["3.1.0"]`. The clients of go-openadr speak OpenADR 3.0.1
too, but its VTN serves 3.1.0 only, and a capability document has one list for
both roles: declaring 3.0.1 would claim a 3.0.1 server. So the client is not
paired with `openleadr301` in `scripts/interop.sh`; go-openadr's own interop
suite tests it against that VTN.

`--limit 0` is not sent: the library leaves the page size to the VTN when the
limit is 0, which returns up to 50 objects.
