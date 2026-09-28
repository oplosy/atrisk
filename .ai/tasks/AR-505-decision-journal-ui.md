---
id: AR-505
title: Build the decision journal UI
status: merged
phase: 5
depends_on: [AR-402, AR-501]
branch: task/AR-505-decision-journal-ui
base_sha: ac8a92d3b85bab739f612928657f3dea59083eaa
local_tree_base_sha: 27c6d9e279c438d3983b12222472b7b8318a8059
owned_paths: [apps/web/src/features/journal/, apps/web/src/routes/decisions/]
shared_paths: [apps/web/src/components/, apps/web/src/generated/, apps/web/src/App.tsx, apps/web/src/routes/root/RootRoute.tsx, apps/web/src/App.test.tsx]
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
npm test -- --run apps/web/src/routes/decisions apps/web/src/App.test.tsx
npm run typecheck
npm run build
npm run lint
npm run format:check
task verify
```

Record `/decisions` browser evidence at desktop and 320px, including keyboard
navigation, draft recovery, finalization preview, historical reconstruction, and
the blocked integrity-error state. Keep evidence references in the task report.
