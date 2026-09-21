---
id: AR-402
title: Seal decision evidence snapshots
status: draft
phase: 4
depends_on: [AR-305, AR-401]
branch: task/AR-402-decision-evidence
owned_paths: [internal/application/evidence/, db/queries/evidence/, apps/api/handlers/evidence/]
shared_paths: [db/migrations/, contracts/openapi/, internal/domain/journal/]
adrs: [ADR-006, ADR-007, ADR-013, ADR-015]
---

# AR-402: Seal decision evidence snapshots

## Outcome

Finalizing a decision atomically binds immutable portfolio, data, valuation, risk,
scenario, engine, and contract versions that can be reconstructed later.

## In scope

- Evidence manifest, atomic finalization, canonical hash, reconstruction query,
  unavailable-artifact handling, and amendment links.

## Out of scope

- Copying mutable latest views or regenerating old evidence with a new engine.

## Acceptance criteria

- [ ] Finalization fails atomically if a required referenced result is mutable/incomplete.
- [ ] Later data revisions, portfolio snapshots, and scenario versions do not alter reconstruction.
- [ ] Manifest/hash covers every referenced identifier and version.
- [ ] Missing archived evidence is an explicit integrity failure, not a latest fallback.
- [ ] A golden historical-decision fixture reconstructs byte-identically after new revisions.

## Required verification

```text
task test-go TEST=DecisionEvidence
task test-integration TEST=HistoricalDecisionReconstruction
task test-contract
```
