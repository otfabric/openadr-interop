#!/usr/bin/env bash
# Fails when versions.yaml disagrees with what the builds actually pin.
#
# Every adapter Dockerfile declares its pins as build arguments with a default:
#   ARG UPSTREAM_VERSION=...     the upstream release
#   ARG UPSTREAM_SHA=...         its commit, where the build fetches one
#   ARG <NAME>_DIGEST=sha256:... every base image
# Each of those values must appear in versions.yaml.
#
# The Dockerfiles of OT Fabric's own stacks (own/*/Dockerfile) pin no upstream
# release, by design; their base image digests must appear in the entry of
# own_implementations whose build: is their directory.
#
# The adapters a release publishes (ADAPTERS of the Makefile) are exactly the
# implementations that versions.yaml says are published.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
status=0

need() {  # need <description> <value that must appear in versions.yaml>
    if grep -qF -- "$2" versions.yaml; then
        echo "ok   $1: $2"
    else
        echo "FAIL $1: $2 is not in versions.yaml"
        status=1
    fi
}

found=0
for dockerfile in adapters/*/Dockerfile; do
    [[ -f "${dockerfile}" ]] || continue
    found=$((found + 1))
    adapter=$(basename "$(dirname "${dockerfile}")")
    version=$(sed -nE 's/^ARG UPSTREAM_VERSION=(.+)$/\1/p' "${dockerfile}" | head -1)
    if [[ -z "${version}" ]]; then
        echo "FAIL ${adapter}: the Dockerfile has no ARG UPSTREAM_VERSION=<pin>"
        status=1
    else
        need "${adapter} version" "version: \"${version}\""
    fi
    sha=$(sed -nE 's/^ARG UPSTREAM_SHA=(.+)$/\1/p' "${dockerfile}" | head -1)
    [[ -n "${sha}" ]] && need "${adapter} revision" "${sha}"
    while read -r name digest; do
        need "${adapter} ${name}" "${digest}"
    done < <(sed -nE 's/^ARG ([A-Z0-9_]+_DIGEST)=(sha256:[0-9a-f]{64})$/\1 \2/p' "${dockerfile}")
    if grep -nE '^FROM ' "${dockerfile}" | grep -vE '@(\$\{[A-Z0-9_]+_DIGEST\}|sha256:)| AS [a-z]+$|^[0-9]+:FROM [a-z]+( AS [a-z]+)?$' >/dev/null; then
        echo "FAIL ${adapter}: a FROM line is not pinned by digest"
        status=1
    fi
done
# own_entry <directory>: the lines of the entry of own_implementations in
# versions.yaml whose build: is that directory; nothing when there is none.
own_entry() {
    awk -v build="    build: \"$1\"" '
        /^[^ #]/ { list = ($0 == "own_implementations:"); next }
        list && /^  [^ #]/ { if (hit) exit; lines = ""; next }
        list { lines = lines $0 "\n"; if (index($0, build) == 1) hit = 1 }
        END { if (hit) printf "%s", lines }
    ' versions.yaml
}
for dockerfile in own/*/Dockerfile; do
    [[ -f "${dockerfile}" ]] || continue
    build=$(dirname "${dockerfile}")
    entry=$(own_entry "${build}")
    if [[ -z "${entry}" ]]; then
        echo "FAIL ${build}: no entry of own_implementations in versions.yaml has build: \"${build}\""
        status=1
        continue
    fi
    while read -r name digest; do
        if grep -qF -- "${digest}" <<<"${entry}"; then
            echo "ok   ${build} ${name}: ${digest}"
        else
            echo "FAIL ${build} ${name}: ${digest} is not in its own_implementations entry of versions.yaml"
            status=1
        fi
    done < <(sed -nE 's/^ARG ([A-Z0-9_]+_DIGEST)=(sha256:[0-9a-f]{64})$/\1 \2/p' "${dockerfile}")
    if grep -nE '^FROM ' "${dockerfile}" | grep -vE '@(\$\{[A-Z0-9_]+_DIGEST\}|sha256:)| AS [a-z]+$|^[0-9]+:FROM [a-z]+( AS [a-z]+)?$' >/dev/null; then
        echo "FAIL ${build}: a FROM line is not pinned by digest"
        status=1
    fi
