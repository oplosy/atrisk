---
id: AR-003
title: Build CI and verification targets
status: merged
phase: 0
depends_on: [AR-002]
branch: task/AR-003-ci-and-verification
base_sha: ae36677feff56bf05c961820ff09ef73d6c65089
owned_paths: [Taskfile.yml, .github/workflows/, scripts/verify/]
shared_paths: [go.mod, risk-engine/pyproject.toml, package.json]
adrs: [ADR-018, ADR-019]
---

# AR-003: Build CI and verification targets

## Outcome

One local command and required GitHub checks deterministically validate the whole
repository without deploying anything.

## In scope

- `make` targets for format check, lint, typecheck, unit, contract, integration,
  build, generated-file drift, and aggregate `verify`.
- GitHub Actions with least-privilege permissions, concurrency cancellation,
  dependency caches, timeouts, and uploaded failure artifacts.
- Commands that work on Linux CI and are documented for Windows developers.

## Out of scope

- Deployment, releases, external secrets, or domain-specific integration tests.

## Acceptance criteria

- [x] `task verify` fails on format, lint, type, test, build, or generated drift.
- [x] CI uses lockfiles and pinned action major/commit policy from governance.
- [x] Workflow token permissions are read-only unless a job proves a need.
- [x] Re-running CI does not mutate tracked files.

## Required verification

```text
task verify
git status --short
```
