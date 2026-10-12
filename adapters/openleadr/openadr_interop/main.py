# SPDX-License-Identifier: MIT
"""Command line of the adapter: the container contract."""

from __future__ import annotations

import json
import os
import re
import socket
import sys

from . import ADAPTER, SCHEMA_VERSION
from .fixture import FixtureError, load

FIXTURES_DIR = "/fixtures"
BUILD_INFO = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "build-info.json")
_FIXTURE_NAME = re.compile(r"^[a-z0-9][a-z0-9-]*$")

USAGE = """usage:
  openadr-interop server [--fixture PATH] [--bind-address ADDR] [--port N] [--public-url URL] [--ready-file PATH]
  openadr-interop print-capabilities
  openadr-interop print-fixture [NAME]
  openadr-interop version
This adapter has no client role."""


class UsageError(Exception):
    """Exit 2: nothing was started and nothing was sent."""


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


def build_info() -> dict:
    try:
        with open(BUILD_INFO, encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return {}


def capabilities() -> dict:
    info = build_info()
    return {
        "schemaVersion": SCHEMA_VERSION,
        "adapter": ADAPTER,
        "adapterVersion": info.get("adapterVersion", "dev"),
        "protocol": "openadr3",
        "openadrVersions": ["3.1.0"],
        "upstream": {
            "name": "OpenLEADR openleadr-rs",
            "version": info.get("upstreamVersion", "unknown"),
            "revision": info.get("upstreamRevision", "unknown"),
            "license": "Apache-2.0 OR MIT",
            "language": "Rust",
            "independentEngine": True,
        },
        "roles": {"server": True, "client": False},
        "features": {
            # The VTN is built with its internal OAuth provider and issues
            # tokens at /auth/token.
            "serverTokenEndpoint": True,
            # ven objects are returned without objectType.
            "serverVenObjectType": False,
            # A bearer token the VTN did not issue is answered 403, not 401.
            "serverInvalidTokenIs401": False,
            # A VEN is shown the programs and events that carry a target it
            # was granted, whether or not its search names the target.
            "serverTargetsMustBeNamed": False,
            "clientTokenDiscovery": False,
            "clientReportDescriptorsInPrograms": False,
        },
        "serverObjects": ["programs", "events", "reports", "vens", "resources"],
        "clientOperations": {"bl": [], "ven": []},
    }


def cmd_server(args: list[str]) -> int:
    given = flags(args, {"fixture", "bind-address", "port", "public-url", "ready-file"})
    port_text = given.get("port", "8080")
    if not port_text.isdigit() or not 1 <= int(port_text) <= 65535:
        raise UsageError(f"--port: {port_text!r} is not a port number")
    port = int(port_text)
    # The OpenLEADR VTN always listens on every address.
    if given.get("bind-address", "0.0.0.0") != "0.0.0.0":
        raise UsageError("--bind-address: this adapter listens on 0.0.0.0 only")
    public_url = given.get("public-url", f"http://{socket.gethostname()}:{port}").rstrip("/")
    if not re.match(r"^https?://[^/\s]+(/[^\s]*)?$", public_url):
        raise UsageError(f"--public-url: {public_url!r} is not an http or https URL")
    path = given.get("fixture", f"{FIXTURES_DIR}/baseline/fixture.json")
    try:
        fixture = load(path)
    except FixtureError as exc:
        print(f"openadr-interop: invalid fixture: {exc}", file=sys.stderr)
        return 4

    from .server import serve  # pylint: disable=import-outside-toplevel

    return serve(fixture, port, public_url, given.get("ready-file", "/run/openadr-interop/ready"))


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
        if command == "server":
            return cmd_server(rest)
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
            info = build_info()
            print(f"openadr-interop {ADAPTER} {info.get('adapterVersion', 'dev')} "
                  f"(OpenLEADR openleadr-rs {info.get('upstreamVersion', 'unknown')})")
            return 0
        if command == "client":
            raise UsageError("this adapter has no client role")
        raise UsageError(f"unknown command {command!r}")
    except UsageError as exc:
        print(f"openadr-interop: {exc}\n{USAGE}", file=sys.stderr)
        return 2
