#!/bin/sh
# SPDX-License-Identifier: MIT
#
# Collects the licence texts of every crate that goes into the VTN binary.
#
# Usage: collect-licenses.sh <output directory>     (run in the OpenLEADR
#        workspace, after the build; uses cargo only, no further tool)
#
# The crates are those cargo resolves for the openleadr-vtn package on this
# target with normal and build dependencies, proc macros included: they are
# not linked, but the code they generate is. The texts are copied from the
# sources cargo downloaded and verified against Cargo.lock. CRATES.txt lists
# every crate with the licence it declares; a crate that ships no licence
# text is listed in NO-TEXT.txt and is covered by that declaration.

set -eu

out="${1:?usage: collect-licenses.sh <output directory>}"
host=$(rustc -vV | sed -n 's/^host: //p')
registry="${CARGO_HOME:-$HOME/.cargo}/registry/src"
mkdir -p "${out}"
: > "${out}/CRATES.txt"
: > "${out}/NO-TEXT.txt"

cargo tree --locked --offline -p openleadr-vtn --features internal-oauth \
    --edges normal,build --target "${host}" --prefix none \
    --format '{p}|{l}' | sed 's/ (\*)$//' | sort -u > /tmp/crates.list

while IFS='|' read -r package license; do
    name=$(echo "${package}" | cut -d' ' -f1)
    version=$(echo "${package}" | cut -d' ' -f2 | sed 's/^v//')
    # The crates of the OpenLEADR workspace itself: their licence is upstream's.
    case "${package}" in *"(/"*) continue ;; esac
    printf '%s %s\t%s\n' "${name}" "${version}" "${license}" >> "${out}/CRATES.txt"
    source=$(find "${registry}" -mindepth 2 -maxdepth 2 -type d -name "${name}-${version}" | head -1)
    target="${out}/${name}-${version}"
    if [ -n "${source}" ]; then
        mkdir -p "${target}"
        find "${source}" -maxdepth 1 \( -type f -o -type d \) \
            \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'UNLICENSE*' -o -iname 'COPYRIGHT*' \) \
            -exec cp -R {} "${target}/" \;
    fi
    if [ -z "${source}" ] || [ -z "$(find "${target}" -type f | head -1)" ]; then
        rmdir "${target}" 2>/dev/null || true
        printf '%s %s\t%s\n' "${name}" "${version}" "${license}" >> "${out}/NO-TEXT.txt"
    fi
done < /tmp/crates.list

test -s "${out}/CRATES.txt"
echo "collect-licenses: $(wc -l < "${out}/CRATES.txt") crates, $(wc -l < "${out}/NO-TEXT.txt") without a licence text"
