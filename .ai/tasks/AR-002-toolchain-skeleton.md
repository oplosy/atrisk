---
id: AR-002
title: Create the polyglot toolchain skeleton
status: merged
phase: 0
depends_on: [AR-001]
branch: task/AR-002-toolchain-skeleton
base_sha: b483537c17821dd45383983254a842ec953266d4
owned_paths: [go.mod, go.sum, apps/, internal/, risk-engine/, package.json, package-lock.json]
shared_paths: [.gitignore, .editorconfig, README.md]
adrs: [ADR-003, ADR-009, ADR-013]
---

# AR-002: Create the polyglot toolchain skeleton

## Outcome

Go, Python, and React components build from a clean clone with pinned runtimes and
locked dependencies, without implementing domain behavior.

## In scope

- Go 1.27 module and minimal `api`/`collector` commands.
- Python 3.14 `uv` project and importable risk-engine package.
- React 19 TypeScript/Vite application using npm lockfile.
- Formatting, lint, typecheck, and test configuration for each language.
- Exact runtime/version files and generated-file conventions.

## Out of scope

- Database access, HTTP domain endpoints, ingestion, calculations, or UI flows.

## Acceptance criteria

- [x] Clean installs use committed lockfiles and documented commands.
- [x] Each component has a minimal passing test and production build.
- [x] No component depends on another component's internal source files.
- [x] Repository ignores build output, environments, secrets, and local worktrees.
- [x] Runtime and dependency versions are reported by a diagnostic command.

## Required verification

```text
go test ./...
uv run --project risk-engine pytest
npm ci
npm test -- --run
npm run build
git diff --check
```
