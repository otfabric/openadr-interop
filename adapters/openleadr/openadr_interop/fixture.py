# SPDX-License-Identifier: MIT
"""Loading and validating a fixture: docs/FIXTURES.md.

This file is the same in every Python adapter (`make validate` checks that),
and implements the rules of scripts/validate-fixtures.py without needing a
JSON Schema library in the image.
"""

from __future__ import annotations

import json
import os
import re

SCHEMA_VERSION = "1.0"
OPENADR_VERSIONS = ("3.1.0", "3.0.1")
# The fixture each revision's servers default to.
BASELINE = {"3.1.0": "baseline", "3.0.1": "baseline-301"}

_LABEL = re.compile(r"^[a-z0-9][a-z0-9-]{0,63}$")
_CLIENT_ID = re.compile(r"^[a-zA-Z0-9_-]{1,128}$")
_CLIENT_SECRET = re.compile(r"^[a-zA-Z0-9_-]{8,128}$")
_TOP = {"schemaVersion", "name", "description", "openadrVersion", "clients", "vens", "resources", "programs", "events"}


class FixtureError(Exception):
    """The fixture is missing or invalid."""


def _name(where: str, value: object) -> str:
    if not isinstance(value, str) or not 1 <= len(value) <= 128:
        raise FixtureError(f"{where} must be a string of 1 to 128 characters")
    return value


def _targets(where: str, value: object, version: str) -> None:
    """Targets are strings in OpenADR 3.1.0, valuesMap objects in 3.0.1."""
    if value is None:
        return
    if not isinstance(value, list) or len(set(json.dumps(v, sort_keys=True) for v in value)) != len(value):
        raise FixtureError(f"{where} must be a list of distinct targets")
    for item in value:
        if version == "3.0.1":
            if not isinstance(item, dict) or set(item) != {"type", "values"}:
                raise FixtureError(f"{where}[] must be an object of type and values (OpenADR 3.0.1)")
            _name(f"{where}[].type", item["type"])
            values = item["values"]
            if not isinstance(values, list) or not values or not all(isinstance(v, (str, int, float, bool)) for v in values):
                raise FixtureError(f"{where}[].values must be a non-empty list of strings, numbers or booleans")
        else:
            _name(f"{where}[]", item)


def _objects(doc: dict, key: str, required: tuple[str, ...], allowed: tuple[str, ...] | None) -> list[dict]:
    version = doc.get("openadrVersion")
    items = doc.get(key)
    if not isinstance(items, list):
        raise FixtureError(f"{key} must be a list")
    for i, item in enumerate(items):
        if not isinstance(item, dict):
            raise FixtureError(f"{key}[{i}] must be an object")
        for name in required:
            if name not in item:
                raise FixtureError(f"{key}[{i}] has no {name}")
        if allowed is not None and set(item) - set(allowed):
            raise FixtureError(f"{key}[{i}] has unknown properties: {sorted(set(item) - set(allowed))}")
        _targets(f"{key}[{i}].targets", item.get("targets"), version)
    return items


def _unique(where: str, values: list) -> None:
    seen = set()
    for value in values:
        if value in seen:
            raise FixtureError(f"{where} {value!r} is used more than once")
        seen.add(value)


