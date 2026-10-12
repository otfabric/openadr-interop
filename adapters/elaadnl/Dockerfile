# SPDX-License-Identifier: MIT
# openadr-interop adapter for the ElaadNL openadr3-client.
#
# Build from the repository root:
#   docker build -f adapters/elaadnl/Dockerfile -t openadr-interop-elaadnl:dev .
#
# openadr3-client is Apache-2.0; the adapter is MIT like the rest of the
# repository. Pins are mirrored in versions.yaml.

# Where the base image comes from. It is pinned by digest, so a mirror of
# Docker Hub serves exactly the same image; CI uses one to stay clear of
# Docker Hub's pull rate limit (see the Makefile).
ARG BASE_REGISTRY=docker.io/library
# python:3.14.8-slim-trixie, multi-arch index (linux/amd64 and linux/arm64).
ARG PYTHON_DIGEST=sha256:a2b82f3c48559aa0a8446d9af49826b6e2b2016f4cd2afabfe6013ec53729170

# ---------------------------------------------------------------------------
# Stage 1: openadr3-client and its dependencies from PyPI, the adapter
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/python:3.14.8-slim-trixie@${PYTHON_DIGEST} AS build

# The upstream release. requirements.txt pins the same version, and every
# dependency, with the hashes of their files. A candidate build overrides
# UPSTREAM_VERSION: there is no lock for another release, so it is installed
# with whatever dependencies it resolves to, without hash checking.
ARG UPSTREAM_VERSION=2.4.0
ARG ADAPTER_VERSION=dev

ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_ROOT_USER_ACTION=ignore

COPY adapters/elaadnl/requirements.txt /src/requirements.txt

RUN set -eu; \
    site=/opt/openadr-interop/site-packages; \
    pinned="$(sed -nE 's/^openadr3-client==([^ \\]+).*/\1/p' /src/requirements.txt)"; \
    test -n "${pinned}"; \
    if [ "${UPSTREAM_VERSION}" = "${pinned}" ]; then \
        pip install --only-binary :all: --require-hashes --no-compile --target "${site}" -r /src/requirements.txt; \
    else \
        echo "candidate build: openadr3-client ${UPSTREAM_VERSION} instead of the pin ${pinned}, no lock to check" >&2; \
        pip install --only-binary :all: --no-compile --target "${site}" "openadr3-client==${UPSTREAM_VERSION}"; \
    fi; \
    installed="$(PYTHONPATH="${site}" python3 -c 'from importlib.metadata import version; print(version("openadr3-client"))')"; \
    test "${installed}" = "${UPSTREAM_VERSION}"; \
    wheel_sha="$(sed -nE '/^openadr3-client==/,/^[a-z]/ s/.*--hash=sha256:([0-9a-f]{64}).*/\1/p' /src/requirements.txt | head -1)"; \
    if [ "${UPSTREAM_VERSION}" != "${pinned}" ]; then wheel_sha=unverified; fi; \
    mkdir -p /licenses/python /licenses/packages; \
    cp /usr/local/lib/python3.14/LICENSE.txt /licenses/python/LICENSE.txt; \
    : > /licenses/packages/PACKAGES.txt; \
    for info in "${site}"/*.dist-info; do \
        name="$(basename "${info}" .dist-info)"; \
        printf '%s\t%s\n' "${name}" "$(sed -nE 's/^License(-Expression)?: //p' "${info}/METADATA" | head -1)" >> /licenses/packages/PACKAGES.txt; \
        mkdir -p "/licenses/packages/${name}"; \
        find "${info}" -type f \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'AUTHORS*' \) \
            -exec cp {} "/licenses/packages/${name}/" \; ; \
        rmdir "/licenses/packages/${name}" 2>/dev/null || true; \
    done; \
    test -d "/licenses/packages/openadr3_client-${installed}"; \
    printf '{"adapterVersion":"%s","upstreamVersion":"%s","upstreamRevision":"sha256:%s"}\n' \
        "${ADAPTER_VERSION}" "${installed}" "${wheel_sha}" > /opt/openadr-interop/build-info.json

COPY adapters/elaadnl/openadr_interop /opt/openadr-interop/openadr_interop
COPY adapters/elaadnl/openadr-interop /opt/openadr-interop/bin/openadr-interop
COPY LICENSE /licenses/openadr-interop/LICENSE

# The runtime user cannot write byte code next to the sources: compile now.
RUN python3 -m compileall -q /opt/openadr-interop \
    && chmod 0755 /opt/openadr-interop/bin/openadr-interop

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/python:3.14.8-slim-trixie@${PYTHON_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/openadr-interop \
    && chown interop /run/openadr-interop

COPY --from=build /opt/openadr-interop /opt/openadr-interop
COPY --from=build /opt/openadr-interop/bin/openadr-interop /usr/local/bin/openadr-interop
COPY --from=build /licenses /licenses
COPY fixtures /fixtures

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

LABEL org.opencontainers.image.title="openadr-interop-elaadnl" \
      org.opencontainers.image.description="OpenADR 3.1 reference VEN and Business Logic client built on the ElaadNL openadr3-client" \
      org.opencontainers.image.source="https://github.com/otfabric/openadr-interop" \
      org.opencontainers.image.licenses="MIT AND Apache-2.0" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

USER interop

# This image has no server: without a command it describes itself.
ENTRYPOINT ["/usr/local/bin/openadr-interop"]
CMD ["print-capabilities"]
