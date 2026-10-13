#!/usr/bin/env bash
# Reference-stack self-test: every client adapter against every server adapter.
#
# Usage: interop.sh <adapter> [<adapter> ...]      e.g. interop.sh openleadr elaadnl
#
# For each case in tests/cases.tsv and each client/server pairing the outcome
# must match the expectation of the case. Then, for each pairing, the basic
# OpenADR workflow is run and checked on values rather than outcomes:
# Business Logic publishes a program and an event, a VEN finds both and
# reports, Business Logic reads the report, and everything is removed again.
#
# tests/known-bugs.tsv lists bugs of specific upstream versions; a case they
# explain is counted as a known bug instead of a failure.
#
# This tests the reference adapters against each other. It says nothing about
# any library that consumes the images.
#
# Environment: REGISTRY, VERSION (image tag, default dev), VERBOSE=1,
#              REPORT_DIR (keeps every result document when set).

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck source=scripts/lib.sh
source "${SCRIPT_DIR}/lib.sh"

[[ $# -ge 1 ]] || { echo "usage: interop.sh <adapter> [<adapter> ...]" >&2; exit 2; }
ADAPTERS=("$@")
CASES="${ROOT}/tests/cases.tsv"
KNOWN_BUGS="${ROOT}/tests/known-bugs.tsv"
require_tools
WORK=$(mktemp -d)
trap 'rm -rf "${WORK}"; cleanup' EXIT
network_up

# ---- known upstream bugs that apply to the versions under test ----
# ${WORK}/active: "<adapter>\t<operation regex>\t<description>" per line.
: > "${WORK}/active"
: > "${WORK}/reproduced"
for adapter in "${ADAPTERS[@]}"; do
    docker run --rm "$(image_of "${adapter}")" print-capabilities > "${WORK}/caps-${adapter}.json" 2>/dev/null
    version=$(jq -r '.upstream.version' "${WORK}/caps-${adapter}.json")
    log "${adapter}: upstream ${version}"
    while IFS=$'\t' read -r bug_adapter bug_version bug_regex bug_text; do
        [[ -z "${bug_adapter}" || "${bug_adapter}" == \#* ]] && continue
        if [[ "${bug_adapter}" == "${adapter}" && "${bug_version}" == "${version}" ]]; then
            printf '%s\t%s\t%s\n' "${bug_adapter}" "${bug_regex}" "${bug_text}" >> "${WORK}/active"
            log "known bug in ${adapter} ${version}: ${bug_text}"
        fi
    done < "${KNOWN_BUGS}"
done

# has <adapter> <feature>: the adapter declares the feature.
has() { jq -e --arg f "$2" '.features[$f] == true' "${WORK}/caps-$1.json" >/dev/null 2>&1; }

# has_role <adapter> <server|client>
has_role() { jq -e --arg r "$2" '.roles[$r] == true' "${WORK}/caps-$1.json" >/dev/null 2>&1; }

# role_of <fixture> <client name>: bl or ven.
role_of() { jq -r --arg n "$2" '.clients[] | select(.name == $n) | .role' "${ROOT}/fixtures/$1/fixture.json"; }

# offers <client adapter> <role> <operation>: the library has the operation
# for that role.
offers() { jq -e --arg role "$2" --arg op "$3" '.clientOperations[$role] | index($op) != null' "${WORK}/caps-$1.json" >/dev/null 2>&1; }

# supported <client> <server> <needs>: the pairing declares every feature in
# the comma-separated list of "server:<feature>" and "client:<feature>".
supported() {
    local need side feature
    [[ "$3" == "-" ]] && return 0
    for need in ${3//,/ }; do
        side="${need%%:*}"; feature="${need#*:}"
        if [[ "${side}" == "server" ]]; then has "$2" "${feature}" || return 1
        else has "$1" "${feature}" || return 1; fi
    done
    return 0
}

SKIPPED=0
skip() { SKIPPED=$((SKIPPED + 1)); [[ -n "${VERBOSE:-}" ]] && log "skip: $*"; return 0; }

# known_bug <client> <server> <operation>: prints the description of a known
# bug that may affect this pairing and operation, if there is one.
known_bug() {
    local bug_adapter bug_regex bug_text
    while IFS=$'\t' read -r bug_adapter bug_regex bug_text; do
        if [[ "${bug_adapter}" == "$1" || "${bug_adapter}" == "$2" ]] && [[ "$3" =~ ${bug_regex} ]]; then
            echo "${bug_text}"
            return 0
        fi
    done < "${WORK}/active"
    return 1
}

# hit_known_bug <pair> <operation> <description>
hit_known_bug() {
    KNOWN=$((KNOWN + 1))
    echo "$3" >> "${WORK}/reproduced"
    [[ -n "${VERBOSE:-}" ]] && log "known bug: $1: $2"
    return 0
}

keep() {  # keep <label> <document>
    [[ -n "${REPORT_DIR:-}" ]] || return 0
    mkdir -p "${REPORT_DIR}"
    printf '%s\n' "$2" > "${REPORT_DIR}/$(echo "$1" | tr -c 'A-Za-z0-9._\n-' '_').json"
}

CLIENTS=(); SERVERS=()
for adapter in "${ADAPTERS[@]}"; do
    has_role "${adapter}" client && CLIENTS+=("${adapter}")
    has_role "${adapter}" server && SERVERS+=("${adapter}")
done
if [[ ${#CLIENTS[@]} -eq 0 || ${#SERVERS[@]} -eq 0 ]]; then
    log "needs at least one adapter with the client role and one with the server role (got: ${ADAPTERS[*]})"
    exit 2
fi
log "clients: ${CLIENTS[*]}; servers: ${SERVERS[*]} (${VERSION})"

# client <adapter> <server container> <fixture> <operation and flags...>
client() {
    local adapter="$1" server="$2" fixture="$3"; shift 3
    run_client "${adapter}" "$@" --url "$(url_of "${server}")" --fixture "/fixtures/${fixture}/fixture.json"
}

# resolve <server container> <fixture>: writes ${WORK}/ids-<container>, the
# IDs the VTN gave the objects of the fixture, as "<kind>:<name>\t<id>".
# Asked through the first client adapter, as the Business Logic client.
resolve() {
    local server="$1" fixture="$2" out="${WORK}/ids-$1" kind list field doc
    [[ -f "${out}" ]] && return 0
    : > "${out}"
    for kind in program event ven; do
        list="list-${kind}s"; field="${kind}Name"
        doc=$(client "${CLIENTS[0]}" "${server}" "${fixture}" "${list}" --as bl)
        jq -r --arg k "${kind}" --arg f "${field}" '.response // [] | .[] | "\($k):\(.[$f])\t\(.id)"' <<<"${doc}" >> "${out}" 2>/dev/null
    done
    return 0
}

# expand <server container> <operation>: replaces @kind:name@ by the ID.
expand() {
    local text="$2" key id
    while [[ "${text}" =~ @([a-z]+:[A-Za-z0-9._-]+)@ ]]; do
        key="${BASH_REMATCH[1]}"
        id=$(awk -F'\t' -v k="${key}" '$1 == k { print $2; exit }' "${WORK}/ids-$1")
        [[ -n "${id}" ]] || { echo "UNRESOLVED:${key}"; return 1; }
        text="${text//@${key}@/${id}}"
    done
    echo "${text}"
}

# server_for <adapter> <fixture> <state>: a ready server and its IDs. Servers
# of read-only cases are shared per adapter and fixture.
server_for() {
    local adapter="$1" fixture="$2" state="$3" tag name
    if [[ "${state}" == "ro" ]]; then tag="shared-${fixture}"; else tag="case${n}"; fi
    name=$(SERVER_TAG="${tag}" server_name "${adapter}")
    if [[ "$(docker inspect -f '{{.State.Running}}' "${name}" 2>/dev/null)" != "true" ]]; then
        SERVER_TAG="${tag}" start_server "${adapter}" --fixture "/fixtures/${fixture}/fixture.json" >/dev/null
        wait_ready "${name}" || return 1
    fi
    resolve "${name}" "${fixture}"
    echo "${name}"
}

# ---- the cases ----
n=0
while IFS=$'\t' read -r state fixture want_exit want_summary needs operation; do
    [[ -z "${state}" || "${state}" == \#* ]] && continue
    n=$((n + 1))
    read -ra words <<<"${operation}"
    op="${words[0]}"
    who=""
    anonymous=""
    for ((i = 1; i < ${#words[@]}; i++)); do
        [[ "${words[i]}" == "--as" ]] && who="${words[i + 1]}"
        [[ "${words[i]}" == "--anonymous" ]] && anonymous="${words[i + 1]}"
    done
    # A client without a token is no client of the fixture: its role is given.
    if [[ -n "${anonymous}" ]]; then role="${anonymous}"; else role=$(role_of "${fixture}" "${who}"); fi
    for server in "${SERVERS[@]}"; do
        for cl in "${CLIENTS[@]}"; do
            pair="${cl}->${server}"
            if ! offers "${cl}" "${role}" "${op}" || ! supported "${cl}" "${server}" "${needs}"; then
                skip "${pair}: ${operation}"; continue
            fi
            name=$(server_for "${server}" "${fixture}" "${state}") || { fail "${pair}: no ${server} server with the ${fixture} fixture"; continue; }
            expanded=$(expand "${name}" "${operation}") || { fail "${pair}: ${operation}: ${expanded}"; continue; }
            read -ra args <<<"${expanded}"
            doc=$(client "${cl}" "${name}" "${fixture}" "${args[@]}"); got_exit=$?
            keep "case${n}-${cl}-${server}" "${doc}"
            got_summary=$(jq -r "${SUMMARY}" <<<"${doc}" 2>/dev/null)
            if [[ "${got_exit}" == "${want_exit}" && "${got_summary}" == "${want_summary}" ]]; then
                ok "${pair}: ${operation}"
            elif bug=$(known_bug "${cl}" "${server}" "${operation}"); then
                hit_known_bug "${pair}" "${operation}" "${bug}"
            else
                fail "${pair}: ${operation}: exit ${got_exit}, ${got_summary}; want exit ${want_exit}, ${want_summary}"
                [[ -n "${VERBOSE:-}" ]] && log "    $(jq -c '{error, problem}' <<<"${doc}" 2>/dev/null)"
            fi
            [[ "${state}" == "rw" ]] && stop_server "${name}"
        done
    done
done < "${CASES}"

# ---- the workflow, checked on values ----
# check <label> <document> <jq expression that must hold>
check() {
    if jq -e "$3" <<<"$2" >/dev/null 2>&1; then ok "$1"; else fail "$1: $(jq -c '{ok, error, status, response}' <<<"$2" 2>/dev/null | cut -c1-600)"; fi
}

for server in "${SERVERS[@]}"; do
    for cl in "${CLIENTS[@]}"; do
        pair="${cl}->${server}"
        if ! offers "${cl}" bl create-program || ! offers "${cl}" bl create-event || ! offers "${cl}" ven create-report; then
            skip "${pair}: workflow"; continue
        fi
        n="workflow"
        name=$(server_for "${server}" baseline rw) || { fail "${pair}: no ${server} server for the workflow"; continue; }
        c() { client "${cl}" "${name}" baseline "$@"; }

        doc=$(c create-program --as bl --body '{"programName":"workflow","payloadDescriptors":[{"objectType":"EVENT_PAYLOAD_DESCRIPTOR","payloadType":"PRICE","units":"KWH","currency":"EUR"}]}')
        keep "workflow-${cl}-${server}-1-create-program" "${doc}"
        check "${pair}: Business Logic creates a program" "${doc}" '.ok and .status == 201 and .response.programName == "workflow"
            and (.response.id | type == "string") and .response.payloadDescriptors[0].currency == "EUR"'
        program=$(jq -r '.response.id // empty' <<<"${doc}")

        doc=$(c create-event --as bl --body "{\"programID\":\"${program}\",\"eventName\":\"workflow-prices\",\"priority\":3,\"intervalPeriod\":{\"start\":\"2031-06-01T10:00:00Z\",\"duration\":\"PT15M\"},\"reportDescriptors\":[{\"payloadType\":\"USAGE\"}],\"intervals\":[{\"id\":0,\"payloads\":[{\"type\":\"PRICE\",\"values\":[0.31]}]},{\"id\":1,\"payloads\":[{\"type\":\"PRICE\",\"values\":[0.24]}]}]}")
        keep "workflow-${cl}-${server}-2-create-event" "${doc}"
        check "${pair}: Business Logic creates an event" "${doc}" '.ok and .status == 201 and .response.eventName == "workflow-prices" and (.response.id | type == "string")'
        event=$(jq -r '.response.id // empty' <<<"${doc}")

        doc=$(c list-programs --as ven-2)
        check "${pair}: the VEN discovers the program" "${doc}" '.ok and ([.response[].programName] | index("workflow") != null)'

        doc=$(c list-events --as ven-2 --program-id "${program}")
        keep "workflow-${cl}-${server}-4-list-events" "${doc}"
        check "${pair}: the VEN reads the event as it was published" "${doc}" '.ok and (.response | length) == 1
            and .response[0].eventName == "workflow-prices" and .response[0].priority == 3
            and (.response[0].intervalPeriod.start | fromdateiso8601) == 1938074400
            and ([.response[0].intervals[] | .payloads[0].values[0]] == [0.31, 0.24])
            and ([.response[0].intervals[].id] == [0, 1])
            and .response[0].reportDescriptors[0].payloadType == "USAGE"'

        doc=$(c create-report --as ven-2 --body "{\"eventID\":\"${event}\",\"clientName\":\"site-2\",\"reportName\":\"usage\",\"resources\":[{\"resourceName\":\"meter\",\"intervals\":[{\"id\":0,\"payloads\":[{\"type\":\"USAGE\",\"values\":[1.8]}]}]}]}")
        keep "workflow-${cl}-${server}-5-create-report" "${doc}"
        check "${pair}: the VEN reports, under its own client ID" "${doc}" '.ok and .status == 201 and .response.clientID == "interop-ven-2" and .response.eventID != null'
        report=$(jq -r '.response.id // empty' <<<"${doc}")

        doc=$(c list-reports --as bl --program-id "${program}")
        keep "workflow-${cl}-${server}-6-list-reports" "${doc}"
        check "${pair}: Business Logic reads the report" "${doc}" '.ok and (.response | length) == 1 and .response[0].reportName == "usage"
            and .response[0].resources[0].resourceName == "meter"
            and .response[0].resources[0].intervals[0].payloads[0].values == [1.8]'

        doc=$(c list-reports --as ven-1 --event-id "${event}")
        check "${pair}: another VEN does not see the report" "${doc}" '.ok and (.response | length) == 0'

        doc=$(c update-report --as ven-2 --id "${report}" --body "{\"eventID\":\"${event}\",\"clientName\":\"site-2\",\"reportName\":\"usage-corrected\",\"resources\":[{\"resourceName\":\"meter\",\"intervals\":[{\"id\":0,\"payloads\":[{\"type\":\"USAGE\",\"values\":[1.9]}]}]}]}")
        check "${pair}: the VEN corrects its report" "${doc}" '.ok and .status == 200 and .response.reportName == "usage-corrected"'
        doc=$(c get-report --as bl --id "${report}")
        check "${pair}: Business Logic reads the correction" "${doc}" '.ok and .response.resources[0].intervals[0].payloads[0].values == [1.9]'

        doc=$(c delete-report --as ven-2 --id "${report}")
        check "${pair}: the VEN deletes its report" "${doc}" '.ok and .status == 200'
        doc=$(c delete-event --as bl --id "${event}")
        check "${pair}: Business Logic deletes the event" "${doc}" '.ok and .status == 200'
        doc=$(c delete-program --as bl --id "${program}")
        check "${pair}: Business Logic deletes the program" "${doc}" '.ok and .status == 200'
        doc=$(c get-program --as bl --id "${program}")
        check "${pair}: the program is gone" "${doc}" '.ok == false and .status == 404'
        stop_server "${name}"
    done
done

# ---- a known bug that no longer shows is a stale entry ----
while IFS=$'\t' read -r bug_adapter bug_regex bug_text; do
    [[ -z "${bug_adapter}" ]] && continue
    if ! grep -qxF "${bug_text}" "${WORK}/reproduced"; then
        fail "known bug of ${bug_adapter} did not show: ${bug_text} (remove it from tests/known-bugs.tsv)"
    fi
done < "${WORK}/active"

finish
