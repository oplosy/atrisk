---
id: AR-704
title: Repair API perimeter and batch CSV commits
status: ready
phase: 7
depends_on: [AR-702]
branch: task/AR-704-api-import-repair
base_sha: a4a060559e40c0773fe7eb7475b8f08b313ec1bc
owned_paths: [apps/api/cmd/api/, internal/imports/]
shared_paths: [docs/releases/PUBLISHING.md, infra/images/api.Dockerfile, test/integration/api_import_repair_test.go]
adrs: [ADR-001, ADR-009, ADR-010, ADR-016]
---

# AR-704: Repair API perimeter and batch CSV commits

## Outcome and readiness

Repair the explicitly user-authorized review findings at revision 1b555e6dcacb9113d8526c057cb6738e8c5974dd. AR-703 forbids behavior changes, so this is a separate repair packet. Accepted ADRs are in docs/decisions/README.md. No architecture decision is superseded. The orchestrator locked boundaries and failure semantics below before dispatch. Start from clean current main; do not merge the unrelated AR-703 PR to obtain helpers.

## In scope

- Findings 1, 9, 11: loopback default; finite HTTP body/read and idle deadlines; set-based CSV ownership/instrument validation and bulk position/price inserts.
- Preserve explicit -listen overrides, route aliases, byte/row caps, preview binding, idempotency, exact decimals and atomic failure.
- Container entrypoint must explicitly bind its intended backend interface; remote exposure still requires external TLS/OIDC. Document this distinction.
- Avoid rate-limit/auth redesign and dependency changes.

## Out of scope

Unrelated cleanup, trading, tenancy/auth redesign, rewriting immutable history, direct main pushes.

## Acceptance criteria

- Default listener is loopback; explicit container/network override remains functional.
- Stalled/slow body terminates within a configured deadline; legitimate JSON/multipart inputs remain accepted.
- Large position/manual-price imports use bounded bulk operations, preserve ownership checks, duplicate rejection and atomic rollback.
- Tests cover unknown/cross-portfolio IDs and exact decimals; include statement-count or benchmark evidence.

## Required verification

```text
go test ./apps/api/cmd/api ./internal/imports -count=1
go test ./test/integration -run 'TestAPIImportRepair|TestManualCSV' -count=1
task verify (hosted CI with isolated PostgreSQL/Garage)
```

Focused checks during implementation, then one complete handoff gate. Record missing local Task/dependencies explicitly and obtain hosted task verify before completion.

## Handoff evidence

Use .ai/REPORT_TEMPLATE.md. Record paths, assumptions, acceptance evidence, commands/results, full SHA, task remote sync, clean tree and risks. Writer must not spawn agents; orchestrator reviews and integrates.

