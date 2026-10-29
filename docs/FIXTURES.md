# Fixtures

A fixture says what a reference VTN holds when it reports ready: the OAuth2
clients it knows, and the VENs, resources, programs and events that exist.
Reference servers load it; reference clients take their credentials from it;
a consumer that tests its own VTN loads the same fixture into it, so that
the same client operations have the same expected outcome everywhere.

Schema version: **1.1** (`fixture_schema` in [versions.yaml](../versions.yaml)),
[fixtures/schema/openadr-fixture.schema.json](../fixtures/schema/openadr-fixture.schema.json).

## Document

`fixtures/<name>/fixture.json`:

| Field | Meaning |
|-------|---------|
| `schemaVersion` | `"1.0"` (schema 1.1 is an addition to it) |
| `name` | Name of the fixture; equals the name of its directory |
| `description` | For people |
| `openadrVersion` | `"3.1.0"` or `"3.0.1"`: the revision of every object in the fixture |
| `clients` | OAuth2 clients: `name`, `role` (`bl` or `ven`), `clientId`, `clientSecret` |
| `vens` | ven objects: `venName`, `client` (name of a `ven` client), optional `targets` |
| `resources` | resource objects: `resourceName`, `ven` (a `venName`), optional `targets` |
| `programs` | Each a program request of the fixture's revision |
| `events` | Each an event request of the fixture's revision, with `program` (a `programName`) in place of `programID` |

Targets are written as the revision writes them: in 3.1.0 a list of strings
(`["group-a"]`), in 3.0.1 a list of valuesMap objects
(`[{"type": "GROUP", "values": ["group-a"]}]`), each with a `type` and at
least one value, and no two alike.

Rules beyond the schema, checked by `scripts/validate-fixtures.py` and by
every adapter before it serves or uses a fixture:

- `name` is the name of the directory the file is in.
- Client names and client IDs are unique, and there is a `bl` client: it is
  the one that creates the objects.
- `venName`, `programName` and `eventName` are unique, and a `resourceName`
  is unique within its ven. (The specification does not require unique event
  names; tests look fixture objects up by name.)
- References resolve: a ven's `client` is a `ven` client, a resource's `ven`
  and an event's `program` exist.
- Targets have the form of `openadrVersion`, and the adapter serves or
  speaks that revision (`openadrVersions` of its capabilities): a server
  given a fixture of another revision exits 4, a client exits 2.

**The client secrets are not secrets.** They are test values, published
here and baked into every image, for VTNs that exist for the length of a
test.

## What a server does with it

1. Registers every client with the scopes the specification gives its role
   (3.1.0):
   Business Logic `read_all`, `write_programs`, `write_events`, `write_vens`,
   `write_subscriptions`; a VEN `read_targets`, `read_ven_objects`,
   `write_reports`, `write_vens`, `write_subscriptions`. An implementation
   that names or splits scopes differently is given its equivalents. OpenLEADR
   v0.1.4 (3.0.1) has no scopes but role tables: the `bl` client may manage
   VENs and act for any business, and the client of a ven is linked to it
   once the ven exists, before the server is ready.
2. As the `bl` client, through the VTN's own API and in this order, creates
   the vens (a Business Logic request carrying the client ID of the ven's
   client and the targets), the resources, the programs and the events. In
   3.0.1 a resource is created under its ven, `POST /vens/{venID}/resources`.

The IDs of the objects are the VTN's and differ from run to run: look
objects up by name.

Because the objects are created through the API, a fixture can only hold
what the VTN accepts from its Business Logic client. There are no reports in
a fixture: reports are written by VENs.

## The fixtures

### `baseline`

| | |
|---|---|
| Clients | `bl` (Business Logic); `ven-1`, `ven-2`, `ven-3` (VENs) |
| VENs | `site-1` of `ven-1`, granted the target `group-a`; `site-2` of `ven-2`, no targets. `ven-3` has no ven object |
| Resources | `battery` of `site-1` |
| Programs | `day-ahead`, open to all, with a `PRICE` event payload descriptor; `group-a-flex`, targeted at `group-a` |
| Events | `day-ahead-prices` in `day-ahead`: three hourly `PRICE` intervals from 2030-01-01T00:00:00Z, and a request for a `USAGE` report; `group-a-limit` in `group-a-flex`, targeted at `group-a`: one `IMPORT_CAPACITY_LIMIT` interval |

It is built so that the access rules show: `ven-1` can reach the targeted
program and event and `ven-2` cannot; each VEN has objects the other must
not see; `ven-3` is a client that has yet to register.

### `baseline-301`

The baseline in OpenADR 3.0.1, for a server of 3.0.1 (`openleadr301`): the
same clients with the same IDs and secrets, the same objects and names,
with targets as 3.0.1 writes them.

| | |
|---|---|
| Clients | `bl` (Business Logic); `ven-1`, `ven-2`, `ven-3` (VENs) |
| VENs | `site-1` of `ven-1`, targets `{"type": "GROUP", "values": ["group-a"]}`; `site-2` of `ven-2`, no targets. `ven-3` has no ven object |
| Resources | `battery` of `site-1` |
| Programs | `day-ahead`, open to all, with a `PRICE` event payload descriptor; `group-a-flex`, targeted at `{"type": "VEN_NAME", "values": ["site-1"]}` |
| Events | `day-ahead-prices` in `day-ahead`: three hourly `PRICE` intervals from 2030-01-01T00:00:00Z, and a request for a `USAGE` report; `group-a-limit` in `group-a-flex`, targeted at `VEN_NAME` `site-1`: one `IMPORT_CAPACITY_LIMIT` interval of 11.5 |

The targeted program and event name the ven rather than its group: that is
how OpenLEADR v0.1.4 decides which VENs see a program. `ven-1` sees
`group-a-flex` and `ven-2` does not (checked by `make smoke`).

### `descriptors`

One program, `prices-and-usage`, whose `payloadDescriptors` hold an event
payload descriptor and a report payload descriptor, as the specification
allows. It is separate from the baseline because a client that cannot read
such a program cannot list the programs of a VTN that has one (see
[COMPATIBILITY.md](COMPATIBILITY.md)).

## Adding a fixture

Add `fixtures/<name>/fixture.json`, run `make validate`, describe it here,
and add cases that use it to `tests/cases.tsv`. Images bake in every fixture
of the repository, so a new fixture arrives with the next release.
