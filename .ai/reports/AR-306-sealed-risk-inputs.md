# AR-306 Orchestrator Handoff

## Task authority

- Task packet: `.ai/tasks/AR-306-sealed-risk-inputs.md`
- Packet status: `ready`; AR-402 was merged to `main` at `bfd36bd5e49a433dbd3657548019b18a4e2a0b58`, resolving the migration-number conflict.
- Referenced ADRs: ADR-009 through ADR-015, accepted in `docs/decisions/README.md`.
- Owned paths: as listed in the task packet.
- Shared paths changed: as listed in the task packet; no task code is committed yet.

## Result

`active` (resume checkpoint; partial implementation remains unverified)

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 through AC-7 | Not yet verified; implementation is partial and no tests were run after the partial edits. |

## Stop-condition check

- Decision or scope conflict: none in accepted architecture; ADRs are present in the accepted register, not individual files.
- Prior blocking condition resolved: AR-402 is merged. The uncommitted AR-306 migration still needs renumbering after the merged `00011_decision_evidence.sql`; regenerate contracts after rebasing before verification.

## Verification

| Command | Result |
|---|---|
| `git diff --check` | pass on partial worktree |
| AR-306 test commands | not run; implementation paused before tests |

## Change inventory

- Partial uncommitted files: `apps/api/handlers/risk/handler.go`, `apps/api/handlers/risk/handler_test.go`, `contracts/openapi/openapi.json`, `db/migrations/00011_sealed_risk_inputs.sql`, `internal/application/risk/service.go`, `internal/application/scenarios/service.go`, `internal/application/scenarios/service_test.go`, `risk-engine/src/atlasrisk/jobs/postgres.py`, `risk-engine/src/atlasrisk/jobs/scenario.py`.
- Schema/API changes: incomplete; migration must be renumbered after AR-402 integration, and contracts regenerated against the updated base.
- Generated artifacts: not yet updated.

## Git state

- Branch: `task/AR-306-sealed-risk-inputs`
- Last commit: `5e33dd2` task packet/plan only, based on `0277e082843b6caa8f16e95e96bac06ba4d4a1a4`; the partial implementation and resumption note are now awaiting a recovery checkpoint and rebase.
- Remote branch: not pushed.
- Worktree: dirty by design; preserve partial edits for resumption.

## Assumptions and risks

- Rebase this task on the updated `main`, renumber its migration after all merged migrations, regenerate contracts, and then rerun the complete packet verification.
- Docker was not run or reconfigured.
