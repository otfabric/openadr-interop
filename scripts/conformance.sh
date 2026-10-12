#!/usr/bin/env bash
# Runs the conformance suite (conformance/) against one reference server.
#
# Usage: conformance.sh <adapter>          e.g. conformance.sh openleadr
#
# Starts the server image with the baseline fixture on a loopback port of
# this machine and runs the scenarios as the fixture's clients bl, ven-3 and
# ven-2. ven-3 is the VEN the scenarios register and grant targets to: the
# fixture gives it no ven object, and a VTN may allow one per client. conformance/known/<adapter>.tsv lists the scenarios that are known
# to fail against the adapter's upstream version; the run fails on any other
# failure, and on a listed scenario that passes.
#
# Needs Docker, jq and Go. Environment: REGISTRY, VERSION (image tag, default
# dev), REPORT_DIR (default reports), CONFORMANCE_PORT (default 18080).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"

ADAPTER="${1:?usage: conformance.sh <adapter>}"
IMAGE="$(image_of "${ADAPTER}")"
HOST_PORT="${CONFORMANCE_PORT:-18080}"
URL="http://127.0.0.1:${HOST_PORT}"
FIXTURE="${ROOT}/fixtures/baseline/fixture.json"
OUT="${REPORT_DIR:-reports}/conformance-${ADAPTER}"
case "${OUT}" in /*) ;; *) OUT="${ROOT}/${OUT}" ;; esac
require_tools
command -v go >/dev/null || { log "missing required tool: go"; exit 2; }
trap cleanup EXIT

caps=$(docker run --rm "${IMAGE}" print-capabilities 2>/dev/null)
jq -e '.roles.server == true' <<<"${caps}" >/dev/null 2>&1 || { log "${ADAPTER} has no server role"; exit 2; }
upstream=$(jq -r '.upstream.name' <<<"${caps}")
version=$(jq -r '.upstream.version' <<<"${caps}")

name="conformance-${ADAPTER}-${RUN_ID}"
docker run -d --name "${name}" --label "openadr-interop.run=${RUN_ID}" -p "127.0.0.1:${HOST_PORT}:${PORT}" \
    "${IMAGE}" server --public-url "${URL}" >/dev/null || exit 1
wait_ready "${name}" || exit 1

id_of() { jq -r --arg n "$1" '.clients[] | select(.name == $n) | .clientId' "${FIXTURE}"; }
secret_of() { jq -r --arg n "$1" '.clients[] | select(.name == $n) | .clientSecret' "${FIXTURE}"; }
known=()
[[ -f "${ROOT}/conformance/known/${ADAPTER}.tsv" ]] && known=(-known "${ROOT}/conformance/known/${ADAPTER}.tsv" -known-version "${version}")

log "${upstream} ${version} at ${URL}"
cd "${ROOT}/conformance" && GOWORK=off \
    OPENADR_INTEROP_BL_SECRET="$(secret_of bl)" \
    OPENADR_INTEROP_VEN_SECRET="$(secret_of ven-3)" \
    OPENADR_INTEROP_VEN2_SECRET="$(secret_of ven-2)" \
    go run ./cmd/openadr-interop -url "${URL}" -out "${OUT}" \
        -bl-client-id "$(id_of bl)" -ven-client-id "$(id_of ven-3)" -ven2-client-id "$(id_of ven-2)" \
        -target-name "${upstream}" -target-version "${version}" ${known[@]+"${known[@]}"}
