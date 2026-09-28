# AR-305 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-305-risk-result-api.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-009, ADR-011, ADR-013, ADR-014, ADR-015 (accepted register in `docs/decisions/README.md`)
- Owned paths: `internal/application/risk/`, `apps/api/handlers/risk/`, `db/queries/risk/`
- Shared paths changed and justification: `apps/api/cmd/api/main.go`, `contracts/openapi/`, `contracts/jobs/`, `contracts/generated/`, and `scripts/generate/contract-models.mjs` are explicitly authorized by the amended packet. API wiring and generator object-array support are required for the published RiskRun contract to be reachable and generated.

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/application/risk.Service.Submit` delegates to the account-scoped scenario service and preserves idempotent job semantics; handler requires the `Idempotency-Key` header and rejects a body idempotency field. |
| AC-2 | `mapStatus` distinguishes queued, running, retryable, permanent, cancelled, and completed; lifecycle tests cover status/cancel/positions routes, and malformed path or submit UUIDs return 400. |
| AC-3 | `Run` exposes account/snapshot/job IDs, scenario version/content hash, request/result hashes, schema/engine versions, input snapshots, and stored result payload. |
| AC-4 | `mapQuality` preserves degraded/blocked states and never maps them to healthy; reason codes include persisted worker errors and position causes. |
| AC-5 | `contracts/openapi/openapi.json`, generated contract models, `contracts/jobs/risk-request.golden.json`, and `contracts/jobs/contract_roundtrip_test.go` perform an actual generated Go model round-trip with object positions. |

## Stop-condition check

- Decision or scope conflict: none; the orchestrator amended the packet shared paths.
- Missing dependency, unsafe migration, or unavailable verification: PostgreSQL-backed `RiskEndToEnd` is implemented in `test/integration/risk_result_api_test.go` but locally skipped because no test database URL is configured; Docker was not started or modified.

## Verification

| Command | Result |
|---|---|
| `task test-go TEST=RiskAPI` | unavailable: `task` executable is not installed |
| `go test ./apps/api/handlers/risk ./internal/application/risk -count=1` | pass; lifecycle and malformed submit UUID tests executed |
| `task test-integration TEST=RiskEndToEnd` | unavailable: `task` executable is not installed |
| `go test ./test/integration -run TestRiskEndToEnd -count=1 -v` | test compiled; skipped with `isolated database unavailable: test database URL is required` |
| `task test-contract` | unavailable: `task` executable is not installed |
| `node --test contracts/jobs/contract.test.mjs test/contract/contract.test.mjs` | pass, 15 tests; generated Go round-trip executed by contract test |
| `task check-generated` | unavailable: `task` executable is not installed |
| `node scripts/generate/contract-models.mjs` | pass |
| `node scripts/verify/check-generated.mjs` | pass after generated outputs are staged |
| `go test ./apps/... ./internal/...` | pass |
| `go test ./apps/api/handlers/risk ./internal/application/risk` | pass |
| `git diff --check` | pass |

## Change inventory

- Files changed: risk application service/tests, risk HTTP handler/tests including submit UUID validation, real-DB `TestRiskEndToEnd`, API route wiring, OpenAPI source, contract generator object-array support and allow-list, generated contract models, risk request/result golden contracts, generated Go round-trip test, this report.
- Schema/API changes: submit, get/status, paginated positions, and cancel risk-run endpoints under `/api/v1/risk/runs`.
- Generated artifacts: `contracts/generated/go/contracts.go`, `contracts/generated/typescript/contracts.ts`, `contracts/generated/python/contracts.py`, and Python package export.

## Git state

- Branch: `task/AR-305-risk-result-api`
- Commit SHA: pending follow-up commit after UUID validation fixes
- Remote branch: `task/AR-305-risk-result-api` (follow-up push pending)
- Worktree: dirty before follow-up commit

## Assumptions and risks

- The existing AR-303 scenario service and worker persistence are the source of truth for calculation and immutable result evidence; this task does not calculate risk in Go.
- Position pagination is keyset-based on canonical `snapshot_line_id`; cursors are opaque base64url values.
- The HTTP handler is mounted for both `/api/v1` and `/v1` compatibility, matching existing API routes.
- Cancellation maps an unfinished `scenario_runs` row to terminal `failed` because the existing AR-303 database state check has no `cancelled` value; API lifecycle status remains `cancelled` from the authoritative `risk_jobs` row. Completed evidence is never updated, and cancellation locks the job before closing the scenario run to avoid worker races.
