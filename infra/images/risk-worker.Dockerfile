# syntax=docker/dockerfile:1
# AtlasRisk risk worker: claims jobs from the PostgreSQL queue at
# ATLASRISK_DATABASE_URL until stopped.
# Build from the repository root: docker build -f infra/images/risk-worker.Dockerfile .

FROM ghcr.io/astral-sh/uv:0.12.8@sha256:d1cbaeadc234fe19c0d93daabcf5e98738cd93c6d1dd4918ef6aa30735feb23a AS uv

FROM python:3.14.7-slim-trixie@sha256:51dafde81dbdb6ebde285137a295cf18a47ca95234fe388a343719cb97305b3d AS build
COPY --from=uv /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never \
    UV_PYTHON=/usr/local/bin/python3.14 UV_PROJECT_ENVIRONMENT=/opt/atlasrisk
WORKDIR /src
COPY risk-engine/pyproject.toml risk-engine/uv.lock risk-engine/README.md ./
RUN uv sync --locked --no-dev --extra worker --no-install-project
COPY risk-engine/src ./src
RUN uv sync --locked --no-dev --extra worker --no-editable

FROM python:3.14.7-slim-trixie@sha256:51dafde81dbdb6ebde285137a295cf18a47ca95234fe388a343719cb97305b3d
ARG VERSION=dev
LABEL org.opencontainers.image.source="https://github.com/oplosy/atrisk" \
      org.opencontainers.image.title="atrisk-risk-worker" \
      org.opencontainers.image.version="${VERSION}"
RUN useradd --system --uid 10001 --user-group --no-create-home \
      --shell /usr/sbin/nologin atlasrisk
COPY --from=build /opt/atlasrisk /opt/atlasrisk
ENV PATH=/opt/atlasrisk/bin:$PATH PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
USER 10001:10001
ENTRYPOINT ["atlasrisk-risk-worker"]
