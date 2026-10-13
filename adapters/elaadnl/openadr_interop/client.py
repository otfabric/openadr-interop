# SPDX-License-Identifier: MIT
"""One client operation with the ElaadNL openadr3-client.

The adapter adds nothing to the protocol: it hands the request to the
library's Business Logic or VEN client, and reports what the library
returned or raised. A request body is given in the JSON form of the
specification and goes through the library's own models, so a VTN is sent
exactly what an application using the library would send.
"""

from __future__ import annotations

import importlib
import json
import logging
import os
import sys
import threading
import time
from urllib.parse import urlsplit

import pydantic
import requests

from . import ADAPTER, SCHEMA_VERSION

MODELS = "openadr3_client.oadr310.models"

# Request models: object -> (module, create model, update model), per role
# where the specification has separate Business Logic and VEN requests.
REQUEST_MODELS = {
    "program": {"*": ("program.program", "NewProgram", "ProgramUpdate")},
    "event": {"*": ("event.event", "NewEvent", "EventUpdate")},
    "report": {"*": ("report.report", "NewReport", "ReportUpdate")},
    "ven": {"bl": ("ven.ven", "NewVenBlRequest", "VenUpdateBlRequest"),
            "ven": ("ven.ven", "NewVenVenRequest", "VenUpdateVenRequest")},
    "resource": {"bl": ("resource.resource", "NewResourceBlRequest", "ResourceUpdateBlRequest"),
                 "ven": ("resource.resource", "NewResourceVenRequest", "ResourceUpdateVenRequest")},
    "subscription": {"*": ("subscriptions.subscription", "NewSubscription", "SubscriptionUpdate")},
}

# Resource groups are an extension with a client and models of their own.
RESOURCE_GROUPS = "openadr3_client.extensions.resource_group"


