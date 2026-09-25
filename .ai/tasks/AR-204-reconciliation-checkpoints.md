---
id: AR-204
title: Add valuation reconciliation checkpoints
status: ready
phase: 2
depends_on: [AR-203]
branch: task/AR-204-reconciliation-checkpoints
base_sha: 964384c28d224420d8dcf62965967425bc53fc39
owned_paths: [internal/domain/reconciliation/, internal/application/reconciliation/, db/queries/reconciliation/, apps/api/handlers/reconciliation/]
shared_paths: [db/migrations/, contracts/openapi/, apps/api/cmd/api/]
adrs: [ADR-010, ADR-011, ADR-012, ADR-025]
---

# AR-204: Add valuation reconciliation checkpoints

## Outcome

Users can bind an externally stated NAV to a valuation, see exact absolute/relative
differences, and receive a deterministic reconciled/unreconciled result.

## In scope

- Immutable checkpoints bound to one valid valuation and one account from its
  source snapshot, including source label, currency, cutoff, external NAV,
  optional line checks, effective tolerance version, exact differences,
  reconciliation state, and API representation.

## Out of scope

- Broker connectivity, automatic correction, or accounting-ledger reconciliation.

## Acceptance criteria

- [ ] A checkpoint references one immutable valuation and account from its
  snapshot, preserves source label, reporting currency, exact UTC cutoff,
  external NAV, and supplied line checks; only `valid` valuations with at least
  one line for that account can be reconciled.
- [ ] The account NAV is the exact sum of its persisted valuation-line TRY or
  USD amounts. No valuation rerun, other currency, or implicit FX conversion is
  used.
- [ ] Absolute difference is `abs(external_nav - valuation_nav)`; relative
  difference is absolute difference divided by `abs(external_nav)`, or null when
  external NAV is zero. Reconciliation is inclusive at the effective tolerance.
- [ ] Default tolerance is the greater of `0.01` reporting-currency units and
  one basis point of absolute external NAV. An explicit account tolerance is a
  non-negative NUMERIC(38,18) append-only versioned absolute amount. Its
  endpoint serializes version creation per account and enforces unique
  `(account_id, version)`; concurrent
  integration tests prove no duplicate/lost versions. Checkpoint creation
  serializes on the account and snapshots the latest committed version in that
  transaction, or version 0 and the default formula when no override exists.
- [ ] Currency or cutoff mismatch blocks comparison with a stable reason code;
  no implicit FX conversion or cutoff substitution occurs.
- [ ] Optional line checks identify distinct snapshot lines belonging to the
  account; each external amount is inherently in the checkpoint currency.
  Unknown, duplicate, and cross-account checks are rejected. With
  `line_checks_complete=false`, a supplied subset returns 201 with
  `line_check_state=partial`; with it true, any missing account line returns
  409 `LINE_CHECK_INCOMPLETE`, while a full set that sums exactly to external
  NAV returns `line_check_state=complete`. No checks returns `none`. Exact
  per-line differences are returned without replacing the account-level NAV
  status.
- [ ] Checkpoint creation is append-only: later checkpoints and attempts to
  update an earlier checkpoint cannot change its inputs, tolerance version, or
  result.
- [ ] Contract-first `POST /api/v1/accounts/{account_id}/reconciliation-tolerances`,
  `POST /api/v1/valuations/{valuation_id}/reconciliations`, and
  `GET /api/v1/reconciliations/{reconciliation_id}` APIs create a tolerance
  version, create an immutable checkpoint, and retrieve it. Responses expose
  valuation/account IDs, NAV currency/cutoff, exact differences, effective
  tolerance/version, state (`reconciled` or `unreconciled`), and line-check
  evidence. Stable conflict codes are `VALUATION_NOT_VALID`,
  `ACCOUNT_NOT_IN_SNAPSHOT`, `ACCOUNT_HAS_NO_LINES`, `CURRENCY_MISMATCH`,
  `CUTOFF_MISMATCH`, `LINE_CHECK_UNKNOWN`, `LINE_CHECK_DUPLICATE`,
  `LINE_CHECK_CROSS_ACCOUNT`, `LINE_CHECK_INCOMPLETE`, and
  `LINE_CHECK_TOTAL_MISMATCH`; zero external NAV returns null relative
  difference, not an error. Generated types stay synchronized; PostgreSQL
  integration exercises these routes and proves persisted provenance.

## Required verification

```text
task test-go TEST=Reconciliation
task test-go-integration TEST=ReconciliationAPI
task test-contract
task migrate-test
task verify
```
