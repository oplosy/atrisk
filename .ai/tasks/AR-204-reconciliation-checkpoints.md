---
id: AR-204
title: Add valuation reconciliation checkpoints
status: draft
phase: 2
depends_on: [AR-203]
branch: task/AR-204-reconciliation-checkpoints
owned_paths: [internal/domain/reconciliation/, internal/application/reconciliation/, db/queries/reconciliation/, apps/api/handlers/reconciliation/]
shared_paths: [db/migrations/, contracts/openapi/]
adrs: [ADR-010, ADR-012]
---

# AR-204: Add valuation reconciliation checkpoints

## Outcome

Users can bind an externally stated NAV to a valuation, see exact absolute/relative
differences, and receive a deterministic reconciled/unreconciled result.

## In scope

- Immutable checkpoints, source label, currency, cutoff, external NAV, optional
  line checks, default/account tolerance, and API representation.

## Out of scope

- Broker connectivity, automatic correction, or accounting-ledger reconciliation.

## Acceptance criteria

- [ ] Default tolerance is max(0.01 base units, one basis point of external NAV).
- [ ] Explicit account tolerance is versioned and visible on the result.
- [ ] Currency/cutoff mismatch blocks comparison.
- [ ] Difference arithmetic uses exact decimals and line totals reconcile.
- [ ] A later checkpoint does not mutate the earlier checkpoint.

## Required verification

```text
task test-go TEST=Reconciliation
task test-go-integration TEST=ReconciliationAPI
task test-contract
```
