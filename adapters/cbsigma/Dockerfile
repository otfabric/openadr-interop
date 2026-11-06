# SPDX-License-Identifier: MIT
#
# openadr-interop adapter for cB-Sigma openadr (OpenADR 3, Go): its VTN
# (oadr3/vtn) as the server role and its REST client (oadr3.APIClient) as the
# Business Logic and VEN clients.
#
# Build from the repository root:
#   docker build -f adapters/cbsigma/Dockerfile -t openadr-interop-cbsigma:dev .
#
# cB-Sigma openadr is Apache-2.0. The adapter is MIT and links it statically;
# the image carries its LICENSE and NOTICE under /licenses. The pin is below,
# in go.mod (with its checksum in go.sum) and in versions.yaml.

# Where the base images come from. They are pinned by digest, so a mirror of
# Docker Hub serves exactly the same images.
ARG BASE_REGISTRY=docker.io/library
# golang:1.27.2-trixie and debian:trixie-slim, multi-arch indexes
# (linux/amd64 and linux/arm64).
ARG GOLANG_DIGEST=sha256:47667934874913a449b96faa7ef4777fa285d9ec29126ee80ed61c45b7cccf0e
ARG DEBIAN_DIGEST=sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2

# ---------------------------------------------------------------------------
# Stage 1: build the adapter (the upstream module linked in)
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/golang:1.27.2-trixie@${GOLANG_DIGEST} AS build

# The release pin. "make candidate-cbsigma CANDIDATE_VERSION=<version>"
# overrides it to try another upstream version without touching the pin.
ARG UPSTREAM_VERSION=v0.2.0
# The commit of the release tag, for print-capabilities.
ARG UPSTREAM_SHA=ed8241708939c35b201acf1fd1e768b2d551107f
ARG ADAPTER_VERSION=dev

ENV CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local
WORKDIR /src
COPY adapters/cbsigma/go.mod adapters/cbsigma/go.sum ./

# At the pin the module must match go.sum; anything else is a candidate.
RUN if [ "$(go list -m -f '{{.Version}}' github.com/cB-Sigma/openadr)" = "${UPSTREAM_VERSION}" ]; then \
         GOFLAGS=-mod=readonly go mod download && go mod verify; \
       else \
         go get "github.com/cB-Sigma/openadr@${UPSTREAM_VERSION}"; \
       fi

COPY adapters/cbsigma/*.go ./
# The tests read the fixtures where the image keeps them.
COPY fixtures /fixtures
RUN test -z "$(gofmt -l .)" \
    && go vet ./... \
    && go test ./... \
    && go build -trimpath -ldflags "-s -w \
         -X main.adapterVersion=${ADAPTER_VERSION} \
         -X main.upstreamVersion=$(go list -m -f '{{.Version}}' github.com/cB-Sigma/openadr) \
         -X main.upstreamRevision=${UPSTREAM_SHA}" \
         -o /out/openadr-interop . \
    && test "$(go version -m /out/openadr-interop | awk '$1 == "dep" { print $2 }')" = "github.com/cB-Sigma/openadr" \
    && mkdir -p /out/licenses/openadr /out/licenses/go \
    && cp "$(go list -m -f '{{.Dir}}' github.com/cB-Sigma/openadr)/LICENSE" \
          "$(go list -m -f '{{.Dir}}' github.com/cB-Sigma/openadr)/NOTICE" /out/licenses/openadr/ \
    && cp "$(go env GOROOT)/LICENSE" /out/licenses/go/LICENSE

# ---------------------------------------------------------------------------
# Stage 2: runtime
# ---------------------------------------------------------------------------
FROM ${BASE_REGISTRY}/debian:trixie-slim@${DEBIAN_DIGEST} AS runtime

ARG ADAPTER_VERSION=dev

RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin interop \
    && mkdir -p /run/openadr-interop \
    && chown interop /run/openadr-interop

COPY --from=build /out/openadr-interop /usr/local/bin/openadr-interop
COPY --from=build /out/licenses /licenses
COPY LICENSE /licenses/openadr-interop/LICENSE
COPY THIRD_PARTY_NOTICES.md /licenses/THIRD_PARTY_NOTICES.md
COPY fixtures /fixtures

LABEL org.opencontainers.image.title="openadr-interop-cbsigma" \
      org.opencontainers.image.description="OpenADR 3 reference VTN and clients built on cB-Sigma openadr" \
      org.opencontainers.image.source="https://github.com/otfabric/openadr-interop" \
      org.opencontainers.image.licenses="MIT AND Apache-2.0" \
      org.opencontainers.image.version="${ADAPTER_VERSION}"

USER interop
EXPOSE 8080

HEALTHCHECK --interval=2s --timeout=2s --start-period=10s --retries=15 \
  CMD test -f /run/openadr-interop/ready

ENTRYPOINT ["/usr/local/bin/openadr-interop"]
CMD ["server"]
