---
id: AR-401
title: Implement the append-only decision journal
status: draft
phase: 4
depends_on: [AR-201]
branch: task/AR-401-decision-journal
owned_paths: [internal/domain/journal/, internal/application/journal/, db/queries/journal/, apps/api/handlers/journal/]
shared_paths: [db/migrations/, contracts/openapi/]
adrs: [ADR-002, ADR-007, ADR-022]
---

# AR-401: Implement the append-only decision journal

## Outcome

Users can record a structured investment decision and append reviews/amendments
without rewriting the original thesis or turning intent into execution.

## In scope

- Thesis, alternatives, evidence references, invalidation conditions, horizon,
  risk budget, intended action, tags, reviews, and linked amendments.
- Draft/finalized lifecycle and API validation.

## Out of scope

- Orders, recommendations, broker actions, LLM-generated theses, or evidence sealing.

## Acceptance criteria

- [ ] Finalized decisions are immutable; reviews/amendments are separate rows.
- [ ] Risk budget has explicit currency/measure/horizon units.
- [ ] Invalidation conditions are required structured text, not an optional afterthought.
- [ ] Intended action cannot trigger an external side effect.
- [ ] Timeline API preserves creation/review order and authorship/source metadata.

## Required verification

```text
task migrate-test
task test-go TEST=DecisionJournal
task test-go-integration TEST=DecisionJournalAPI
task test-contract
```
