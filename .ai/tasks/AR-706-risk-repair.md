---
id: AR-706
title: Recover risk leases and support bounded cash metrics
status: ready
phase: 7
depends_on: [AR-702]
branch: task/AR-706-risk-repair
base_sha: a4a060559e40c0773fe7eb7475b8f08b313ec1bc
owned_paths: [internal/application/scenarios/, internal/jobs/, risk-engine/src/atlasrisk/jobs/, risk-engine/src/atlasrisk/metrics/, risk-engine/src/atlasrisk/scenarios/, risk-engine/tests/, .ai/reports/AR-706-risk-repair.md]
shared_paths: [contracts/openapi/, contracts/jobs/, contracts/generated/, test/integration/risk_repair_test.go, test/integration/scenario_provenance_test.go, docs/architecture/DATA_AND_RISK_MODEL.md]
adrs: [ADR-008, ADR-009, ADR-010, ADR-011, ADR-012, ADR-013, ADR-015]
---

# AR-706: Recover risk leases and support bounded cash metrics

## Outcome and readiness

Repair the explicitly user-authorized review findings at revision 1b555e6dcacb9113d8526c057cb6738e8c5974dd. AR-703 forbids behavior changes, so this is a separate repair packet. Accepted ADRs are in docs/decisions/README.md. No architecture decision is superseded. The orchestrator locked boundaries and failure semantics below before dispatch. Start from clean current main; do not merge the unrelated AR-703 PR to obtain helpers.

## In scope

- Findings 2, 5, 10, 12: expired-job recovery; explicit stale-completion handling; terminal scenario state consistency; supported cash history; decimal-string shock validation; bounded correlation workload.
- Python worker owns poll-loop recovery. Go recovery mirrors terminal scenario behavior. Long work must safely renew leases or be explicitly bounded; stale owners cannot commit.
- Cash history uses constant native units and point-in-time dated FX to USD for foreign cash; USD cash is constant. Include cash NAV/exposure, no fabricated FX or tradable-price forward fill. Zero cash volatility is valid; undefined cash correlations remain explicit and cannot alone block a supported mixed portfolio.
- Decimal strings remain exact; validate accepted shock shapes/types/finite domain/storage bounds synchronously before any enqueue/version.
- Preserve pairwise alignment/missing-date semantics. Reuse returns only when equivalent; enforce documented deterministic instrument/pair budgets before heavy work with explicit blocked quality/reason. Never silently drop coverage.
- Preserve prior immutable results and version risk outputs when numerical behavior changes. No new metrics/formulas/auth/dependencies/migrations.

## Out of scope

Unrelated cleanup, trading, tenancy/auth redesign, rewriting immutable history, direct main pushes.

## Acceptance criteria

- Crash/restart, expired lease, renewal/lost-owner completion and max-attempt scenarios have deterministic lifecycle tests.
- Cash-only USD and mixed cash/spot scenarios work; foreign cash uses dated FX, missing FX blocks.
- Numeric decimal shocks/oversized values fail atomically before enqueue; decimal strings/templates succeed.
- Metric goldens preserve tolerances and missing-date/calendar semantics; capacity boundary covered and measured.
- Update public schema and regenerate types with scripts, never hand-edit generated files.

## Required verification

```text
go test ./internal/application/scenarios ./internal/jobs -count=1
uv run --project risk-engine --locked pytest risk-engine/tests -q
uv run --project risk-engine --locked ruff check risk-engine/src risk-engine/tests
node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs
go test ./test/integration -run 'TestRiskRepair|TestScenarioInputProvenance|TestRiskJobLifecycle|TestRiskEndToEnd' -count=1
task verify (hosted CI with isolated PostgreSQL/Garage)
```

Focused checks during implementation, then one complete handoff gate. Record missing local Task/dependencies explicitly and obtain hosted task verify before completion.

## Handoff evidence

Use .ai/REPORT_TEMPLATE.md. Record paths, assumptions, acceptance evidence, commands/results, full SHA, task remote sync, clean tree and risks. Writer must not spawn agents; orchestrator reviews and integrates.

