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
import logging
import os
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
}


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
    return value.model_dump(by_alias=True, mode="json", exclude_none=True)


def call(request: dict) -> object:
    """Build the library client for the role and perform the operation."""
    from openadr3_client.bl.http_factory import BusinessLogicHttpClientFactory  # pylint: disable=import-outside-toplevel
    from openadr3_client.oadr310._vtn.interfaces.filters import PaginationFilter, TargetFilter  # pylint: disable=import-outside-toplevel
    from openadr3_client.ven.http_factory import VirtualEndNodeHttpClientFactory  # pylint: disable=import-outside-toplevel
    from openadr3_client.version import OADRVersion  # pylint: disable=import-outside-toplevel

    who, obj, verb = request["client"], request["object"], request["verb"]
    if request["url"].startswith("http://") or (request["token_url"] or "").startswith("http://"):
        # oauthlib, too, refuses a token endpoint without TLS unless told otherwise.
        os.environ["OAUTHLIB_INSECURE_TRANSPORT"] = "1"
    settings = {
        "vtn_base_url": request["url"],
        "client_id": who["clientId"],
        "client_secret": who["clientSecret"],
        "token_url": request["token_url"],
        # The library refuses plain HTTP unless told otherwise; reference
        # servers on a test network have no certificate.
        "allow_insecure_http": request["url"].startswith("http://"),
        "version": OADRVersion.OADR_310,
    }
    if who["role"] == "bl":
        library = BusinessLogicHttpClientFactory.create_http_bl_client(**settings)
    else:
        library = VirtualEndNodeHttpClientFactory.create_http_ven_client(**settings)
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


def _list_arguments(obj: str) -> tuple[str, ...]:
    """The arguments of the library's search for an object type."""
    return {
        "program": ("target", "pagination"),
        "event": ("target", "pagination", "program_id"),
        "report": ("pagination", "program_id", "event_id", "client_name"),
        "ven": ("ven_name", "target", "pagination"),
        "resource": ("ven_id", "resource_name", "target", "pagination"),
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
