# AR-402 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-402-decision-evidence.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-006, ADR-007, ADR-013, ADR-015 in the accepted `docs/decisions/README.md` register
- Owned paths: `internal/application/evidence/`, `db/queries/evidence/`, `apps/api/handlers/evidence/`
- Shared paths changed and justification: migration 11 adds the immutable manifest and completed-risk-result guard; journal service and handler attach finalization/reconstruction; API wiring passes the configured archive; OpenAPI documents the endpoint and reference semantics; integration tests and migration-version assertion prove behavior.

## Result

`needs-review` (hosted isolated-PostgreSQL verification pending)

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `Service.Finalize` locks a draft and seals evidence in the same transaction. `Seal` rejects absent/foreign snapshots, blocked valuations, unfinished risk runs, missing engine versions, and unavailable raw archive objects. Integration test checks a queued result and missing archive leave the decision draft. |
| AC-2 | Manifest stores the exact snapshot, valuation/quote path, scenario version, risk job/result, and selected revision rows, not latest projections. Integration fixture inserts later snapshot and scenario version and compares bytes. |
| AC-3 | Manifest includes contract version, every typed reference and its row data, scenario/engine/schema versions from the completed run, and a SHA-256 over canonical Go JSON bytes. The public `DecisionEvidence` response is now generated in Go, TypeScript, and Python; contract regression coverage verifies all three targets. |
| AC-4 | Reconstruction verifies the stored hash and every archived raw object by key and content hash; missing objects yield `EVIDENCE_INTEGRITY_FAILURE` rather than recomputation. |
| AC-5 | `TestHistoricalDecisionReconstruction` compares exact manifest bytes and hash after later inserts; isolated PostgreSQL execution is pending hosted CI. |

## Stop-condition check

- Decision or scope conflict: none. Referenced ADRs are accepted register entries; individual ADR-006/007/013/015 files do not exist.
- Missing dependency or unavailable verification: `task` executable and isolated PostgreSQL URL are unavailable locally. Docker was not started or reconfigured. Hosted PR CI must run `task verify` with its isolated database.

## Verification

| Command | Result |
|---|---|
| `task test-go TEST=DecisionEvidence` | `task` unavailable; `go test ./internal/application/evidence ./internal/application/journal ./apps/api/handlers/journal -count=1` passed. |
| `task test-integration TEST=HistoricalDecisionReconstruction` | `task` unavailable; direct test compiled and skipped without isolated DB URL. Hosted execution pending. |
| `task test-contract` | `task` unavailable; `node scripts/generate/contract-models.mjs` and both Node contract suites passed (11 + 4 tests). |
| `task migrate-test` | unavailable locally without isolated PostgreSQL URL; hosted execution pending. |
| `node scripts/verify/check-generated.mjs` | passed after committing generated outputs. |
| `git diff --check` | passed. |

## Change inventory

- Files changed: evidence service/tests, journal service/handler/tests, API wiring, decision journal integration fixture, historical reconstruction integration test, migration, migration-version assertion, OpenAPI, and this report.
- Schema/API changes: `decision_evidence` immutable manifest with canonical bytes and hash; completed risk results protected from mutation; `GET /api/v1/decisions/{decision_id}/evidence`.
- Generated artifacts: `DecisionEvidence` added to Go, TypeScript, Python, and Python package exports by the registered contract generator.

## Git state

- Branch: `task/AR-402-decision-evidence`
- Implementation commit SHA: `a43c6f9` (`fix(evidence): complete decision evidence contract models [AR-402]`)
- Report update commit SHA: recorded in the final handoff after this report commit.
- Remote branch: pending
- Worktree: dirty until commit

## Assumptions and risks

- Finalization requires exactly one account-linked portfolio snapshot, one completed valuation run, and one completed scenario/risk run on the same snapshot. Optional typed revision/raw-object references can bind additional market or macro evidence. This is stricter than AR-401 draft creation, which permits narrative references before finalization.
- Without an archive adapter, a decision involving raw source evidence fails closed. Hosted CI must validate PostgreSQL queries, migration upgrade, and golden reconstruction; local compilation is not runtime proof.
- Evidence UUID validation accepts both lowercase and uppercase hexadecimal digits; regression coverage is in `internal/application/evidence/service_test.go`.
