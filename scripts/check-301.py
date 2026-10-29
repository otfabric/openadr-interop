#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""What an OpenADR 3.0.1 server of the baseline-301 fixture must do.

Usage: check-301.py <base URL>     run inside the server's container (smoke.sh)

A token from /auth/token for each client; programs whose targets are
valuesMap objects; the program targeted at VEN_NAME site-1 seen by the
client of site-1 and not by the client of site-2. One line per check that
passed; on a failure, the reason on the last line and exit 1.
"""

import json
import sys
import urllib.error
import urllib.parse
import urllib.request

FIXTURE = "/fixtures/baseline-301/fixture.json"


def call(url, token=None, form=None):
    headers = {"Accept": "application/json"}
    data = None
    if form is not None:
        data = urllib.parse.urlencode(form).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    with urllib.request.urlopen(urllib.request.Request(url, data=data, headers=headers), timeout=10) as response:
        return json.load(response)


def main():
    base = sys.argv[1].rstrip("/")
    with open(FIXTURE, encoding="utf-8") as handle:
        fixture = json.load(handle)
    clients = {c["name"]: c for c in fixture["clients"]}
    tokens = {}
    for name in ("bl", "ven-1", "ven-2"):
        answer = call(f"{base}/auth/token", form={"grant_type": "client_credentials",
                                                  "client_id": clients[name]["clientId"],
                                                  "client_secret": clients[name]["clientSecret"]})
        if not isinstance(answer.get("access_token"), str) or answer.get("token_type", "").lower() != "bearer":
            raise SystemExit(f"POST /auth/token for {name}: {answer}")
        tokens[name] = answer["access_token"]
    print("3.0.1: a bearer token from /auth/token for bl, ven-1 and ven-2")

    programs = {p["programName"]: p for p in call(f"{base}/programs", tokens["bl"])}
    if sorted(programs) != ["day-ahead", "group-a-flex"]:
        raise SystemExit(f"GET /programs as bl: {sorted(programs)}")
    want = [{"type": "VEN_NAME", "values": ["site-1"]}]
    if programs["group-a-flex"].get("targets") != want:
        raise SystemExit(f"group-a-flex targets as bl: {programs['group-a-flex'].get('targets')}, want {want}")
    print("3.0.1: GET /programs as bl, with valuesMap targets")

    seen = {name: {p["programName"] for p in call(f"{base}/programs", tokens[name])} for name in ("ven-1", "ven-2")}
    if "group-a-flex" not in seen["ven-1"] or "day-ahead" not in seen["ven-1"]:
        raise SystemExit(f"GET /programs as ven-1: {sorted(seen['ven-1'])}")
    if "group-a-flex" in seen["ven-2"] or "day-ahead" not in seen["ven-2"]:
        raise SystemExit(f"GET /programs as ven-2: {sorted(seen['ven-2'])}")
    print("3.0.1: the program targeted at site-1 is seen by ven-1 and not by ven-2")


if __name__ == "__main__":
    try:
        main()
    except (urllib.error.URLError, OSError, ValueError, KeyError) as exc:
        raise SystemExit(f"{type(exc).__name__}: {exc}") from exc
