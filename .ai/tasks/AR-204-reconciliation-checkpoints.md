---
id: AR-204
title: Add valuation reconciliation checkpoints
status: ready
phase: 2
depends_on: [AR-203]
branch: task/AR-204-reconciliation-checkpoints
base_sha: 964384c28d224420d8dcf62965967425bc53fc39
owned_paths: [internal/domain/reconciliation/, internal/application/reconciliation/, db/queries/reconciliation/, apps/api/handlers/reconciliation/]
shared_paths: [db/migrations/, contracts/openapi/]
adrs: [ADR-010, ADR-012]
---

# AR-204: Add valuation reconciliation checkpoints

## Outcome

Users can bind an externally stated NAV to a valuation, see exact absolute/relative
differences, and receive a deterministic reconciled/unreconciled result.

## In scope

- Immutable checkpoints bound to one completed valuation, including its account,
  source label, currency, cutoff, external NAV, optional line checks, effective
  tolerance version, exact differences, reconciliation state, and API
  representation.

## Out of scope

- Broker connectivity, automatic correction, or accounting-ledger reconciliation.

## Acceptance criteria

- [ ] A checkpoint references one completed immutable valuation and preserves
  its account, source label, reporting currency, cutoff, external NAV, and any
  supplied line checks; a blocked or incomplete valuation cannot be reconciled.
- [ ] Absolute difference is `abs(external_nav - valuation_nav)`; relative
  difference is absolute difference divided by `abs(external_nav)`, or null when
  external NAV is zero. Reconciliation is inclusive at the effective tolerance.
- [ ] Default tolerance is the greater of `0.01` base-currency units and one
  basis point of absolute external NAV. An explicit account tolerance is exact,
  non-negative, versioned, and visible with the result.
- [ ] Currency or cutoff mismatch blocks comparison with a stable reason code;
  no implicit FX conversion or cutoff substitution occurs.
- [ ] Difference arithmetic and optional line-check totals use exact decimals;
  when a complete line-check set is supplied, its totals reconcile to the
  checkpoint NAV at the stored scale.
- [ ] Checkpoint creation is append-only: later checkpoints and attempts to
  update an earlier checkpoint cannot change its inputs, tolerance version, or
  result.
- [ ] API contract/generated types stay synchronized; PostgreSQL integration
  tests exercise the HTTP endpoint and prove persisted checkpoint provenance.

## Required verification

```text
task test-go TEST=Reconciliation
task test-go-integration TEST=ReconciliationAPI
task test-contract
task migrate-test
task verify
```
