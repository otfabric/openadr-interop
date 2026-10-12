# SPDX-License-Identifier: MIT
"""Command line of the adapter: the container contract."""

from __future__ import annotations

import json
import os
import re
import sys

from . import ADAPTER, SCHEMA_VERSION
from .fixture import FixtureError, client as fixture_client, load

FIXTURES_DIR = "/fixtures"
BUILD_INFO = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "build-info.json")
_FIXTURE_NAME = re.compile(r"^[a-z0-9][a-z0-9-]*$")
_ID = re.compile(r"^[a-zA-Z0-9_-]{1,128}$")

OBJECTS = ("program", "event", "report", "ven", "resource")
VERBS = ("list", "get", "create", "update", "delete")

# What the library offers each role. It follows the scopes of the
# specification: Business Logic does not write reports, a VEN does not write
# programs or events.
CLIENT_OPERATIONS = {
    "bl": [f"{verb}-{obj}{'s' if verb == 'list' else ''}"
           for obj in OBJECTS for verb in VERBS if obj != "report" or verb in ("list", "get")],
    "ven": [f"{verb}-{obj}{'s' if verb == 'list' else ''}"
            for obj in OBJECTS for verb in VERBS if obj not in ("program", "event") or verb in ("list", "get")],
}
ALL_OPERATIONS = sorted(set(CLIENT_OPERATIONS["bl"]) | set(CLIENT_OPERATIONS["ven"]))

# The search filters of each object type, as flag name -> library argument.
LIST_FILTERS = {
    "program": {},
    "event": {"program-id": "program_id"},
    "report": {"program-id": "program_id", "event-id": "event_id", "client-name": "client_name"},
    "ven": {"ven-name": "ven_name"},
    "resource": {"ven-id": "ven_id", "resource-name": "resource_name"},
}
# Report searches take no targets.
TARGET_FILTER = {"program", "event", "ven", "resource"}

USAGE = """usage:
  openadr-interop client <operation> --url URL --as CLIENT [--fixture PATH] [--token-url URL]
                         [--timeout-ms N] [operation flags]
  openadr-interop print-capabilities
  openadr-interop print-fixture [NAME]
  openadr-interop version
operations: """ + ", ".join(ALL_OPERATIONS) + """
This adapter has no server role."""


class UsageError(Exception):
    """Exit 2: nothing was sent."""


def flags(args: list[str], known: set[str]) -> dict[str, str]:
    """Parse `--name value` pairs; anything else is a usage error."""
    result: dict[str, str] = {}
    i = 0
    while i < len(args):
        name = args[i]
        if not name.startswith("--") or name[2:] not in known:
            raise UsageError(f"unknown flag or argument {name!r}")
        if i + 1 >= len(args):
            raise UsageError(f"{name} needs a value")
        if name[2:] in result:
            raise UsageError(f"{name} is given more than once")
        result[name[2:]] = args[i + 1]
        i += 2
    return result


def number(flag: str, text: str, low: int, high: int) -> int:
    if not text.isdigit() or not low <= int(text) <= high:
        raise UsageError(f"--{flag}: {text!r} is not a number within {low}..{high}")
    return int(text)


def object_id(flag: str, text: str) -> str:
    if not _ID.match(text):
        raise UsageError(f"--{flag}: {text!r} is not an object ID")
    return text


