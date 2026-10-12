# Deviations from the specification

The assertions of this suite follow the pinned OpenAPI document. In the
places below the document cannot be met as written, and the suite asserts
something else. Each deviation is applied in `spec/spec.go`, covered by a
test in `spec/spec_test.go`, and repeated in every report, so that a result
can never rest on an undeclared reading.

A deviation is removed when a later revision of the specification resolves
it; the code refuses to load a document that no longer has the fault.

## SPEC-310-001: the `ven` and `resource` schemas are unsatisfiable

`components.schemas.ven` is `allOf` of `objectMetadata` and `BlVenRequest`.
`objectMetadata.objectType` must be one of `PROGRAM`, `EVENT`, `REPORT`,
`SUBSCRIPTION`, `VEN`, `RESOURCE`. `BlVenRequest.objectType` must be
`BL_VEN_REQUEST`. Both are required, and no value is both. The same holds
for `resource`, with `BlResourceRequest` and `BL_RESOURCE_REQUEST`.

**Asserted instead:** in `ven` and `resource` responses the enum of the
request discriminator is dropped. `objectType` then has to be an object
type; the scenarios require `VEN` and `RESOURCE` respectively, in line with
"Object Metadata" in the Definition. Everything else in both schemas applies
unchanged.

## SPEC-310-002: `createVen` defines no 409 response

The Definition ("Object names") requires `venName` to be unique within a
VTN, and lists `409 Conflict (item already exists)` among the codes a VTN
SHALL support for POST. The OpenAPI document defines 409 for every other
create operation, but not for `createVen` (201, 400, 401, 403, 500).

**Asserted instead:** a 409 from `createVen` is accepted and its body is
checked against the `conflict` response the document defines elsewhere.

## Readings of ambiguous text

These are not deviations, but places where the text allows more than one
behaviour and the scenarios accept each of them.

- **A VEN reading another client's object by ID.** The Definition ("VEN
  created object privacy") asks for an empty set in searches and does not
  say what a read, update or delete by ID returns. REPORT-003 accepts 403
  and 404.
- **The `read_bl` scope.** Six MQTT topic operations require a scope
  `read_bl` that `securitySchemes` does not declare. No scenario depends on
  it yet.
- **`/auth/token` errors.** The OpenAPI document gives the `problem` schema
  for 400 and 401 of `/auth/token`, while its `authError` schema, which
  nothing references, describes the RFC 6749 error body. No scenario asserts
  the body of a failed token request yet.
