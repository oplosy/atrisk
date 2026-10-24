# syntax=docker/dockerfile:1
# AtlasRisk collector: durable PostgreSQL schedule worker. A local JSON
# schedule file is supplied at runtime with --config.
# Build from the repository root: docker build -f infra/images/collector.Dockerfile .

FROM golang:1.27.0-trixie@sha256:df98008ecd2b0ecc9f0a94d1b07e3564a9c92b555369b33d9b5f60d0765b2db7 AS build
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/collector ./apps/collector
COPY internal ./internal
ARG VERSION=dev
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/atlasrisk-collector ./apps/collector/cmd/collector

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/oplosy/atrisk" \
      org.opencontainers.image.title="atrisk-collector" \
      org.opencontainers.image.version="${VERSION}"
WORKDIR /app
COPY --from=build /out/atlasrisk-collector /app/atlasrisk-collector
USER 65532:65532
ENTRYPOINT ["/app/atlasrisk-collector"]
