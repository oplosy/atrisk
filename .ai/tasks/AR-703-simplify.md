---
id: AR-703
title: Behavior-preserving simplification pass
status: ready
phase: 7
depends_on: [AR-701, AR-702]
branch: task/AR-703-simplify
base_sha: a4a060559e40c0773fe7eb7475b8f08b313ec1bc
owned_paths: [apps/, internal/, risk-engine/, scripts/, test/]
shared_paths: []
adrs: [ADR-001]
---

# AR-703: Behavior-preserving simplification pass

## Outcome

The codebase has less duplicated, dead, or needlessly complex code, with no
change to behavior, public contracts, schema, or recorded outputs.

## Context

All V1 packets and release packaging are merged. The user requested a `/simplify`
pass over the whole codebase after `v1.0.1`.

## In scope

- Reuse of existing helpers instead of re-implementations.
- Removal of dead code and redundant state.
- Removal of wasted work and unnecessary nesting.

## Out of scope

- Behavior, API, schema, contract, or migration changes (`contracts/`, `db/`).
- Generated files, lockfiles, dependency changes.
- Findings whose fix changes deterministic valuation or risk output.

## Acceptance criteria

- [ ] Every change is behavior-preserving and stays within `owned_paths`.
- [ ] Existing tests pass unchanged; any edited test keeps its assertions.
- [ ] Skipped findings are listed in the report with the reason.

## Required verification

```text
task verify   (CI)
```

## Handoff evidence

- Commit SHA and pushed branch.
- Changed files.
- Commands and results.
- Acceptance-criterion mapping.
- Remaining risks or `none`.
