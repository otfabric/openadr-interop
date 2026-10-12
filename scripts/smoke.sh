#!/usr/bin/env bash
# Container-contract smoke test for one adapter image.
#
# Usage: smoke.sh <adapter>          e.g. smoke.sh openleadr
#
# Checks docs/CONTAINER_CONTRACT.md for the roles the adapter declares: the
# command surface, capabilities, the baked fixtures, usage errors, and for a
# server readiness, the event stream, exit codes and a clean stop on SIGTERM.
# The checks of a client that need a VTN run against the adapter itself when
# it is also a server, and otherwise against SMOKE_SERVER.
#
# Environment: REGISTRY, VERSION (image tag, default dev), VERBOSE=1,
#              SMOKE_SERVER (server adapter for a client-only adapter, default openleadr),
#              REPORT_DIR (keeps the capability document when set).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"

ADAPTER="${1:?usage: smoke.sh <adapter>}"
IMAGE="$(image_of "${ADAPTER}")"
require_tools
bad=$(mktemp -d)
trap 'rm -rf "${bad}"; cleanup' EXIT
network_up
log "${IMAGE}"

expect_exit() {  # expect_exit <code> <label> <docker run args...>
    local want="$1" label="$2"; shift 2
    docker run --rm --network "${NETWORK}" --label "openadr-interop.run=${RUN_ID}" "$@" >/dev/null 2>&1
    local got=$?
    if [[ "${got}" == "${want}" ]]; then ok "${label}: exit ${got}"; else fail "${label}: exit ${got}, want ${want}"; fi
}

# ---- capabilities ----
caps=$(docker run --rm "${IMAGE}" print-capabilities 2>/dev/null)
if [[ -n "${REPORT_DIR:-}" ]]; then
    mkdir -p "${REPORT_DIR}"
    printf '%s\n' "${caps}" > "${REPORT_DIR}/capabilities-${ADAPTER}.json"
fi
if jq -e --arg a "${ADAPTER}" '
        .schemaVersion == "1.0" and .adapter == $a and .protocol == "openadr3"
        and (.openadrVersions | type == "array" and length > 0)
        and (.adapterVersion | type == "string") and (.upstream.name | type == "string")
        and (.upstream.version | type == "string") and (.upstream.license | type == "string")
        and (.upstream.independentEngine | type == "boolean")
        and (.roles.server | type == "boolean") and (.roles.client | type == "boolean")
        and (.roles.server or .roles.client)
        and (.features | type == "object") and ([.features[] | type == "boolean"] | all)
        and (.serverObjects | type == "array")
        and (.clientOperations.bl | type == "array") and (.clientOperations.ven | type == "array")' <<<"${caps}" >/dev/null 2>&1
then ok "print-capabilities"; else fail "print-capabilities is not a valid capability document: ${caps}"; fi
if [[ $(wc -l <<<"${caps}") -eq 1 ]]; then ok "print-capabilities is one line"; else fail "print-capabilities printed more than one line"; fi
has_role() { jq -e --arg r "$1" '.roles[$r] == true' <<<"${caps}" >/dev/null 2>&1; }
offers() { jq -e --arg role "$1" --arg op "$2" '.clientOperations[$role] | index($op) != null' <<<"${caps}" >/dev/null 2>&1; }

