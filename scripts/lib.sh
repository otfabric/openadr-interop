#!/usr/bin/env bash
# Shared helpers for the self-test scripts. Source, do not execute.
#
# Servers and clients run as containers on a private Docker network and reach
# each other by container name, so no host port is published and parallel runs
# do not collide.

REGISTRY="${REGISTRY:-ghcr.io/otfabric}"
VERSION="${VERSION:-dev}"
RUN_ID="${RUN_ID:-$$}"
NETWORK="openadr-interop-${RUN_ID}"
READY_FILE="/run/openadr-interop/ready"
PORT=8080
WAIT_TIMEOUT="${WAIT_TIMEOUT:-90}"

PASS=0
FAIL=0

log()  { echo "[$(basename "$0" .sh)] $*" >&2; }
ok()   { PASS=$((PASS + 1)); [[ -n "${VERBOSE:-}" ]] && log "ok: $*"; return 0; }
fail() { FAIL=$((FAIL + 1)); log "FAIL: $*"; return 0; }

image_of() { echo "${REGISTRY}/openadr-interop-$1:${VERSION}"; }

require_tools() {
    local t
    for t in docker jq; do
        command -v "$t" >/dev/null || { log "missing required tool: $t"; exit 2; }
    done
}

network_up() { docker network create "${NETWORK}" >/dev/null; }

cleanup() {
    docker ps -aq --filter "label=openadr-interop.run=${RUN_ID}" | xargs -r docker rm -f >/dev/null 2>&1
    docker network rm "${NETWORK}" >/dev/null 2>&1
    return 0
}

# server_name <adapter>: the container name start_server uses. SERVER_TAG
# distinguishes servers of one adapter that run at the same time.
server_name() { echo "srv-$1-${SERVER_TAG:-one}-${RUN_ID}"; }

# url_of <container>: the URL of the API of a server, as clients on the
# network reach it.
url_of() { echo "http://$1:${PORT}"; }

# start_server <adapter> [server args...]  -> prints the container name.
# The VTN tells clients where its token endpoint is, so it is told the URL
# under which they reach it.
start_server() {
    local adapter="$1"; shift
    local name
    name=$(server_name "${adapter}")
    docker rm -f "${name}" >/dev/null 2>&1
    docker run -d --name "${name}" --network "${NETWORK}" \
        --label "openadr-interop.run=${RUN_ID}" \
        "$(image_of "${adapter}")" server --public-url "$(url_of "${name}")" "$@" >/dev/null
    echo "${name}"
}

# wait_ready <container>: the ready file exists only once the fixture is
# loaded and the VTN answers. No fixed delays.
wait_ready() {
    local name="$1" waited=0 state
    while true; do
        state=$(docker inspect --format '{{.State.Status}}' "${name}" 2>/dev/null || echo absent)
        if [[ "${state}" != "running" ]]; then
            log "server ${name} is ${state}"; docker logs --tail 20 "${name}" >&2 2>/dev/null; return 1
        fi
        docker exec "${name}" test -f "${READY_FILE}" 2>/dev/null && return 0
        if (( waited >= WAIT_TIMEOUT * 5 )); then
            log "server ${name} not ready after ${WAIT_TIMEOUT}s"; docker logs --tail 20 "${name}" >&2; return 1
        fi
        sleep 0.2; waited=$((waited + 1))
    done
}

stop_server() { docker rm -f "$1" >/dev/null 2>&1; return 0; }

# run_client <adapter> <args...>: prints the result document and returns the
# exit code of the adapter. Capture both with
#     doc=$(run_client ...); CLIENT_EXIT=$?
# CLIENT_MOUNT, when set, is passed to docker as a volume (for --fixture).
run_client() {
    local adapter="$1"; shift
    docker run --rm --network "${NETWORK}" --label "openadr-interop.run=${RUN_ID}" \
        ${CLIENT_MOUNT:+-v "${CLIENT_MOUNT}"} \
        "$(image_of "${adapter}")" client "$@" 2>/dev/null
}

# One line that states the outcome of an operation; the expectation format of
# tests/cases.tsv. "n" is the number of objects the VTN returned: the length
# of a search result, 1 for a single object, 0 when there is none.
# shellcheck disable=SC2034
SUMMARY='"ok=\(.ok) error=\(.error.code // "-") status=\(.status // "-") n=\(if .response == null then 0 elif (.response | type) == "array" then (.response | length) else 1 end)"'

KNOWN=0

finish() {
    echo "" >&2
    local extra=""
    [[ "${KNOWN}" -gt 0 ]] && extra="${extra}, ${KNOWN} known upstream bugs"
    [[ "${SKIPPED:-0}" -gt 0 ]] && extra="${extra}, ${SKIPPED} skipped (capability not declared)"
    log "${PASS} passed, ${FAIL} failed${extra}"
    [[ "${FAIL}" -eq 0 ]]
}
