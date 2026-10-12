# SPDX-License-Identifier: MIT
# openadr-interop adapter for the OpenLEADR VTN (openleadr-rs).
#
# Build from the repository root:
#   docker build -f adapters/openleadr/Dockerfile -t openadr-interop-openleadr:dev .
#
# OpenLEADR is Apache-2.0 OR MIT; the adapter is MIT like the rest of the
# repository. OpenLEADR publishes no image, so the VTN is built from source at
# the commit of its release. Pins are mirrored in versions.yaml.

# Where the base images come from. They are pinned by digest, so a mirror of
# Docker Hub serves exactly the same images; CI uses one to stay clear of
# Docker Hub's pull rate limit (see the Makefile).
ARG BASE_REGISTRY=docker.io/library
# rust:1.94-alpine, multi-arch index (linux/amd64 and linux/arm64).
ARG RUST_DIGEST=sha256:77237dd363a0b127bb5ef532c2d64c0deb380b738e43a9c4bdac73398d6d0a08
# alpine:3.22, multi-arch index.
ARG ALPINE_DIGEST=sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8

# ---------------------------------------------------------------------------
# Stage 1: the OpenLEADR VTN, with its internal OAuth provider
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/rust:1.94-alpine@${RUST_DIGEST} AS build

# The upstream release and its commit. A candidate build overrides both.
ARG UPSTREAM_VERSION=v0.2.7
ARG UPSTREAM_SHA=f586cc3cf26fe2807041cadf989e219c6e5b2073
ARG ADAPTER_VERSION=dev

RUN apk add --no-cache git cmake g++ make openssl-dev libgcc

WORKDIR /src
RUN set -eu; \
    git init -q .; \
    git remote add origin https://github.com/OpenLEADR/openleadr-rs.git; \
    git fetch -q --depth 1 origin "${UPSTREAM_SHA}"; \
    git checkout -q FETCH_HEAD; \
    test "$(git rev-parse HEAD)" = "${UPSTREAM_SHA}"

# The queries are checked against the cached .sqlx metadata, not a live
# database. Dynamic linking of the C library, as upstream's own Dockerfile.
RUN SQLX_OFFLINE=true RUSTFLAGS="-Ctarget-feature=-crt-static" \
    cargo build --release --locked --bin openleadr-vtn --features internal-oauth

COPY adapters/openleadr/collect-licenses.sh /collect-licenses.sh
RUN set -eu; \
    mkdir -p /out/licenses/openleadr /out/licenses/rust-std; \
    cp LICENSE-APACHE LICENSE-MIT COPYRIGHT /out/licenses/openleadr/; \
    sh /collect-licenses.sh /out/licenses/crates; \
    doc="$(rustc --print sysroot)/share/doc/rust"; \
    cp -R "${doc}/COPYRIGHT-library.html" "${doc}/licenses" /out/licenses/rust-std/ 2>/dev/null \
        || cp "${doc}"/COPYRIGHT* /out/licenses/rust-std/; \
    cp target/release/openleadr-vtn /out/openleadr-vtn; \
    printf '{"adapterVersion":"%s","upstreamVersion":"%s","upstreamRevision":"%s"}\n' \
        "${ADAPTER_VERSION}" "${UPSTREAM_VERSION}" "$(git rev-parse HEAD)" > /out/build-info.json

# ---------------------------------------------------------------------------
# Stage 2: runtime. The VTN, a PostgreSQL for it, and the adapter (Python,
# standard library only) that runs both and loads the fixture.
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/alpine:3.22@${ALPINE_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev

RUN set -eu; \
    apk add --no-cache libgcc libssl3 postgresql17 postgresql17-client python3 argon2; \
    adduser -u 10001 -D -H -s /sbin/nologin interop; \
    mkdir -p /run/openadr-interop /licenses/alpine; \
    chown interop /run/openadr-interop; \
    apk info -v | sort > /licenses/alpine/PACKAGES.txt; \
    python3 -c 'import sys; assert sys.version_info >= (3, 11)'

COPY --from=build /out/openleadr-vtn /usr/local/bin/openleadr-vtn
COPY --from=build /out/licenses /licenses
COPY --from=build /out/build-info.json /opt/openadr-interop/build-info.json
COPY adapters/openleadr/openadr_interop /opt/openadr-interop/openadr_interop
COPY adapters/openleadr/openadr-interop /usr/local/bin/openadr-interop
COPY LICENSE /licenses/openadr-interop/LICENSE
COPY fixtures /fixtures

# The runtime user cannot write byte code next to the sources: compile now.
RUN python3 -m compileall -q /opt/openadr-interop && chmod 0755 /usr/local/bin/openadr-interop

ENV PATH=/usr/libexec/postgresql17:/usr/local/bin:/usr/bin:/bin \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

LABEL org.opencontainers.image.title="openadr-interop-openleadr" \
      org.opencontainers.image.description="OpenADR 3.1 reference VTN built on OpenLEADR openleadr-rs" \
      org.opencontainers.image.source="https://github.com/otfabric/openadr-interop" \
      org.opencontainers.image.licenses="MIT AND (Apache-2.0 OR MIT) AND PostgreSQL" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

USER interop
EXPOSE 8080

HEALTHCHECK --interval=2s --timeout=2s --start-period=30s --retries=15 \
  CMD test -f /run/openadr-interop/ready

ENTRYPOINT ["/usr/local/bin/openadr-interop"]
CMD ["server"]
