#!/usr/bin/env bash
# Builds and pushes both release images for TAG, then records each pushed
# image by digest in IMAGES_FILE (default images.txt), one per line:
#   ghcr.io/<owner>/atrisk-api:v1.2.3@sha256:...
# Requires a prior `docker login` to REGISTRY's host.
set -euo pipefail

: "${TAG:?TAG is required}"
: "${REGISTRY:?REGISTRY is required, for example ghcr.io/owner}"
[[ $TAG =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "release tag must look like v1.2.3, got $TAG" >&2; exit 2; }
out=${IMAGES_FILE:-images.txt}
revision=$(git rev-parse HEAD)
: > "$out"

publish() {  # name dockerfile
  local ref="$REGISTRY/$1:$TAG" digest
  docker build --file "$2" --build-arg VERSION="$TAG" \
    --label org.opencontainers.image.revision="$revision" --tag "$ref" .
  docker run --rm "$ref" --version
  digest=$(docker push "$ref" | tee /dev/stderr | awk '/digest: sha256:/ {print $3}' | tail -n 1)
  [[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "no digest reported for $ref" >&2; exit 1; }
  echo "$ref@$digest" >> "$out"
}

publish atrisk-api infra/images/api.Dockerfile
publish atrisk-risk-worker infra/images/risk-worker.Dockerfile
cat "$out"
