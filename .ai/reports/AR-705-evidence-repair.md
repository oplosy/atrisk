# AR-705 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-705-evidence-repair.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-006, ADR-007, ADR-012, ADR-013, ADR-015 (accepted entries in `docs/decisions/README.md`)
- Owned paths: `internal/application/evidence/`, `.ai/reports/AR-705-evidence-repair.md`
- Shared paths changed and justification: `test/integration/decision_evidence_test.go` repairs the fixture's legacy NULL risk valuation; `test/integration/evidence_repair_test.go` adds the packet's DB regression proofs.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `TestDecisionJournalAPIRepairRejectsRiskValuationMismatch` and `TestDecisionJournalAPIRepairRejectsNullRiskValuation` prove finalization rejects mismatch/NULL atomically and leaves the decision draft. |
| AC-2 | The existing `TestHistoricalDecisionReconstruction` plus the consistent path in `TestDecisionJournalAPIRepairClosesHistoricalMetricRawDependencies` pass. |
| AC-3 | `TestDecisionJournalAPIRepairClosesHistoricalMetricRawDependencies` removes the historical raw source before sealing, restores it, seals, corrupts it during reconstruction, and confirms the original manifest bytes remain unchanged after recovery. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local `task` executable is unavailable; hosted `task verify` remains required. The local e2e test also fails before the decision flow because the isolated Garage-backed manual price import returns HTTP 500.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/application/evidence -count=1` | pass |
| `go test ./test/integration -run 'TestDecisionEvidence|TestHistoricalDecision|TestEvidenceRepair|TestDecisionJournalAPIRepair' -count=1` | pass |
| `go test ./internal/application/... ./apps/api/... -count=1` | pass |
| `go test ./test/e2e -count=1` | fail: isolated Garage-backed manual price import returned HTTP 500 (`journey_test.go:97`) |
| `task verify` | unavailable: `task` executable is not installed in the worker environment |
| `git diff --check` | pass |

## Change inventory

- Files changed: `internal/application/evidence/service.go`, `test/integration/decision_evidence_test.go`, `test/integration/evidence_repair_test.go`, this report.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-705-evidence-repair`
- Commit SHA: `9df9afd` (full SHA recorded in handoff)
- Remote branch: task branch only; hosted sync must be verified by the orchestrator
- Worktree: clean after commit

## Assumptions and risks

- The sealed risk provenance shape includes `metric_inputs.price_revision_history` and AR-706 `metric_inputs.cash_revision_history`; UUID-backed price/FX revisions must match persisted identity fields and archive bytes. Synthetic `cash-constant:<instrument-id>:<date>` entries are validated as deterministic generated inputs and have no raw object dependency.
- Hosted `task verify` and the Garage-backed e2e import failure require orchestrator follow-up before marking the packet complete.
