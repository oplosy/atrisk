#!/usr/bin/env bash
set -Eeuo pipefail

go test ./test/integration -run '^TestCoreDatabase(Migrations|PreviousVersionUpgrade)$' -count=1
echo "migration-from-previous-release verification passed"
