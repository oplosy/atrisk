#!/usr/bin/env bash
set -Eeuo pipefail

export ATLASRISK_REQUIRE_TEST_DATABASE=1
: "${ATLASRISK_TEST_DATABASE_URL:?ATLASRISK_TEST_DATABASE_URL is required; migrations must not be skipped}"

go test ./test/integration -run '^TestCoreDatabase(Migrations|PreviousVersionUpgrade)$' -count=1
echo "migration-from-previous-release verification passed"