# ---- the baked fixtures are the repository's ----
for fixture in "${ROOT}"/fixtures/*/fixture.json; do
    fname=$(basename "$(dirname "${fixture}")")
    if diff -q <(docker run --rm "${IMAGE}" print-fixture "${fname}" 2>/dev/null) "${fixture}" >/dev/null
    then ok "print-fixture ${fname}"; else fail "print-fixture ${fname} differs from fixtures/${fname}/fixture.json"; fi
done
if diff -q <(docker run --rm "${IMAGE}" print-fixture 2>/dev/null) "${ROOT}/fixtures/baseline/fixture.json" >/dev/null
then ok "print-fixture defaults to baseline"; else fail "print-fixture without a name is not the baseline fixture"; fi
expect_exit 1 "print-fixture of an unknown name" "${IMAGE}" print-fixture no-such-fixture
expect_exit 0 "version" "${IMAGE}" version

# ---- usage errors never touch the network ----
expect_exit 2 "unknown command" "${IMAGE}" frobnicate
if has_role server; then
    expect_exit 2 "server with an unknown flag" "${IMAGE}" server --frobnicate 1
    expect_exit 2 "server with a port that is not a number" "${IMAGE}" server --port http
    expect_exit 2 "server with a public URL that is not one" "${IMAGE}" server --public-url nowhere
    if jq -e '.features.serverMqtt == true' <<<"${caps}" >/dev/null 2>&1; then
        expect_exit 2 "server with the broker on the port of the API" "${IMAGE}" server --port 9000 --mqtt-port 9000
        expect_exit 2 "server with a broker URL that is not one" "${IMAGE}" server --mqtt-public-url http://nowhere
    fi
else
    expect_exit 2 "server on an adapter without the role" "${IMAGE}" server
fi
if has_role client; then
    expect_exit 2 "client without an operation" "${IMAGE}" client
    expect_exit 2 "client with an unknown operation" "${IMAGE}" client frobnicate --url http://nowhere --as bl
    expect_exit 2 "client without --url" "${IMAGE}" client list-programs --as bl
    expect_exit 2 "client without --as" "${IMAGE}" client list-programs --url http://nowhere
    expect_exit 2 "client with a URL that is not one" "${IMAGE}" client list-programs --url nowhere --as bl
    expect_exit 2 "client that is not in the fixture" "${IMAGE}" client list-programs --url http://nowhere --as nobody
    expect_exit 2 "client with an unknown flag" "${IMAGE}" client list-programs --url http://nowhere --as bl --frobnicate 1
    expect_exit 2 "client with a flag of another operation" "${IMAGE}" client list-programs --url http://nowhere --as bl --id abc
    expect_exit 2 "get without --id" "${IMAGE}" client get-program --url http://nowhere --as bl
    expect_exit 2 "an ID that is not one" "${IMAGE}" client get-program --url http://nowhere --as bl --id a/b
    expect_exit 2 "create without --body" "${IMAGE}" client create-program --url http://nowhere --as bl
    expect_exit 2 "a body that is not JSON" "${IMAGE}" client create-program --url http://nowhere --as bl --body '{not json'
    expect_exit 2 "a body that is not an object" "${IMAGE}" client create-program --url http://nowhere --as bl --body '[]'
    expect_exit 2 "a limit that is not a number" "${IMAGE}" client list-programs --url http://nowhere --as bl --limit many
    expect_exit 2 "a missing fixture" "${IMAGE}" client list-programs --url http://nowhere --as bl --fixture /no/such/fixture.json
    if ! offers ven create-program; then
        expect_exit 2 "an operation the library does not offer the role" "${IMAGE}" client create-program --url http://nowhere --as ven-1 --body '{"programName":"p"}'
    fi
    if ! offers bl create-report; then
        expect_exit 2 "an operation the library does not offer the role" "${IMAGE}" client create-report --url http://nowhere --as bl --body '{}'
    fi

    # ---- a connection that cannot be made ----
    doc=$(run_client "${ADAPTER}" list-programs --url "http://no-such-host-${RUN_ID}:${PORT}" --as ven-1 --timeout-ms 3000); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 3 ]] && jq -e --arg a "${ADAPTER}" '.schemaVersion == "1.0" and .adapter == $a
            and .operation == "list-programs" and .client == "ven-1" and .role == "ven"
            and .ok == false and .error.code == "connect-failed" and .authenticated == false
            and .status == null and .response == null and .problem == null' <<<"${doc}" >/dev/null 2>&1
    then ok "connect failure: exit 3 and a result document"; else fail "connect failure: exit ${CLIENT_EXIT}, ${doc}"; fi
fi

if has_role server; then
    # ---- an invalid fixture stops the server before anything listens ----
    chmod 755 "${bad}"
    # Each invalid fixture sits in a directory named after it, so that it
    # breaks exactly one rule and not also "name is the directory name".
    mkdir -p "${bad}"/{good,nobl,ref,dup,broken,name}/baseline "${bad}/name/other"
    base="${ROOT}/fixtures/baseline/fixture.json"
    cp "${base}" "${bad}/good/baseline/fixture.json"
    jq '.clients |= map(select(.role != "bl"))' "${base}" > "${bad}/nobl/baseline/fixture.json"
    jq '.events[0].program = "no-such-program"' "${base}" > "${bad}/ref/baseline/fixture.json"
    jq '.programs[1].programName = .programs[0].programName' "${base}" > "${bad}/dup/baseline/fixture.json"
    echo '{ not json' > "${bad}/broken/baseline/fixture.json"
    cp "${base}" "${bad}/name/other/fixture.json"
    chmod -R a+rX "${bad}"
    expect_exit 4 "server with a fixture without a Business Logic client" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/nobl/baseline/fixture.json
    expect_exit 4 "server with an event of an unknown program" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/ref/baseline/fixture.json
    expect_exit 4 "server with two programs of one name" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/dup/baseline/fixture.json
    expect_exit 4 "server with a fixture that is not JSON" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/broken/baseline/fixture.json
    expect_exit 4 "server with a fixture whose name is not its directory" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/name/other/fixture.json
    expect_exit 4 "server with a missing fixture" "${IMAGE}" server --fixture /no/such/fixture.json
    # The control: the same mount with a valid fixture is served.
    control=$(docker run -d --network "${NETWORK}" --label "openadr-interop.run=${RUN_ID}" -v "${bad}:/bad:ro" "${IMAGE}" server --fixture /bad/good/baseline/fixture.json)
    served=no
    for _ in $(seq 1 300); do
        if docker logs "${control}" 2>/dev/null | grep -q '"event":"ready"'; then served=yes; break; fi
        [[ "$(docker inspect -f '{{.State.Running}}' "${control}" 2>/dev/null)" == "true" ]] || break
        sleep 0.2
    done
    docker rm -f "${control}" >/dev/null 2>&1
    if [[ "${served}" == "yes" ]]; then ok "server with a mounted valid fixture: ready"; else fail "server with a mounted valid fixture did not become ready"; fi
fi

# ---- the server the remaining checks run against ----
SERVER="${ADAPTER}"
if ! has_role server; then
    SERVER="${SMOKE_SERVER:-openleadr}"
    if ! docker image inspect "$(image_of "${SERVER}")" >/dev/null 2>&1; then
        log "no image of the server adapter ${SERVER}: the checks that need a VTN are not run"
        finish; exit $?
    fi
    log "client checks run against the ${SERVER} server"
fi
name=$(start_server "${SERVER}")
if wait_ready "${name}"; then ok "server becomes ready"; else fail "server did not become ready"; finish; exit 1; fi

if has_role server; then
    # ---- server: the ready event, health, user ----
    first=$(docker logs "${name}" 2>/dev/null | head -1)
    if jq -e --arg a "${ADAPTER}" --arg url "$(url_of "${name}")" '.event == "ready" and .adapter == $a
            and .fixture == "baseline" and (.address | type == "string") and .url == $url
            and (.tokenUrl | type == "string")' <<<"${first}" >/dev/null 2>&1
    then ok "ready event"; else fail "first stdout line is not the ready event: ${first}"; fi
    if jq -e '.features.serverMqtt == true' <<<"${caps}" >/dev/null 2>&1; then
        if jq -e '.mqttUrl | type == "string" and startswith("mqtt://")' <<<"${first}" >/dev/null 2>&1
        then ok "ready event names the broker"; else fail "ready event without mqttUrl: ${first}"; fi
        # The broker answers on its port, and turns away a client it does not know.
        if docker exec "${name}" mosquitto_sub -h 127.0.0.1 -p 1883 -u nobody -P nothing -t '#' -C 1 -W 2 >/dev/null 2>&1
        then fail "the broker let an unknown client subscribe to everything"; else ok "the broker refuses an unknown client"; fi
    fi
    health=$(docker inspect --format '{{if .State.Health}}yes{{else}}no{{end}}' "${name}")
    if [[ "${health}" == yes ]]; then ok "image has a health check"; else fail "image has no health check"; fi
    user=$(docker exec "${name}" id -u 2>/dev/null)
    if [[ "${user}" == 10001 ]]; then ok "runs as uid 10001"; else fail "runs as uid ${user}, want 10001"; fi
fi

if has_role client; then
    # ---- client: result documents ----
    doc=$(run_client "${ADAPTER}" list-programs --url "$(url_of "${name}")" --as bl); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 0 ]] && jq -e --arg a "${ADAPTER}" '
            .schemaVersion == "1.0" and .adapter == $a and .operation == "list-programs"
            and .client == "bl" and .role == "bl" and .ok == true and .error == null and .problem == null
            and .authenticated == true and .status == 200 and (.elapsedMs | type == "number")
            and ([.response[].programName] | sort) == ["day-ahead", "group-a-flex"]
            and ([.response[] | (.id | type == "string") and (.createdDateTime | type == "string")] | all)' <<<"${doc}" >/dev/null 2>&1
    then ok "client result document"; else fail "list-programs: exit ${CLIENT_EXIT}, ${doc}"; fi
    if [[ $(wc -l <<<"${doc}") -eq 1 ]]; then ok "client result is one line"; else fail "client printed more than one line on stdout"; fi

    doc=$(run_client "${ADAPTER}" get-program --url "$(url_of "${name}")" --as bl --id "no-such-program"); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 1 ]] && jq -e '.ok == false and .error.code == "http-error" and .authenticated == true
            and .status == 404 and .response == null and (.problem | type == "object")' <<<"${doc}" >/dev/null 2>&1
    then ok "error result document"; else fail "get of an unknown program: exit ${CLIENT_EXIT}, ${doc}"; fi

    # A client whose secret the VTN does not know: a fixture that differs
    # from the server's in one secret.
    jq '(.clients[] | select(.name == "ven-1") | .clientSecret) = "not-the-secret"' "${ROOT}/fixtures/baseline/fixture.json" > "${bad}/wrong.json"
    mkdir -p "${bad}/wrong/baseline" && mv "${bad}/wrong.json" "${bad}/wrong/baseline/fixture.json" && chmod -R a+rX "${bad}"
    doc=$(CLIENT_MOUNT="${bad}:/bad:ro" run_client "${ADAPTER}" list-programs --url "$(url_of "${name}")" --as ven-1 --fixture /bad/wrong/baseline/fixture.json); CLIENT_EXIT=$?
    if [[ "${CLIENT_EXIT}" == 1 ]] && jq -e '.ok == false and .error.code == "auth-failed" and .authenticated == false
            and .status == null and .response == null' <<<"${doc}" >/dev/null 2>&1
    then ok "wrong client secret: auth-failed"; else fail "wrong client secret: exit ${CLIENT_EXIT}, ${doc}"; fi

    # ---- a client that waits for a VTN that never answers ----
    docker pause "${name}" >/dev/null
    doc=$(run_client "${ADAPTER}" list-programs --url "$(url_of "${name}")" --as bl --timeout-ms 1000); CLIENT_EXIT=$?
    docker unpause "${name}" >/dev/null
    if [[ "${CLIENT_EXIT}" == 1 || "${CLIENT_EXIT}" == 3 ]] && jq -e '.ok == false and (.error.code == "timeout" or .error.code == "connect-failed")' <<<"${doc}" >/dev/null 2>&1
    then ok "silent VTN: $(jq -r '.error.code' <<<"${doc}"), exit ${CLIENT_EXIT}"; else fail "silent VTN: exit ${CLIENT_EXIT}, ${doc}"; fi
fi

if has_role server; then
    # ---- a port that is already taken ----
    docker exec "${name}" /usr/local/bin/openadr-interop server --ready-file /tmp/second >/dev/null 2>&1
    code=$?
    if [[ "${code}" != 0 ]]; then ok "second server on a busy port fails (exit ${code})"; else fail "second server on a busy port exited 0"; fi
    if docker exec "${name}" test -f "${READY_FILE}" 2>/dev/null; then ok "the first server is still ready"; else fail "the second server took the first one down"; fi

    # ---- stdout, and a clean stop ----
    events=$(docker logs "${name}" 2>/dev/null)
    if grep -v '^{' <<<"${events}" | grep -q .; then fail "server stdout has lines that are not JSON"; else ok "server stdout is JSON Lines only"; fi
    docker stop -t 20 "${name}" >/dev/null
    code=$(docker inspect --format '{{.State.ExitCode}}' "${name}")
    last=$(docker logs "${name}" 2>/dev/null | tail -1)
    if [[ "${code}" == 0 ]]; then ok "SIGTERM: exit 0"; else fail "SIGTERM: exit ${code}"; fi
    if jq -e '.event == "stopped"' <<<"${last}" >/dev/null 2>&1; then ok "stopped event"; else fail "last stdout line is not the stopped event: ${last}"; fi
fi
stop_server "${name}"

finish
