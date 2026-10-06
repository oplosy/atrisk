# syntax=docker/dockerfile:1
# AtlasRisk API. `atlasrisk-api` serves HTTP; `atlasrisk-api migrate` applies
# the bundled db/migrations to ATLASRISK_DATABASE_URL and exits.
# Build from the repository root: docker build -f infra/images/api.Dockerfile .

FROM golang:1.27.0-trixie@sha256:df98008ecd2b0ecc9f0a94d1b07e3564a9c92b555369b33d9b5f60d0765b2db7 AS build
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/api ./apps/api
COPY internal ./internal
ARG VERSION=dev
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/atlasrisk-api ./apps/api/cmd/api

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/oplosy/atrisk" \
      org.opencontainers.image.title="atrisk-api" \
      org.opencontainers.image.version="${VERSION}"
WORKDIR /app
COPY --from=build /out/atlasrisk-api /app/atlasrisk-api
COPY db/migrations /app/db/migrations
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/atlasrisk-api"]
