# AR-306 Orchestrator Handoff

## Task authority

- Task packet: `.ai/tasks/AR-306-sealed-risk-inputs.md`
- Packet status: `ready`; AR-402 was merged to `main` at `bfd36bd5e49a433dbd3657548019b18a4e2a0b58`, resolving the migration-number conflict.
- Referenced ADRs: ADR-009 through ADR-015, accepted in `docs/decisions/README.md`.
- Owned paths: as listed in the task packet.
- Shared paths changed: migration, OpenAPI contract, and task/report handoff paths as listed in the task packet.

## Result

`active` (resume checkpoint; partial implementation remains unverified)

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 through AC-7 | Not yet verified; implementation is partial and no tests were run after the partial edits. |

## Stop-condition check

- Decision or scope conflict: none in accepted architecture; ADRs are present in the accepted register, not individual files.
- Prior blocking condition resolved: AR-402 is merged. The checkpointed AR-306 migration still needs renumbering to `00012` after merged `00011_decision_evidence.sql`; regenerate contracts after rebasing before verification.

## Verification

| Command | Result |
|---|---|
| `git diff --check` | pass on partial worktree |
| AR-306 test commands | not run; implementation paused before tests |

## Change inventory

- Checkpointed partial files: `apps/api/handlers/risk/handler.go`, `apps/api/handlers/risk/handler_test.go`, `contracts/openapi/openapi.json`, `db/migrations/00011_sealed_risk_inputs.sql` (must be renumbered), `internal/application/risk/service.go`, `internal/application/scenarios/service.go`, `internal/application/scenarios/service_test.go`, `risk-engine/src/atlasrisk/jobs/postgres.py`, and `risk-engine/src/atlasrisk/jobs/scenario.py`.
- Schema/API changes: incomplete; migration numbering and contract regeneration must be completed on the updated base.
- Generated artifacts: not yet updated.

## Git state

- Branch: `task/AR-306-sealed-risk-inputs`
- Last commit: `f09936f` recovery checkpoint, rebased on `bfd36bd5e49a433dbd3657548019b18a4e2a0b58`.
- Remote branch: not pushed.
- Worktree: clean at the recovery checkpoint; partial code remains unverified.

## Assumptions and risks

- Renumber the migration, regenerate contracts, and then rerun the complete packet verification.
- Docker was not run or reconfigured.
