# ADR-025: Account-scoped immutable reconciliation checkpoints

- Status: Accepted
- Date: 2026-09-25
- Related decisions: ADR-007, ADR-010, ADR-011, ADR-012, ADR-016

## Context

AtlasRisk must compare an externally stated account NAV with the matching
account's persisted valuation without rewriting valuation history, silently
converting currencies, or allowing a partial valuation to look reconciled.
The existing snapshot model assigns each position line to an account, and each
valuation line persists exact TRY and USD amounts.

## Decision

- A checkpoint references one immutable valuation run and one account belonging
  to the portfolio snapshot used by that run. Its NAV is the exact sum of
  persisted valuation-line amounts for that account, using the selected
  `try_amount` or `usd_amount`; it never reruns valuation or converts currencies.
- Only valuation runs with state `valid` may be reconciled. `degraded`,
  `blocked`, incomplete, or account-empty inputs are rejected with stable reason
  codes.
- Supported checkpoint currencies are TRY and USD and must select the matching
  persisted amount. The externally stated currency must match. The external
  cutoff must equal the valuation cutoff as a UTC instant. No implicit FX or
  cutoff substitution is permitted.
- Absolute difference is `abs(external_nav - valuation_nav)`. Relative
  difference is absolute difference divided by `abs(external_nav)`; it is null
  when external NAV is zero. The checkpoint is reconciled when the absolute
  difference is less than or equal to its effective tolerance.
- The default effective tolerance is the greater of 0.01 reporting-currency
  units and one basis point of absolute external NAV. Accounts may define
  append-only, monotonically versioned absolute-tolerance overrides. A
  policy amount is a decimal NUMERIC(38,18) value greater than or equal to
  zero. A tolerance-version create request locks the account row in its transaction,
  reads the next integer version, inserts it under a unique
  `(account_id, version)` constraint, and commits. Checkpoint creation locks the
  same account row and snapshots the latest committed version in that
  transaction, together with the effective tolerance; if none exists, it
  selects version 0 and the default formula. Changing a later version cannot
  alter prior checkpoints.
- Optional line checks identify a unique snapshot line in the selected account
  and preserve the external amount, inherently denominated in the checkpoint
  currency. Unknown, duplicate, or cross-account lines are rejected. If
  `line_checks_complete` is false and a strict subset is supplied, creation
  returns 201 with `line_check_state=partial`; line checks never replace account
  NAV. If `line_checks_complete` is true but any account line is missing,
  creation returns 409 `LINE_CHECK_INCOMPLETE`. A complete set covers every
  account line and sums exactly to external NAV at NUMERIC(38,18) scale;
  creation returns `line_check_state=complete`. With no line checks, the state
  is `none`. Per-line differences are reported but the account-level reconciled
  state is determined by the NAV tolerance. A zero external NAV is valid; only
  its relative difference is null.
- Checkpoints and tolerance policy versions are append-only. API reads return
  the referenced valuation, account, currency/cutoff, policy version, effective
  tolerance, exact differences, state/reason, and line-check evidence.
- The contract-first HTTP surface is:
  - `POST /api/v1/accounts/{account_id}/reconciliation-tolerances` creates the
    next immutable absolute-tolerance version from a decimal-string
    `tolerance_amount`; the response returns `account_id`, `version`, amount,
    and `created_at`.
  - `POST /api/v1/valuations/{valuation_id}/reconciliations` creates a
    checkpoint from `account_id`, `source_label`, `currency`, RFC3339 `cutoff`,
    decimal-string `external_nav`, optional `line_checks` (each containing
    `snapshot_line_id` and decimal-string `external_amount`), and
    `line_checks_complete` (default false). Checkpoint responses return
    `line_check_state` with exactly `none`, `partial`, or `complete`.
  - `GET /api/v1/reconciliations/{reconciliation_id}` returns the immutable
    checkpoint and its selected valuation lines/policy evidence.
  - Successful creates return 201 and retrieval returns 200. Invalid shapes or
    decimal values return 400; missing IDs return 404; valuation/account,
    currency, cutoff, or line membership incompatibilities return 409 with a
    stable machine-readable reason code.
- Created checkpoint states are exactly `reconciled` and `unreconciled`.
  Stable conflict reason codes are `VALUATION_NOT_VALID`,
  `ACCOUNT_NOT_IN_SNAPSHOT`, `ACCOUNT_HAS_NO_LINES`, `CURRENCY_MISMATCH`,
  `CUTOFF_MISMATCH`, `LINE_CHECK_UNKNOWN`, `LINE_CHECK_DUPLICATE`,
  `LINE_CHECK_CROSS_ACCOUNT`, `LINE_CHECK_INCOMPLETE`, and
  `LINE_CHECK_TOTAL_MISMATCH`. Zero external NAV and tolerance-version
  selection are not errors and do not produce reason codes.

## Consequences

- Reconciliation requires account-filtered sums of persisted valuation lines;
  a portfolio-wide total is not a substitute.
- The database must enforce account/portfolio linkage and immutability for
  policy versions and checkpoints, plus uniqueness/serialization for
  `(account_id, version)`.
- The API must expose tolerance-version creation, checkpoint creation, and
  checkpoint retrieval with contract-first request/response schemas.
- This is a comparison workflow only; it does not edit positions, valuations,
  or external accounting records.