done
# The adapters in ADAPTERS of the Makefile, which publish-arch,
# publish-manifest and digests iterate, are the implementations with
# status: published in versions.yaml, and the reverse.
adapters=" $(sed -nE 's/^ADAPTERS \?= (.*)$/\1/p' Makefile) "
states=$(awk '
    /^[^ #]/ { if (name != "") print name, state; name = ""; list = ($0 == "implementations:"); next }
    list && /^  [^ #]/ { if (name != "") print name, state; name = $1; sub(/:$/, "", name); state = ""; next }
    list && name != "" && $1 == "status:" { state = $2 }
    END { if (name != "") print name, state }
' versions.yaml)
if [[ -z "${adapters// /}" ]]; then
    echo "FAIL Makefile: no ADAPTERS ?= <adapters>"
    status=1
fi
for adapter in ${adapters}; do
    state=$(awk -v name="${adapter}" '$1 == name { print $2 }' <<<"${states}")
    if [[ "${state}" == "published" ]]; then
        echo "ok   ${adapter}: in ADAPTERS and status: published"
    else
        echo "FAIL ${adapter}: in ADAPTERS of the Makefile, so a release publishes its image, but versions.yaml says status: ${state:-none}"
        status=1
    fi
done
while read -r name state; do
    [[ "${state}" == "published" && "${adapters}" != *" ${name} "* ]] || continue
    echo "FAIL ${name}: versions.yaml says status: published, but ADAPTERS of the Makefile does not name it"
    status=1
done <<<"${states}"
# elaadnl: the Dockerfile and requirements.txt name the same openadr3-client
# version (another version is a candidate build, installed without hashes),
# and the wheel_sha256 of versions.yaml is a file requirements.txt allows.
requirements_elaadnl=$(sed -nE 's/^openadr3-client==([^ \\]+).*$/\1/p' adapters/elaadnl/requirements.txt)
dockerfile_elaadnl=$(sed -nE 's/^ARG UPSTREAM_VERSION=(.+)$/\1/p' adapters/elaadnl/Dockerfile | head -1)
if [[ -n "${requirements_elaadnl}" && "${requirements_elaadnl}" == "${dockerfile_elaadnl}" ]]; then
    echo "ok   openadr3-client in Dockerfile and requirements.txt: ${requirements_elaadnl}"
else
    echo "FAIL openadr3-client in Dockerfile and requirements.txt: ${dockerfile_elaadnl:-none} and ${requirements_elaadnl:-none}"
    status=1
fi
wheel=$(sed -nE 's/^[[:space:]]*wheel_sha256: "([0-9a-f]{64})".*$/\1/p' versions.yaml)
if [[ -n "${wheel}" ]] && sed -nE '/^openadr3-client==/,/^[a-z]/ s/.*--hash=sha256:([0-9a-f]{64}).*/\1/p' adapters/elaadnl/requirements.txt | grep -xF -- "${wheel}" >/dev/null; then
    echo "ok   elaadnl wheel_sha256 in requirements.txt: ${wheel}"
else
    echo "FAIL elaadnl wheel_sha256: ${wheel:-none} is not a hash of openadr3-client in adapters/elaadnl/requirements.txt"
    status=1
fi
# cbsigma: the Dockerfile and go.mod name the same cB-Sigma openadr version.
gomod_cbsigma=$(sed -nE 's|^[[:space:]]*(require[[:space:]]+)?github.com/cB-Sigma/openadr (v[^ ]+).*$|\2|p' adapters/cbsigma/go.mod)
dockerfile_cbsigma=$(sed -nE 's/^ARG UPSTREAM_VERSION=(.+)$/\1/p' adapters/cbsigma/Dockerfile | head -1)
if [[ -n "${gomod_cbsigma}" && "${gomod_cbsigma}" == "${dockerfile_cbsigma}" ]]; then
    echo "ok   cB-Sigma openadr in Dockerfile and go.mod: ${gomod_cbsigma}"
else
    echo "FAIL cB-Sigma openadr in Dockerfile and go.mod: ${dockerfile_cbsigma:-none} and ${gomod_cbsigma:-none}"
    status=1
fi
# The conformance suite reports the version of this repository.
suite=$(sed -nE 's/^const version = "([^"]+)"$/\1/p' conformance/cmd/openadr-interop/main.go)
need "conformance suite version" "interop_version: \"${suite}\""
# The documents name the same version, and the same contract.
release=$(sed -nE 's/^interop_version: "([^"]+)".*/\1/p' versions.yaml)
contract=$(sed -nE 's/^contract_version: "([^"]+)".*/\1/p' versions.yaml)
doc() {  # doc <file> <description> <text the file must contain>
    if grep -qF -- "$3" "$1"; then
        echo "ok   $1: $2"
    else
        echo "FAIL $1: $2: no \"$3\""
        status=1
    fi
}
doc README.md "status" "**Status: v${release}**, container contract ${contract},"
doc README.md "release link" "releases/tag/v${release})"
doc PLAN.md "release" "## v${release} (released)"
doc docs/CONTAINER_CONTRACT.md "contract version" "Version: **${contract}**"
doc docs/CONTAINER_CONTRACT.md "contract history" "| ${contract} | "
newest=$(sed -nE 's/^## (v[0-9]+\.[0-9]+\.[0-9]+)$/\1/p' RELEASE.md | head -1)
if [[ "${newest}" == "v${release}" ]] && ! grep -qE '^## Unreleased' RELEASE.md; then
    echo "ok   RELEASE.md: newest entry ${newest}"
else
    echo "FAIL RELEASE.md: the newest entry is ${newest:-none}, not v${release}, or an entry is headed Unreleased"
    status=1
fi
if [[ "${found}" -eq 0 ]]; then
    echo "FAIL no adapter Dockerfile found"
    status=1
fi
exit "${status}"