def validate(doc: object, directory_name: str | None = None) -> dict:
    """Check doc against every rule of docs/FIXTURES.md and return it."""
    if not isinstance(doc, dict):
        raise FixtureError("the fixture must be a JSON object")
    if set(doc) - _TOP:
        raise FixtureError(f"unknown properties: {sorted(set(doc) - _TOP)}")
    if doc.get("schemaVersion") != SCHEMA_VERSION:
        raise FixtureError(f"schemaVersion must be {SCHEMA_VERSION!r}")
    name = doc.get("name")
    if not isinstance(name, str) or not _LABEL.match(name):
        raise FixtureError("name must be lower-case letters, digits and hyphens")
    if directory_name is not None and name != directory_name:
        raise FixtureError(f"name is {name!r} but the fixture is in the directory {directory_name!r}")
    if "description" in doc and not isinstance(doc["description"], str):
        raise FixtureError("description must be a string")
    if doc.get("openadrVersion") not in OPENADR_VERSIONS:
        raise FixtureError(f"openadrVersion must be one of {list(OPENADR_VERSIONS)}")

    clients = _objects(doc, "clients", ("name", "role", "clientId", "clientSecret"), ("name", "role", "clientId", "clientSecret"))
    roles = {}
    for i, client in enumerate(clients):
        if not isinstance(client["name"], str) or not _LABEL.match(client["name"]):
            raise FixtureError(f"clients[{i}].name must be lower-case letters, digits and hyphens")
        if client["role"] not in ("bl", "ven"):
            raise FixtureError(f"clients[{i}].role must be bl or ven")
        if not isinstance(client["clientId"], str) or not _CLIENT_ID.match(client["clientId"]):
            raise FixtureError(f"clients[{i}].clientId must be 1 to 128 characters of [a-zA-Z0-9_-]")
        if not isinstance(client["clientSecret"], str) or not _CLIENT_SECRET.match(client["clientSecret"]):
            raise FixtureError(f"clients[{i}].clientSecret must be 8 to 128 characters of [a-zA-Z0-9_-]")
        roles[client["name"]] = client["role"]
    _unique("client name", [c["name"] for c in clients])
    _unique("clientId", [c["clientId"] for c in clients])
    if "bl" not in roles.values():
        raise FixtureError("there must be a client with the role bl: it creates the other objects")

    vens = _objects(doc, "vens", ("venName", "client"), ("venName", "client", "targets"))
    for i, ven in enumerate(vens):
        _name(f"vens[{i}].venName", ven["venName"])
        if roles.get(ven["client"]) != "ven":
            raise FixtureError(f"vens[{i}].client {ven['client']!r} is not a client with the role ven")
    _unique("venName", [v["venName"] for v in vens])

    resources = _objects(doc, "resources", ("resourceName", "ven"), ("resourceName", "ven", "targets"))
    ven_names = {v["venName"] for v in vens}
    for i, resource in enumerate(resources):
        _name(f"resources[{i}].resourceName", resource["resourceName"])
        if resource["ven"] not in ven_names:
            raise FixtureError(f"resources[{i}].ven {resource['ven']!r} is not a venName of the fixture")
    _unique("resource of a ven", [(r["ven"], r["resourceName"]) for r in resources])

    programs = _objects(doc, "programs", ("programName",), None)
    for i, program in enumerate(programs):
        _name(f"programs[{i}].programName", program["programName"])
    _unique("programName", [p["programName"] for p in programs])

    events = _objects(doc, "events", ("eventName", "program"), None)
    program_names = {p["programName"] for p in programs}
    for i, event in enumerate(events):
        _name(f"events[{i}].eventName", event["eventName"])
        if "programID" in event:
            raise FixtureError(f"events[{i}] names its program with program, not programID")
        if event["program"] not in program_names:
            raise FixtureError(f"events[{i}].program {event['program']!r} is not a programName of the fixture")
    # Tests look objects up by name.
    _unique("eventName", [e["eventName"] for e in events])
    return doc


def load(path: str) -> dict:
    """Read and validate the fixture at path, which is <dir>/<name>/fixture.json."""
    try:
        with open(path, encoding="utf-8") as handle:
            doc = json.load(handle)
    except OSError as exc:
        raise FixtureError(f"cannot read {path}: {exc.strerror}") from exc
    except ValueError as exc:
        raise FixtureError(f"{path} is not JSON: {exc}") from exc
    return validate(doc, os.path.basename(os.path.dirname(os.path.abspath(path))))


def served(doc: dict, versions: tuple[str, ...]) -> dict:
    """Refuse a fixture of a revision the adapter does not serve."""
    if doc["openadrVersion"] not in versions:
        raise FixtureError(f"the fixture is of OpenADR {doc['openadrVersion']}; this adapter serves {', '.join(versions)}")
    return doc


def client(doc: dict, name: str) -> dict | None:
    """The client of the fixture with the given name."""
    for candidate in doc["clients"]:
        if candidate["name"] == name:
            return candidate
    return None
