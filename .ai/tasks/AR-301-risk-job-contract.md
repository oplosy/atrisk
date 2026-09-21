---
id: AR-301
title: Implement risk jobs and golden fixtures
status: draft
phase: 3
depends_on: [AR-107, AR-203]
branch: task/AR-301-risk-job-contract
owned_paths: [internal/jobs/, risk-engine/src/atlasrisk/jobs/, contracts/jobs/, test/fixtures/risk/]
shared_paths: [db/migrations/, db/queries/jobs/, apps/api/, Taskfile.yml]
adrs: [ADR-008, ADR-009, ADR-010, ADR-013]
---

# AR-301: Implement risk jobs and golden fixtures

## Outcome

Go can enqueue and Python can claim, validate, execute, and idempotently complete a
versioned deterministic risk job against immutable input snapshots.

## In scope

- Job schema, state machine, leases, attempts, retry policy, cancellation, and
  idempotency keys.
- Worker loop, canonical JSON serialization, engine version, result hash, and
  language-neutral fixture corpus validated by Go and Python.

## Out of scope

- Risk formulas, scenario formulas, external queue, or Python HTTP service.

## Acceptance criteria

- [ ] Concurrent workers cannot execute the same active lease simultaneously.
- [ ] Expired leases recover safely; completion is idempotent.
- [ ] Unknown schema/kind is a permanent explicit failure.
- [ ] Retryable and permanent failures transition differently and retain evidence.
- [ ] Go and Python agree on every golden payload and canonical hash.

## Required verification

```text
task test-go TEST=Jobs
task test-python TEST=jobs
task test-integration TEST=RiskJobLifecycle
task test-contract
```
