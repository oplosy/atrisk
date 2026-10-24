---
id: AR-705
title: Bind decisions to consistent complete risk evidence
status: ready
phase: 7
depends_on: [AR-702]
branch: task/AR-705-evidence-repair
base_sha: a4a060559e40c0773fe7eb7475b8f08b313ec1bc
owned_paths: [internal/application/evidence/, .ai/reports/AR-705-evidence-repair.md]
shared_paths: [test/integration/decision_evidence_test.go, test/integration/evidence_repair_test.go, test/e2e/]
adrs: [ADR-006, ADR-007, ADR-012, ADR-013, ADR-015]
---

# AR-705: Bind decisions to consistent complete risk evidence

## Outcome and readiness

Repair the explicitly user-authorized review findings at revision 1b555e6dcacb9113d8526c057cb6738e8c5974dd. AR-703 forbids behavior changes, so this is a separate repair packet. Accepted ADRs are in docs/decisions/README.md. No architecture decision is superseded. The orchestrator locked boundaries and failure semantics below before dispatch. Start from clean current main; do not merge the unrelated AR-703 PR to obtain helpers.

## In scope

- Findings 3 and 4: require selected risk valuation identity to match canonical selected valuation; reject NULL/legacy mismatch at new sealing.
- Traverse sealed metric price revision histories and dated FX revision paths; resolve raw objects and include them in verified manifest dependencies.
- Check revision ownership/identity against sealed provenance, deduplicate raw objects and fail closed on malformed, missing or corrupt dependencies.
- Existing immutable decision bytes/results remain untouched; no schema/API redesign. Update fixtures inventing incomplete legacy risk links.

## Out of scope

Unrelated cleanup, trading, tenancy/auth redesign, rewriting immutable history, direct main pushes.

## Acceptance criteria

- Same-snapshot different-valuation and NULL valuation fail finalization atomically.
- Consistent risk/valuation finalization remains successful.
- Missing/corrupt historical-only price or FX raw source fails seal/reconstruction; no latest fallback.
- Later revisions do not change originally sealed decision bytes.

## Required verification

```text
go test ./internal/application/evidence -count=1
go test ./test/integration -run 'TestDecisionEvidence|TestHistoricalDecision|TestEvidenceRepair' -count=1
go test ./test/e2e -count=1
task verify (hosted CI with isolated PostgreSQL/Garage)
```

Focused checks during implementation, then one complete handoff gate. Record missing local Task/dependencies explicitly and obtain hosted task verify before completion.

## Handoff evidence

Use .ai/REPORT_TEMPLATE.md. Record paths, assumptions, acceptance evidence, commands/results, full SHA, task remote sync, clean tree and risks. Writer must not spawn agents; orchestrator reviews and integrates.

