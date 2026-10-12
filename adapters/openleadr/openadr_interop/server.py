# SPDX-License-Identifier: MIT
"""The reference VTN: OpenLEADR with a private PostgreSQL, serving a fixture.

OpenLEADR keeps its state in PostgreSQL and takes its OAuth clients from the
database. To be one self-contained image like the other reference servers,
this module runs a throw-away PostgreSQL next to the VTN, registers the
clients of the fixture, and creates the fixture's objects through the VTN's
own REST API, as its Business Logic client. Nothing survives the container.
"""

from __future__ import annotations

import base64
import json
import os
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

from . import ADAPTER

VTN_BINARY = "/usr/local/bin/openleadr-vtn"
START_TIMEOUT = 60.0

# The scopes OpenLEADR's own test fixture gives the two roles. OpenLEADR
# splits some scopes of the specification by role.
SCOPES = {
    "bl": ["read_all", "write_programs", "write_events", "write_vens_bl", "write_subscriptions_bl"],
    "ven": ["read_targets", "read_ven_objects", "write_reports_ven", "write_vens_ven", "write_subscriptions_ven"],
}


class StartError(Exception):
    """The VTN could not be brought up with the fixture."""


def emit(event: dict) -> None:
    print(json.dumps(event, separators=(",", ":")), flush=True)


def log(message: str) -> None:
    print(f"openadr-interop: {message}", file=sys.stderr, flush=True)


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run(command: list[str], **kwargs) -> str:
    """Run a helper to completion; its output is diagnostics, not contract."""
    done = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=False, **kwargs)
    if done.returncode != 0:
        raise StartError(f"{command[0]} failed ({done.returncode}): {done.stderr.strip() or done.stdout.strip()}")
    return done.stdout


def argon2id(secret: str) -> str:
    """The encoded Argon2id hash OpenLEADR stores for a client secret."""
    salt = secrets.token_hex(8)
    return run(["argon2", salt, "-id", "-e", "-t", "2", "-k", "64", "-p", "1"], input=secret).strip()