class ListenError(Exception):
    """listen-mqtt did not get what it was asked to wait for."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class Recorder:
    """Sees every HTTP exchange the library makes, and bounds each wait."""

    def __init__(self, timeout: float) -> None:
        self.timeout = timeout
        # Answers to requests of the operation itself, as opposed to those
        # of getting a token.
        self.api_responses = 0
        self.status: int | None = None
        self.token_issued = False
        self._original = requests.sessions.Session.request

    def install(self) -> None:
        recorder = self

        def request(session, method, url, *args, **kwargs):  # noqa: ANN001, ANN202
            kwargs.setdefault("timeout", recorder.timeout)
            # Token requests and token endpoint discovery are not the operation.
            auth_phase = "/auth/" in urlsplit(str(url)).path or method.upper() == "POST" and "token" in urlsplit(str(url)).path
            response = recorder._original(session, method, url, *args, **kwargs)
            if auth_phase:
                if method.upper() == "POST" and response.ok:
                    recorder.token_issued = True
            else:
                # The library asks for a token while it prepares this request,
                # so a request only counts once it has been answered.
                recorder.api_responses += 1
                recorder.status = response.status_code
            return response

        requests.sessions.Session.request = request


def dump(value: object) -> object:
    """What the library returned, in the JSON form of the specification."""
    if isinstance(value, (tuple, list)):
        return [dump(item) for item in value]
    if isinstance(value, dict):
        return value
    return value.model_dump(by_alias=True, mode="json", exclude_none=True)


def call(request: dict) -> object:
    """Build the library client for the role and perform the operation."""
    from openadr3_client.bl.http_factory import BusinessLogicHttpClientFactory  # pylint: disable=import-outside-toplevel
    from openadr3_client.oadr310._vtn.interfaces.filters import PaginationFilter, TargetFilter  # pylint: disable=import-outside-toplevel
    from openadr3_client.ven.http_factory import VirtualEndNodeHttpClientFactory  # pylint: disable=import-outside-toplevel
    from openadr3_client.version import OADRVersion  # pylint: disable=import-outside-toplevel

    who, obj, verb = request["client"], request["object"], request["verb"]
    insecure = request["url"].startswith("http://")
    if insecure or (request["token_url"] or "").startswith("http://"):
        # oauthlib, too, refuses a token endpoint without TLS unless told otherwise.
        os.environ["OAUTHLIB_INSECURE_TRANSPORT"] = "1"
    # The library refuses plain HTTP unless told otherwise; reference
    # servers on a test network have no certificate.
    settings = {"vtn_base_url": request["url"], "allow_insecure_http": insecure, "version": OADRVersion.OADR_310}
    if who["clientId"] is None:
        if who["role"] == "bl":
            library = BusinessLogicHttpClientFactory.create_anonymous_http_bl_client(**settings)
        else:
            library = VirtualEndNodeHttpClientFactory.create_anonymous_http_ven_client(**settings)
    else:
        settings |= {"client_id": who["clientId"], "client_secret": who["clientSecret"], "token_url": request["token_url"]}
        if who["role"] == "bl":
            library = BusinessLogicHttpClientFactory.create_http_bl_client(**settings)
        else:
            library = VirtualEndNodeHttpClientFactory.create_http_ven_client(**settings)

    if request["operation"] == "get-notifiers":
        return library.notifiers.get_notifiers()
    if request["operation"] == "get-topics":
        return _topics(library, request)
    if request["operation"] == "listen-mqtt":
        return _listen(library, request)

    if obj == "resource-group":
        return _resource_group(library, request)
    interface = getattr(library, obj + "s")

    if verb == "list":
        arguments = dict.fromkeys(_list_arguments(obj))
        arguments.update(request["filters"])
        if request["targets"] is not None:
            arguments["target"] = TargetFilter(targets=request["targets"])
        if request["pagination"] is not None:
            arguments["pagination"] = PaginationFilter(skip=request["pagination"][0], limit=request["pagination"][1])
        return getattr(interface, f"get_{obj}s")(**arguments)
    if verb == "get":
        return getattr(interface, f"get_{obj}_by_id")(request["id"])
    if verb == "delete":
        return getattr(interface, f"delete_{obj}_by_id")(request["id"])

    models = REQUEST_MODELS[obj]
    module, new, update = models.get("*") or models[who["role"]]
    model = getattr(importlib.import_module(f"{MODELS}.{module}"), new if verb == "create" else update)
    body = model.model_validate(request["body"])
    if verb == "create":
        return getattr(interface, f"create_{obj}")(body)
    return getattr(interface, f"update_{obj}_by_id")(request["id"], body)


def _resource_group(library: object, request: dict) -> object:
    """One operation with the library's resource group client."""
    factory = importlib.import_module(f"{RESOURCE_GROUPS}.factory").ResourceGroupClientFactory
    filters = importlib.import_module(f"{RESOURCE_GROUPS}._client.filters")
    models = importlib.import_module(f"{RESOURCE_GROUPS}.models.resource_group")

    who, verb = request["client"], request["verb"]
    # This factory wants the token endpoint: where none is given, the VTN is
    # asked as the library's other clients ask it.
    token_url = request["token_url"] or library.auth.get_auth_server().token_url
    create = factory.create_bl_client if who["role"] == "bl" else factory.create_ven_client
    interface = create(
        vtn_base_url=request["url"], client_id=who["clientId"], client_secret=who["clientSecret"],
        token_url=token_url, allow_insecure_http=request["url"].startswith("http://"),
    )
    if verb == "list":
        arguments = dict(request["filters"])
        if request["targets"] is not None:
            arguments["target"] = filters.TargetFilter(targets=request["targets"])
        if request["pagination"] is not None:
            arguments["pagination"] = filters.PaginationFilter(skip=request["pagination"][0], limit=request["pagination"][1])
        return interface.get_resource_groups(**arguments)
    if verb == "get":
        return interface.get_resource_group_by_id(request["id"])
    if verb == "delete":
        return interface.delete_resource_group_by_id(request["id"])
    if verb == "create":
        return interface.create_resource_group(models.NewResourceGroup.model_validate(request["body"]))
    return interface.update_resource_group_by_id(request["id"], models.ResourceGroupUpdate.model_validate(request["body"]))


def _topics(library: object, request: dict) -> object:
    """The MQTT topics the VTN names for a scope."""
    from .main import TOPIC_SCOPES  # pylint: disable=import-outside-toplevel

    method = getattr(library.notifiers, TOPIC_SCOPES[request["scope"]][0])
    return method() if request["id"] is None else method(request["id"])