def build_info() -> dict:
    try:
        with open(BUILD_INFO, encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return {}


def upstream_version() -> str:
    from importlib.metadata import version  # pylint: disable=import-outside-toplevel

    return version("openadr3-client")


def capabilities() -> dict:
    info = build_info()
    return {
        "schemaVersion": SCHEMA_VERSION,
        "adapter": ADAPTER,
        "adapterVersion": info.get("adapterVersion", "dev"),
        "protocol": "openadr3",
        "openadrVersions": ["3.1.0"],
        "upstream": {
            "name": "ElaadNL openadr3-client",
            "version": upstream_version(),
            "revision": info.get("upstreamRevision", "unknown"),
            "license": "Apache-2.0",
            "language": "Python",
            "independentEngine": True,
        },
        "roles": {"server": False, "client": True},
        "features": {
            "serverTokenEndpoint": False,
            "serverVenObjectType": False,
            "serverInvalidTokenIs401": False,
            "serverTargetsMustBeNamed": False,
            # Without --token-url the library asks the VTN: GET /auth/server.
            "clientTokenDiscovery": True,
            # The library reads a program whose payloadDescriptors hold a
            # REPORT_PAYLOAD_DESCRIPTOR as an invalid response: its program
            # model accepts event payload descriptors only.
            "clientReportDescriptorsInPrograms": False,
        },
        "serverObjects": [],
        "clientOperations": CLIENT_OPERATIONS,
    }


def parse_client(args: list[str]) -> dict:
    """Everything a client operation needs, checked; raises UsageError."""
    if not args:
        raise UsageError("client needs an operation")
    operation, rest = args[0], args[1:]
    if operation not in ALL_OPERATIONS:
        raise UsageError(f"unknown operation {operation!r}")
    verb, _, obj = operation.partition("-")
    obj = obj.removesuffix("s") if verb == "list" else obj

    known = {"url", "as", "fixture", "token-url", "timeout-ms"}
    if verb == "list":
        known |= {"skip", "limit"} | set(LIST_FILTERS[obj]) | ({"targets"} if obj in TARGET_FILTER else set())
    if verb in ("get", "update", "delete"):
        known.add("id")
    if verb in ("create", "update"):
        known.add("body")
    given = flags(rest, known)

    for name in ("url", "as") + (("id",) if "id" in known else ()) + (("body",) if "body" in known else ()):
        if name not in given:
            raise UsageError(f"--{name} is required")
    url = given["url"].rstrip("/")
    if not re.match(r"^https?://[^/\s]+(/[^\s]*)?$", url):
        raise UsageError(f"--url: {given['url']!r} is not an http or https URL")
    token_url = given.get("token-url")
    if token_url is not None and not re.match(r"^https?://[^/\s]+(/[^\s]*)?$", token_url):
        raise UsageError(f"--token-url: {token_url!r} is not an http or https URL")

    request = {
        "operation": operation, "verb": verb, "object": obj, "url": url, "token_url": token_url,
        "timeout": number("timeout-ms", given.get("timeout-ms", "10000"), 1, 600000) / 1000,
        "filters": {}, "targets": None, "pagination": None, "id": None, "body": None,
    }
    if "id" in given:
        request["id"] = object_id("id", given["id"])
    if "body" in given:
        try:
            request["body"] = json.loads(given["body"])
        except ValueError as exc:
            raise UsageError(f"--body is not JSON: {exc}") from exc
        if not isinstance(request["body"], dict):
            raise UsageError("--body must be a JSON object")
    if verb == "list":
        for flag, argument in LIST_FILTERS[obj].items():
            if flag in given:
                request["filters"][argument] = object_id(flag, given[flag]) if flag.endswith("-id") else given[flag]
        if "targets" in given:
            request["targets"] = [t for t in given["targets"].split(",")]
            if not all(request["targets"]):
                raise UsageError("--targets: an empty target")
        if "skip" in given or "limit" in given:
            request["pagination"] = (number("skip", given.get("skip", "0"), 0, 2**31 - 1),
                                     number("limit", given.get("limit", "50"), 0, 2**31 - 1))

    # The client is one of the fixture: its credentials never appear on a
    # command line.
    try:
        fixture = load(given.get("fixture", f"{FIXTURES_DIR}/baseline/fixture.json"))
    except FixtureError as exc:
        raise UsageError(f"--fixture: {exc}") from exc
    who = fixture_client(fixture, given["as"])
    if who is None:
        raise UsageError(f"--as: the fixture has no client named {given['as']!r}")
    if operation not in CLIENT_OPERATIONS[who["role"]]:
        raise UsageError(f"{operation} is not available to a {who['role']} client of this library")
    request["client"] = who
    return request


def cmd_print_fixture(args: list[str]) -> int:
    if len(args) > 1:
        raise UsageError("print-fixture takes at most one name")
    name = args[0] if args else "baseline"
    if not _FIXTURE_NAME.match(name):
        print(f"openadr-interop: no fixture named {name!r}", file=sys.stderr)
        return 1
    try:
        with open(f"{FIXTURES_DIR}/{name}/fixture.json", "rb") as handle:
            sys.stdout.buffer.write(handle.read())
    except OSError:
        print(f"openadr-interop: no fixture named {name!r}", file=sys.stderr)
        return 1
    return 0


def main(argv: list[str]) -> int:
    try:
        if not argv:
            raise UsageError("a command is required")
        command, rest = argv[0], argv[1:]
        if command == "client":
            request = parse_client(rest)
            from .client import run  # pylint: disable=import-outside-toplevel

            document, code = run(request)
            print(json.dumps(document, separators=(",", ":")))
            return code
        if command == "print-capabilities":
            if rest:
                raise UsageError("print-capabilities takes no arguments")
            print(json.dumps(capabilities(), separators=(",", ":")))
            return 0
        if command == "print-fixture":
            return cmd_print_fixture(rest)
        if command == "version":
            if rest:
                raise UsageError("version takes no arguments")
            print(f"openadr-interop {ADAPTER} {build_info().get('adapterVersion', 'dev')} "
                  f"(ElaadNL openadr3-client {upstream_version()})")
            return 0
        if command == "server":
            raise UsageError("this adapter has no server role")
        raise UsageError(f"unknown command {command!r}")
    except UsageError as exc:
        print(f"openadr-interop: {exc}\n{USAGE}", file=sys.stderr)
        return 2
