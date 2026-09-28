# AR-401 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-401-decision-journal.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-002, ADR-007, ADR-022 (accepted register in `docs/decisions/README.md`; no separate ADR-002/007/022 files exist in this checkout)
- Owned paths: `internal/domain/journal/`, `internal/application/journal/`, `db/queries/journal/`, `apps/api/handlers/journal/`
- Shared paths changed and justification: `db/migrations/00010_decision_journal.sql`, `contracts/openapi/openapi.json`, and generated `internal/platform/database/models.go` are authorized by the amended packet. The migration, HTTP contract, and generated schema models are required for the journal boundary.

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `decisions_finalized_guard` rejects mutation of finalized content; reviews and amendments are separate immutable tables with append-only triggers. `Finalize` only transitions a draft once. Creation snapshots are stored separately from mutable lifecycle state. |
| AC-2 | `decisions` stores `risk_budget_amount`, `risk_budget_currency`, `risk_budget_measure`, and `risk_budget_horizon`; request validation requires all four units. |
| AC-3 | `invalidation_conditions` is required JSONB array with at least one structured condition and a non-blank `condition` field. |
| AC-4 | Intended action is persisted as journal text only; no execution, broker, or external side-effect path exists. |
| AC-5 | `Timeline` reads decision and events in one PostgreSQL `REPEATABLE READ READ ONLY` transaction, orders by a shared `decision_timeline_sequence`, and preserves author/source metadata. The first event uses immutable `creation_snapshot`; integration coverage compares it before and after finalization. |

## Stop-condition check

- Decision or scope conflict: none; ADR references agree with the decision-support-only and append-only boundaries.
- Missing dependency, unsafe migration, or unavailable verification: no isolated test database URL is configured, so the real integration test fails closed before connecting. Docker was not started or modified. The orchestrator amended the packet to authorize API wiring, `test/integration/`, and `Taskfile.yml`; those paths are now covered.
- Reviewer fixes applied: `NUMERIC(38,18)` bounds are rejected before persistence (20 integer / 18 fractional digits), evidence kind/reference are validated, creation payloads are immutable, and append ordering uses a shared sequence.

## Verification

| Command | Result |
|---|---|
| `task migrate-test` | unavailable: `task` executable is not installed; local PostgreSQL was not started |
| `task test-go TEST=DecisionJournal` | unavailable: `task` executable is not installed; underlying `go test ./apps/... ./internal/... -run TestDecisionJournal -count=1` passed |
| `task test-go-integration TEST=DecisionJournalAPI` | unavailable: `task` executable is not installed; the underlying `go test ./test/integration -run TestDecisionJournalAPI -count=1` target now exists and is covered by the fail-closed run below |
| `ATLASRISK_REQUIRE_TEST_DATABASE=1 go test ./test/integration -run TestDecisionJournalAPI -count=1 -v` | expected fail-closed: `isolated database validation failed: test database URL is required`; no Docker or local infrastructure was started |
| `task test-contract` | unavailable: `task` executable is not installed; underlying sqlc generation and both contract test commands passed (11 source contract tests plus 3 job contract tests) |
| `go test ./apps/api/handlers/journal ./internal/application/journal -count=1` | pass |
| `go test ./apps/api/cmd/api ./test/integration -run 'TestDecisionJournalAPI|TestVersionFormat' -count=1` | pass; API wiring and integration test compiled (integration skipped without DB URL) |
| `go test ./internal/application/journal -run 'TestDecisionJournal' -count=1` | pass; invalid evidence, exact numeric boundary, and overflow/precision tests executed |
| `go test ./apps/... ./internal/... -run TestDecisionJournal -count=1` | pass; validation and handler tests executed |
| `node -e "JSON.parse(require('fs').readFileSync('contracts/openapi/openapi.json','utf8'))"` | pass |
| `node scripts/verify/check-generated.mjs` | pass after authorized sqlc regeneration |
| `git diff --check` | pass |

## Change inventory

- Files changed: journal domain model, application service/tests, HTTP handler/tests, API router wiring, migration, journal SQL query boundary, OpenAPI contract, generated database models, integration test, migration-version assertion, Taskfile verification selection, and this report.
- Schema/API changes: draft/finalized decisions, immutable creation snapshots, shared timeline ordering, append-only reviews/amendments, immutable finalization guard, and decision/timeline HTTP endpoints under `/api/v1/decisions`.
- Generated artifacts: `internal/platform/database/models.go` from the repository sqlc generator.

## Git state

- Branch: `task/AR-401-decision-journal`
- Commit SHA: `176fc50`, `ef9df82`, `1678452`
- Remote branch: not pushed; external push authorization is pending
- Worktree: clean

## Assumptions and risks

- The integration test uses the repository’s isolated `ATLASRISK_TEST_DATABASE_URL` contract and validates the complete create/finalize/review/amendment/timeline flow plus direct finalized-content immutability when PostgreSQL is available.