def _listen(library: object, request: dict) -> dict:
    """Wait for notifications on the VTN's MQTT broker, with the library's MQTT client.

    The broker and the topic are the ones the VTN names: nothing about them
    is given on the command line.
    """
    from openadr3_client.oadr310._vtn.mqtt.client import MQTTClient  # pylint: disable=import-outside-toplevel

    binding = library.notifiers.get_notifiers().mqtt
    topic = getattr(_topics(library, request).topics, request["topic_operation"])
    broker = urlsplit(str(binding.uris[0]))
    secure = broker.scheme in ("mqtts", "ssl", "tls")
    if broker.scheme not in ("mqtt", "tcp") and not secure:
        raise ListenError("failed", f"the VTN names a broker this adapter cannot reach: {binding.uris[0]}")

    client = MQTTClient(binding)
    if secure:
        client.tls_set()
    messages: list[dict] = []
    enough, subscribed = threading.Event(), threading.Event()
    refused: list[str] = []

    def on_connect(mqtt, _userdata, _flags, reason, _properties):  # noqa: ANN001, ANN202
        if reason.is_failure:
            refused.append(str(reason))
            enough.set()
            return
        mqtt.subscribe(topic, qos=1)

    def on_subscribe(_mqtt, _userdata, _mid, reasons, _properties):  # noqa: ANN001, ANN202
        if any(reason.is_failure for reason in reasons):
            refused.append("subscription refused: " + ", ".join(str(reason) for reason in reasons))
            enough.set()
            return
        if not subscribed.is_set():
            subscribed.set()
            print(f"openadr-interop: listening on {topic}", file=sys.stderr, flush=True)

    def on_message(_mqtt, _userdata, message):  # noqa: ANN001, ANN202
        try:
            payload = json.loads(message.payload)
        except ValueError:
            payload = message.payload.decode("utf-8", "replace")
        messages.append({"topic": message.topic, "payload": payload})
        if len(messages) >= request["messages"]:
            enough.set()

    client.on_connect, client.on_subscribe, client.on_message = on_connect, on_subscribe, on_message
    try:
        client.connect(broker.hostname, broker.port or (8883 if secure else 1883))
    except OSError as exc:
        raise ListenError("connect-failed", f"MQTT broker {binding.uris[0]}: {exc}") from exc
    client.loop_start()
    try:
        enough.wait(request["listen"])
    finally:
        client.disconnect()
        client.loop_stop()
    if refused:
        raise ListenError("failed", f"MQTT broker {binding.uris[0]}: {refused[0]}")
    if len(messages) < request["messages"]:
        raise ListenError("timeout", f"{len(messages)} of {request['messages']} notifications on {topic}")
    return {"broker": str(binding.uris[0]), "topic": topic, "messages": messages[:request["messages"]]}


def _list_arguments(obj: str) -> tuple[str, ...]:
    """The arguments of the library's search for an object type."""
    return {
        "program": ("target", "pagination"),
        "event": ("target", "pagination", "program_id"),
        "report": ("pagination", "program_id", "event_id", "client_name"),
        "ven": ("ven_name", "target", "pagination"),
        "resource": ("ven_id", "resource_name", "target", "pagination"),
        "subscription": ("pagination", "program_id", "client_name", "objects"),
    }[obj]


def run(request: dict) -> tuple[dict, int]:
    """Perform the operation; returns the result document and the exit code."""
    logging.getLogger().setLevel(logging.ERROR)
    recorder = Recorder(request["timeout"])
    recorder.install()

    document = {
        "schemaVersion": SCHEMA_VERSION,
        "adapter": ADAPTER,
        "operation": request["operation"],
        "client": request["client"]["name"],
        "role": request["client"]["role"],
        "ok": False,
        "error": None,
        "authenticated": False,
        "status": None,
        "response": None,
        "problem": None,
        "elapsedMs": 0,
    }
    code = 1
    started = time.monotonic()

    def fail(error: str, message: object) -> None:
        document["error"] = {"code": error, "message": str(message)[:500]}

    try:
        document["response"] = dump(call(request))
        document["ok"], code = True, 0
    except ListenError as exc:
        fail(exc.code, exc)
        code = 3 if exc.code == "connect-failed" else 1
    except requests.exceptions.HTTPError as exc:
        response = exc.response
        status = response.status_code if response is not None else None
        if recorder.api_responses == 0:
            # The token endpoint, or its discovery, refused.
            fail("auth-failed", f"no token: HTTP {status}")
        else:
            fail("http-error", f"HTTP {status}")
            try:
                body = response.json()
                document["problem"] = body if isinstance(body, dict) else None
            except ValueError:
                pass
    except requests.exceptions.Timeout as exc:
        if isinstance(exc, requests.exceptions.ConnectTimeout):
            fail("connect-failed", exc)
            code = 3
        else:
            fail("timeout", exc)
    except requests.exceptions.ConnectionError as exc:
        fail("connect-failed", exc)
        code = 3
    except pydantic.ValidationError as exc:
        if recorder.api_responses == 0:
            # The library's own model refused the request body: nothing was sent.
            fail("invalid-request", exc)
        else:
            fail("invalid-response", exc)
    except Exception as exc:  # pylint: disable=broad-exception-caught
        # Whatever else the library raises. Before the first API request it
        # can only be about getting a token.
        fail("auth-failed" if recorder.api_responses == 0 else "failed", f"{type(exc).__name__}: {exc}")

    document["authenticated"] = recorder.token_issued
    document["status"] = recorder.status
    document["elapsedMs"] = round((time.monotonic() - started) * 1000)
    return document, code