class Vtn:
    """PostgreSQL and the VTN, started together and stopped together."""

    def __init__(self, port: int, public_url: str) -> None:
        self.port = port
        self.public_url = public_url
        self.local = f"http://127.0.0.1:{port}"
        self.workdir = tempfile.mkdtemp(prefix="openadr-interop-")
        self.pg_port = free_port()
        self.postgres: subprocess.Popen | None = None
        self.vtn: subprocess.Popen | None = None

    # -- PostgreSQL

    def start_postgres(self) -> None:
        data = os.path.join(self.workdir, "pg")
        run(["initdb", "-D", data, "-U", "openadr", "--auth=trust", "-E", "UTF8", "--no-sync"])
        self.postgres = subprocess.Popen(  # pylint: disable=consider-using-with
            ["postgres", "-D", data, "-p", str(self.pg_port),
             "-c", "listen_addresses=127.0.0.1", "-c", f"unix_socket_directories={self.workdir}",
             # The database lives as long as the container: durability buys nothing.
             "-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off"],
            stdout=sys.stderr, stderr=sys.stderr)
        deadline = time.monotonic() + START_TIMEOUT
        while subprocess.run(["pg_isready", "-q", "-h", "127.0.0.1", "-p", str(self.pg_port), "-U", "openadr", "-d", "postgres"], check=False).returncode != 0:
            if self.postgres.poll() is not None:
                raise StartError(f"postgres exited with {self.postgres.returncode}")
            if time.monotonic() > deadline:
                raise StartError("postgres did not accept connections in time")
            time.sleep(0.1)
        run(["createdb", "-h", "127.0.0.1", "-p", str(self.pg_port), "-U", "openadr", "openadr"])

    def sql(self, statement: str) -> None:
        run(["psql", "-q", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-p", str(self.pg_port), "-U", "openadr",
             "-d", "openadr", "-c", statement])

    # -- the VTN

    def start_vtn(self) -> None:
        env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "PORT": str(self.port),
            "DATABASE_URL": f"postgres://openadr@127.0.0.1:{self.pg_port}/openadr",
            # What GET /auth/server tells clients.
            "OAUTH_TOKEN_URL": f"{self.public_url}/auth/token",
            "OAUTH_BASE64_SECRET": base64.b64encode(secrets.token_bytes(48)).decode(),
            "RUST_LOG": os.environ.get("RUST_LOG", "warn"),
        }
        self.vtn = subprocess.Popen([VTN_BINARY], env=env, stdout=sys.stderr, stderr=sys.stderr)  # pylint: disable=consider-using-with
        # The VTN applies its database migrations before it serves /health.
        deadline = time.monotonic() + START_TIMEOUT
        while True:
            if self.vtn.poll() is not None:
                raise StartError(f"the VTN exited with {self.vtn.returncode}; is port {self.port} in use?")
            try:
                with urllib.request.urlopen(f"{self.local}/health", timeout=2):
                    return
            except (urllib.error.URLError, OSError):
                pass
            if time.monotonic() > deadline:
                raise StartError("the VTN did not become healthy in time")
            time.sleep(0.1)

    def call(self, method: str, path: str, token: str | None, body: object = None, form: dict | None = None) -> object:
        data, headers = None, {"Accept": "application/json"}
        if form is not None:
            data, headers["Content-Type"] = urllib.parse.urlencode(form).encode(), "application/x-www-form-urlencoded"
        elif body is not None:
            data, headers["Content-Type"] = json.dumps(body).encode(), "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        request = urllib.request.Request(f"{self.local}{path}", data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", "replace")[:500]
            raise StartError(f"{method} {path} answered {exc.code}: {detail}") from exc
        except (urllib.error.URLError, OSError, ValueError) as exc:
            raise StartError(f"{method} {path} failed: {exc}") from exc

    # -- the fixture

    def load(self, fixture: dict) -> None:
        # Client IDs and secrets match [a-zA-Z0-9_-]+ (fixture.py), and an
        # encoded Argon2 hash has no quote: none of this needs escaping.
        for client in fixture["clients"]:
            scopes = "{" + ",".join(f'"{s}"' for s in SCOPES[client["role"]]) + "}"
            cid = client["clientId"]
            self.sql(
                'INSERT INTO "user" (id, reference, description, scopes, created, modified) '
                f"VALUES ('{cid}', '{cid}', 'openadr-interop {client['role']} client {client['name']}', '{scopes}', now(), now()); "
                "INSERT INTO user_credentials (user_id, client_id, client_secret) "
                f"VALUES ('{cid}', '{cid}', '{argon2id(client['clientSecret'])}');")

        clients = {c["name"]: c for c in fixture["clients"]}
        bl = next(c for c in fixture["clients"] if c["role"] == "bl")
        token = self.call("POST", "/auth/token", None, form={
            "grant_type": "client_credentials", "client_id": bl["clientId"], "client_secret": bl["clientSecret"]})["access_token"]

        ven_ids, ven_clients = {}, {}
        for ven in fixture["vens"]:
            client_id = clients[ven["client"]]["clientId"]
            created = self.call("POST", "/vens", token, {
                "objectType": "BL_VEN_REQUEST", "venName": ven["venName"], "clientID": client_id,
                "targets": ven.get("targets")})
            ven_ids[ven["venName"]], ven_clients[ven["venName"]] = created["id"], client_id
        for resource in fixture["resources"]:
            self.call("POST", "/resources", token, {
                "objectType": "BL_RESOURCE_REQUEST", "resourceName": resource["resourceName"],
                "venID": ven_ids[resource["ven"]], "clientID": ven_clients[resource["ven"]],
                "targets": resource.get("targets")})
        program_ids = {}
        for program in fixture["programs"]:
            program_ids[program["programName"]] = self.call("POST", "/programs", token, program)["id"]
        for event in fixture["events"]:
            body = {k: v for k, v in event.items() if k != "program"}
            body["programID"] = program_ids[event["program"]]
            self.call("POST", "/events", token, body)

    # -- shutdown

    def stop(self) -> None:
        for process, stop_signal in ((self.vtn, signal.SIGTERM), (self.postgres, signal.SIGINT)):
            if process is None or process.poll() is not None:
                continue
            # SIGINT is PostgreSQL's fast shutdown.
            process.send_signal(stop_signal)
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        shutil.rmtree(self.workdir, ignore_errors=True)


def serve(fixture: dict, port: int, public_url: str, ready_file: str) -> int:
    stopping = False

    def on_signal(_signum, _frame) -> None:
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    vtn = Vtn(port, public_url)
    try:
        vtn.start_postgres()
        vtn.start_vtn()
        vtn.load(fixture)
        os.makedirs(os.path.dirname(ready_file), exist_ok=True)
        with open(ready_file, "w", encoding="utf-8") as handle:
            handle.write("ready\n")
    except (StartError, OSError) as exc:
        log(str(exc))
        vtn.stop()
        return 1

    emit({"event": "ready", "adapter": ADAPTER, "address": f"0.0.0.0:{port}", "url": public_url,
          "tokenUrl": f"{public_url}/auth/token", "fixture": fixture["name"]})
    code = 0
    while not stopping:
        if vtn.vtn.poll() is not None or vtn.postgres.poll() is not None:
            log("the VTN or its database stopped unexpectedly")
            code = 1
            break
        time.sleep(0.2)
    try:
        os.remove(ready_file)
    except OSError:
        pass
    vtn.stop()
    if code == 0:
        emit({"event": "stopped"})
    return code
