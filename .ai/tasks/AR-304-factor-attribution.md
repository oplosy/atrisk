---
id: AR-304
title: Implement explainable stress attribution
status: review
phase: 3
depends_on: [AR-303]
branch: task/AR-304-factor-attribution
base_sha: c3a67cfd98cca007e4afca648828659dd4ed2f97
owned_paths: [risk-engine/src/atlasrisk/attribution/, risk-engine/tests/attribution/]
shared_paths: [contracts/jobs/, test/fixtures/risk/]
adrs: [ADR-013, ADR-014]
---

# AR-304: Implement explainable stress attribution

## Outcome

Every supported stress loss reconciles by position and factor using deterministic
Shapley allocation, while unsupported or numerical residuals remain visible.

## In scope

- Exact position breakdown, factor subsets/permutations for bounded V1 factors,
  Shapley contribution, interaction residual, tolerances, and runtime bounds.
- Properties for symmetry, dummy factor, efficiency, and permutation invariance.

## Out of scope

- Approximate sampling for large factor sets or arbitrary attribution narratives.

## Acceptance criteria

- [x] Factor contribution plus visible residual reconciles to total P&L.
- [x] Position contributions reconcile to total P&L.
- [x] Shapley axioms are covered by property tests.
- [x] Maximum supported factor count is validated before combinatorial work.
- [x] The result records method/version/tolerance and unmapped positions.

## Required verification

```text
uv run --project risk-engine --locked pytest risk-engine/tests/attribution -q
uv run --project risk-engine --locked pytest risk-engine/tests/attribution/test_golden.py -q
task test-contract
```
