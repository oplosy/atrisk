---
id: AR-304
title: Implement explainable stress attribution
status: draft
phase: 3
depends_on: [AR-303]
branch: task/AR-304-factor-attribution
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

- [ ] Factor contribution plus visible residual reconciles to total P&L.
- [ ] Position contributions reconcile to total P&L.
- [ ] Shapley axioms are covered by property tests.
- [ ] Maximum supported factor count is validated before combinatorial work.
- [ ] The result records method/version/tolerance and unmapped positions.

## Required verification

```text
task test-python TEST=attribution
task test-golden TEST=attribution
task test-contract
```
