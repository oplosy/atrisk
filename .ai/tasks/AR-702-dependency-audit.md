---
id: AR-702
title: Restore the dependency scan gate
status: active
phase: 7
depends_on: [AR-602]
branch: task/AR-702-dependency-audit
base_sha: de523e09194e857af58b29783bc66488b776bc62
owned_paths: []
shared_paths: [package.json, package-lock.json, scripts/backup/security-scan.sh]
adrs: [ADR-018]
---

# AR-702: Restore the dependency scan gate

## Outcome

`task security-scan` passes again on `main`, and future advisories in
development-only npm tooling are reported without blocking every change.

## Context

Since 2026-10-05 `npm audit --audit-level=high` fails on `main` without any code
change: new advisories cover `source-map-js` 1.0.0-1.2.1 (via `vite` and
`postcss`) and `braces` (every version, via `typescript-eslint` 8.40.0, then
`fast-glob` and `micromatch`). All affected packages are development tooling;
the release web bundle ships only `react` and `react-dom`.

## In scope

- Upgrade `typescript-eslint` to 8.71.1, which no longer depends on
  `fast-glob`, and `source-map-js` to 1.2.2 in the lockfile.
- Gate `npm audit` on runtime dependencies (`--omit=dev`); keep auditing the
  full tree as a visible, non-blocking warning.

## Out of scope

- Other dependency upgrades and the Go and Python scanners, which stay unchanged.

## Acceptance criteria

- [ ] `npm audit --audit-level=high` reports no vulnerabilities.
- [ ] Web lint, typecheck, tests, and build pass with the upgraded tooling.
- [ ] A high-severity advisory in a runtime dependency still fails `task security-scan`.
- [ ] CI `Verify` passes.

## Required verification

```text
npm audit --audit-level=high
npm run lint && npm run typecheck && npm test -- --run && npm run build
task security-scan   (CI)
task verify          (CI)
```

## Handoff evidence

- Commit SHA and pushed branch.
- Changed files.
- Commands and results.
- Acceptance-criterion mapping.
- Remaining risks or `none`.
