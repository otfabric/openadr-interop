# SPDX-License-Identifier: MIT
"""The reference VTN of OpenADR 3.0.1: OpenLEADR v0.1.4 with a private PostgreSQL.

OpenLEADR keeps its state in PostgreSQL and takes its OAuth clients, and
what each of them may do, from the database. To be one self-contained image
like the other reference servers, this module runs a throw-away PostgreSQL
next to the VTN, registers the clients of the fixture, and creates the
fixture's objects through the VTN's own REST API, as its Business Logic
client. Nothing survives the container.

v0.1.4 listens on port 3000 whatever it is told, so the adapter forwards
the port of the contract to it. It has no MQTT and no Multicast DNS.
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
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

from . import ADAPTER

VTN_BINARY = "/usr/local/bin/openleadr-vtn"
START_TIMEOUT = 60.0


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


class Forwarder:
    """Forwards TCP connections from the port of the contract to the VTN's."""

    def __init__(self, port: int, target: int) -> None:
        self.target = target
        # Bound at once, so that a busy port fails before anything starts;
        # connections wait in the backlog until start.
        self.listener = socket.create_server(("0.0.0.0", port))
        self.thread = threading.Thread(target=self.accept, daemon=True)

    def start(self) -> None:
        self.thread.start()

    def accept(self) -> None:
        while True:
            try:
                client, _ = self.listener.accept()
            except OSError:
                return
            threading.Thread(target=self.connect, args=(client,), daemon=True).start()

    def connect(self, client: socket.socket) -> None:
        try:
            upstream = socket.create_connection(("127.0.0.1", self.target), timeout=10)
        except OSError:
            client.close()
            return
        upstream.settimeout(None)
        back = threading.Thread(target=self.pump, args=(upstream, client), daemon=True)
        back.start()
        self.pump(client, upstream)
        back.join()
        client.close()
        upstream.close()

    @staticmethod
    def pump(source: socket.socket, sink: socket.socket) -> None:
        try:
            while data := source.recv(65536):
                sink.sendall(data)
        except OSError:
            pass
        # Half-close, so the other direction can still finish.
        try:
            sink.shutdown(socket.SHUT_WR)
        except OSError:
            pass

    def stop(self) -> None:
        self.listener.close()


class Vtn:
    """PostgreSQL and the VTN, started together and stopped together."""

    def __init__(self, port: int, vtn_port: int, public_url: str) -> None:
        self.port = port
        self.public_url = public_url
        self.local = f"http://127.0.0.1:{vtn_port}"
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
            "DATABASE_URL": f"postgres://openadr@127.0.0.1:{self.pg_port}/openadr",
            "OAUTH_TYPE": "INTERNAL",
            "OAUTH_KEY_TYPE": "HMAC",
            "OAUTH_BASE64_SECRET": base64.b64encode(secrets.token_bytes(48)).decode(),
            "RUST_LOG": os.environ.get("RUST_LOG", "warn"),
        }
        self.vtn = subprocess.Popen([VTN_BINARY], env=env, stdout=sys.stderr, stderr=sys.stderr)  # pylint: disable=consider-using-with
        # The VTN applies its database migrations before it serves /health.
        deadline = time.monotonic() + START_TIMEOUT
        while True:
            if self.vtn.poll() is not None:
                raise StartError(f"the VTN exited with {self.vtn.returncode}")
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
        """Register the clients, then create the objects as the Business Logic client.

        v0.1.4 has no scopes: what a user may do are its rows in the role
        tables. The Business Logic client may write programs, events and
        VENs of any business; a VEN client is the VEN it is linked to in
        user_ven, which is written once the VEN exists. The roles go into a
        token when it is issued, so all of this is done before ready.
        """
        # Client IDs and secrets match [a-zA-Z0-9_-]+ (fixture.py), and an
        # encoded Argon2 hash has no quote: none of this needs escaping.
        for client in fixture["clients"]:
            cid = client["clientId"]
            statement = (
                'INSERT INTO "user" (id, reference, description, created, modified) '
                f"VALUES ('{cid}', '{cid}', 'openadr-interop {client['role']} client {client['name']}', now(), now()); "
                "INSERT INTO user_credentials (user_id, client_id, client_secret) "
                f"VALUES ('{cid}', '{cid}', '{argon2id(client['clientSecret'])}');")
            if client["role"] == "bl":
                statement += (f"INSERT INTO any_business_user (user_id) VALUES ('{cid}'); "
                              f"INSERT INTO ven_manager (user_id) VALUES ('{cid}');")
            self.sql(statement)

        clients = {c["name"]: c for c in fixture["clients"]}
        bl = next(c for c in fixture["clients"] if c["role"] == "bl")
        token = self.call("POST", "/auth/token", None, form={
            "grant_type": "client_credentials", "client_id": bl["clientId"], "client_secret": bl["clientSecret"]})["access_token"]

        ven_ids = {}
        for ven in fixture["vens"]:
            body = {"objectType": "VEN", "venName": ven["venName"]}
            if ven.get("targets"):
                body["targets"] = ven["targets"]
            ven_id = self.call("POST", "/vens", token, body)["id"]
            ven_ids[ven["venName"]] = ven_id
            # VEN IDs are the VTN's, and client IDs are names: no escaping.
            self.sql(f"INSERT INTO user_ven (ven_id, user_id) VALUES ('{ven_id}', '{clients[ven['client']]['clientId']}');")
        for resource in fixture["resources"]:
            body = {"objectType": "RESOURCE", "resourceName": resource["resourceName"]}
            if resource.get("targets"):
                body["targets"] = resource["targets"]
            self.call("POST", f"/vens/{ven_ids[resource['ven']]}/resources", token, body)
        program_ids = {}
        for program in fixture["programs"]:
            program_ids[program["programName"]] = self.call("POST", "/programs", token, {"objectType": "PROGRAM", **program})["id"]
        for event in fixture["events"]:
            body = {k: v for k, v in event.items() if k != "program"}
            body["objectType"] = "EVENT"
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


def serve(fixture: dict, port: int, vtn_port: int, public_url: str, ready_file: str) -> int:
    stopping = False

    def on_signal(_signum, _frame) -> None:
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)

    vtn = Vtn(port, vtn_port, public_url)
    forwarder = None
    try:
        forwarder = Forwarder(port, vtn_port)
        # The VTN's port must be free too: another VTN there would answer
        # for this one, and be loaded with its fixture.
        with socket.socket() as probe:
            try:
                probe.bind(("0.0.0.0", vtn_port))
            except OSError as exc:
                raise StartError(f"port {vtn_port}, which the VTN listens on, is in use") from exc
        vtn.start_postgres()
        vtn.start_vtn()
        vtn.load(fixture)
        # Clients reach the VTN only once it has the fixture.
        forwarder.start()
        os.makedirs(os.path.dirname(ready_file), exist_ok=True)
        with open(ready_file, "w", encoding="utf-8") as handle:
            handle.write("ready\n")
    except (StartError, OSError) as exc:
        log(str(exc))
        if forwarder:
            forwarder.stop()
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
    forwarder.stop()
    vtn.stop()
    if code == 0:
        emit({"event": "stopped"})
    return code
