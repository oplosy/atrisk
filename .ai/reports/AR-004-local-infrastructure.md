# AR-004 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-004-local-infrastructure.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-001, ADR-004, ADR-005, ADR-017
- Owned paths: `infra/compose/`, `scripts/infra/`
- Shared paths changed and justification: `.env.example`, `Taskfile.yml`, and `README.md` for local infrastructure configuration, commands, and usage documentation.

## Result

`needs-review` — implementation and static checks pass, but Docker Desktop cannot start, so live service health and S3 smoke acceptance remain unverified.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Services become healthy from a clean environment. | Blocked: Docker API unavailable (`failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine; The system cannot find the file specified.`). |
| PostgreSQL and object storage are reachable only through declared local ports. | Compose configuration binds published database/S3 ports to `127.0.0.1`; configuration check passed. Runtime reachability not verified. |
| Test infrastructure cannot reuse development databases/buckets. | Test runs use a unique bounded Compose project name, project-scoped volumes, and ephemeral host ports; static test and dynamic Compose config check passed. |
| Test interruption cleans up only its own project. | Async child-process runner tracks the active Docker command, handles SIGINT/SIGTERM, waits for it to stop, then runs `down --volumes --remove-orphans`; focused static test passed. SIGKILL/forced host termination remains unhandleable. |
| Garage supports the exact Put/Get/Head/List operations used by the archive adapter. | `task test-infra` is configured to perform PutObject/GetObject/HeadObject/ListObjectsV2, but execution was blocked before the Docker API could start. |
| No real credential or personal data appears in tracked configuration. | Removed tracked Garage RPC secret; a project-scoped named volume generates and persists a 32-byte random RPC secret. Static secret configuration test passed. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: Docker Desktop runtime is unavailable; service health, volume permission behavior, and live S3 operations are unverified.

## Verification

| Command | Result |
|---|---|
| `docker compose -f infra/compose/compose.yaml config` | pass |
| dynamic test Compose configuration | pass; no fixed host port and project-scoped volume names |
| `node --test scripts/infra/infra.test.mjs` | pass, 3/3 |
| `node --check scripts/infra/infra.mjs` | pass |
| `.task/bin/task.exe verify` | pass |
| `.task/bin/task.exe test-infra` | static checks pass, 3/3; Docker runtime blocked at `npipe:////./pipe/dockerDesktopLinuxEngine` |
| `git diff --check` | pass |

## Change inventory

- Files changed: `.env.example`, `README.md`, `Taskfile.yml`, `infra/compose/compose.yaml`, `infra/compose/garage.toml`, `scripts/infra/infra.mjs`, `scripts/infra/infra.test.mjs`, and this report.
- Orchestrator-only bookkeeping: `.ai/tasks/AR-003-ci-and-verification.md` is reviewed as `status: merged` from PR #4; it is not modified by AR-004 and no task status is changed here. Its inherited merged record explains why the existing `task verify` target is available to this task.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-004-local-infrastructure`
- Commit SHA: `f6565876e7edb503e14f7dce170ee93c02653a71` (async cleanup implementation; report update follows in this handoff)
- Remote branch: `origin/task/AR-004-local-infrastructure`, implementation SHA matches remote before report commit
- Worktree: clean after the report commit

## Assumptions and risks

- Garage's upstream v2.4.1 image is `FROM scratch` with no `USER` directive, so the generated root-owned mode-0600 secret is expected to be readable; actual mounted-volume permission behavior remains unverified without Docker runtime.
- SIGINT/SIGTERM cleanup is asynchronous and waits for the active child before removing the unique project; SIGKILL or forced host termination cannot run process cleanup handlers.
- First-start RPC secret generation is per Compose project and persists in that project's named volume; `task infra-reset` removes the development secret with other development volumes.
