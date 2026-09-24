---
id: AR-107
title: Implement data-quality gates
status: review
phase: 1
depends_on: [AR-101, AR-106]
branch: task/AR-107-data-quality-engine
base_sha: 7ca863714061a96e4eff394003de0916345844e6
owned_paths: [internal/quality/, internal/application/quality/, db/queries/quality/, apps/api/handlers/quality/]
shared_paths: [contracts/openapi/, db/migrations/, db/queries/core/sqlc.yaml, internal/platform/database/, apps/api/cmd/api/, test/integration/, .github/workflows/ci.yml, test/fixtures/quality/]
adrs: [ADR-011]
---

# AR-107: Implement data-quality gates

## Outcome

Series and calculation inputs receive deterministic fresh/stale/missing/partial/
suspect/revised classifications and valid/degraded/blocked aggregate states.

## In scope

- Versioned freshness/expected-frequency policies and source calendars.
- Structured reason codes, affected interval/entity references, and API endpoints.
- Aggregation rules distinguishing required and optional inputs.

## Freshness policy contract

Read the policy from the existing `series.freshness_policy` JSON object. It must
contain a non-empty `version` and positive `max_age` (`time.Duration` syntax or
`PnD`); `expected` may be `calendar_daily`, `business_daily`, `weekly`,
`monthly`, `quarterly`, or `irregular`; if omitted, derive it from
`series.frequency`.
Optional `availability_lag` uses the same duration syntax, and optional
`holiday_dates` is a list of `YYYY-MM-DD` dates in the series source timezone.
Use the persisted source timezone, falling back to UTC only when none is set.
Calendar slots are every date for `calendar_daily`, Monday-Friday excluding
holidays for `business_daily`, Fridays for `weekly`, the first of each month
for `monthly`, and the first day of each quarter for `quarterly`; `irregular`
does not infer missing periods.
Missing/malformed policies fail closed. Evaluation must return the policy version
and UTC cutoff; it must not mutate series policy or observation history.

## Out of scope

- UI styling or silently repairing missing observations.

## Acceptance criteria

- [x] Fixed-clock tests cover fresh/stale/missing/partial/suspect/revised inputs and expected-period behavior across holidays, weekends, late releases, and gaps.
- [x] Required missing, stale, or suspect input always blocks dependent calculations; blocked results cannot be represented as healthy/valid.
- [x] Optional stale/partial input degrades and never reports valid; fresh required inputs can produce valid results.
- [x] Every evaluation identifies its policy version and UTC evaluation cutoff and returns stable reason codes with affected series/entity/interval references.
- [x] Quality API responses are deterministic and machine-readable; missing or invalid policy/configuration fails closed rather than producing a healthy result.

## Required verification

```text
task test-go TEST=Quality
task test-go-integration TEST=QualityAPI
task test-contract
```
