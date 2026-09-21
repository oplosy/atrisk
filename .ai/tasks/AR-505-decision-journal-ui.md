---
id: AR-505
title: Build the decision journal UI
status: draft
phase: 5
depends_on: [AR-402, AR-501]
branch: task/AR-505-decision-journal-ui
owned_paths: [apps/web/src/features/journal/, apps/web/src/routes/decisions/]
shared_paths: [apps/web/src/components/, apps/web/src/generated/]
adrs: [ADR-002, ADR-007, ADR-022]
---

# AR-505: Build the decision journal UI

## Outcome

Users can draft/finalize a decision, review sealed evidence before finalization,
and later append a review while clearly seeing the original historical context.

## In scope

- Decision form, alternatives, invalidation conditions, risk budget, evidence
  picker/preview, finalization confirmation, reconstruction, reviews/amendments.

## Out of scope

- Order placement, AI-authored decisions, editing finalized evidence, or social sharing.

## Acceptance criteria

- [ ] Finalization preview lists every snapshot/result/version being sealed.
- [ ] Original thesis/evidence and later review are visually and semantically distinct.
- [ ] Broken evidence integrity is a blocking error, never a latest-data fallback.
- [ ] Intended action is labeled as a journal statement, not an executable control.
- [ ] Draft recovery and validation work without duplicate finalization.

## Required verification

```text
task test-web TEST=journal
task test-web-a11y ROUTE=/decisions
task test-e2e TEST=decision_journal
```
