#!/usr/bin/env bash
# Smoke-tests the release images against a disposable PostgreSQL database:
# non-root users, migrate (twice), the API answering a query, and the worker
# starting and stopping cleanly. Containers run the way an installation runs
# them: read-only root filesystem, no capabilities, no new privileges.
#
#   API_IMAGE=... WORKER_IMAGE=... ATLASRISK_DATABASE_URL=... \
#     bash scripts/release/smoke-images.sh
#
# The database must be disposable and reachable from the host network.
set -euo pipefail

: "${API_IMAGE:?API_IMAGE is required}"
: "${WORKER_IMAGE:?WORKER_IMAGE is required}"
: "${ATLASRISK_DATABASE_URL:?ATLASRISK_DATABASE_URL is required}"
api_port=${SMOKE_API_PORT:-18080}
api_name=atrisk-smoke-api
worker_name=atrisk-smoke-worker

opts=(--network host --read-only --cap-drop ALL --security-opt no-new-privileges
      -e ATLASRISK_DATABASE_URL)

cleanup() { docker rm --force "$api_name" "$worker_name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

fail() { echo "smoke: $*" >&2; exit 1; }

for image in "$API_IMAGE" "$WORKER_IMAGE"; do
  user=$(docker image inspect --format '{{.Config.User}}' "$image")
  case ${user%%:*} in
    "" | 0 | root) fail "$image runs as root (user '$user')" ;;
  esac
done

docker run --rm "$API_IMAGE" --version | grep -q '^atlasrisk api version ' \
  || fail "API image does not report its version"
docker run --rm "$WORKER_IMAGE" --version | grep -q '^atlasrisk risk-engine version ' \
  || fail "worker image does not report its version"

echo "smoke: migrate"
docker run --rm "${opts[@]}" "$API_IMAGE" migrate
second=$(docker run --rm "${opts[@]}" "$API_IMAGE" migrate 2>&1) || fail "second migrate failed: $second"
grep -q 'no migrations to run' <<<"$second" || fail "second migrate was not a no-op: $second"

echo "smoke: api"
docker run --detach --name "$api_name" "${opts[@]}" "$API_IMAGE" -listen "127.0.0.1:$api_port" >/dev/null
for _ in $(seq 50); do
  code=$(curl --silent --output /dev/null --write-out '%{http_code}' \
    "http://127.0.0.1:$api_port/api/v1/instruments" || true)
  [[ $code == 200 ]] && break
  sleep 0.2
done
[[ $code == 200 ]] || { docker logs "$api_name" >&2; fail "API answered HTTP $code"; }

echo "smoke: worker"
docker run --detach --name "$worker_name" "${opts[@]}" "$WORKER_IMAGE" >/dev/null
sleep 5
[[ $(docker inspect --format '{{.State.Running}}' "$worker_name") == true ]] \
  || { docker logs "$worker_name" >&2; fail "worker exited early"; }
logs=$(docker logs "$worker_name" 2>&1)
grep -q '"event": "worker started"' <<<"$logs" || fail "worker did not start: $logs"
if grep -q '"level": "\(warning\|error\)"' <<<"$logs"; then fail "worker reported problems: $logs"; fi
docker stop --time 20 "$worker_name" >/dev/null
[[ $(docker inspect --format '{{.State.ExitCode}}' "$worker_name") == 0 ]] \
  || { docker logs "$worker_name" >&2; fail "worker did not stop cleanly"; }
grep -q '"event": "worker stopped"' <<<"$(docker logs "$worker_name" 2>&1)" \
  || fail "worker did not log its shutdown"

echo "smoke: passed"
