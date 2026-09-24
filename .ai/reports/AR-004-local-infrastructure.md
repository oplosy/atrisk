# AR-004 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-004-local-infrastructure.md`
- Packet status at start: `blocked`; runtime access recovered on 2026-09-24 and status returned to `active` for verification.
- Referenced ADRs: ADR-001, ADR-004, ADR-005, ADR-017
- Owned paths: `infra/compose/`, `scripts/infra/`
- Shared paths changed and justification: `.env.example`, `Taskfile.yml`, and `README.md` for local infrastructure configuration, commands, and usage documentation.

## Result

`complete` — live runtime acceptance passed, independent review found no blockers, and runtime evidence PR #8 merged to main. No Docker settings were changed during recovery verification.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Services become healthy from a clean environment. | `docker compose -f infra/compose/compose.yaml up -d --wait` created fresh development volumes and both PostgreSQL 18.6 and Garage 2.4.1 reported healthy. |
| PostgreSQL and object storage are reachable only through declared local ports. | Compose `config` and `ps` show `127.0.0.1:55432->5432` and `127.0.0.1:53900->3900`; `Test-NetConnection` succeeded on both declared loopback ports; `pg_isready` reported accepting connections. |
| Test infrastructure cannot reuse development databases/buckets. | Test runs use a unique bounded Compose project name, project-scoped volumes, and ephemeral host ports; static test and dynamic Compose config check passed. |
| Test interruption cleans up only its own project. | Async child-process runner tracks the active Docker command, handles SIGINT/SIGTERM, waits for it to stop, then runs `down --volumes --remove-orphans`; focused static test passed. SIGKILL/forced host termination remains unhandleable. |
| Garage supports the exact Put/Get/Head/List operations used by the archive adapter. | `task test-infra` dynamic S3 smoke passed `PutObject`, `GetObject`, `HeadObject`, and `ListObjectsV2` against Garage. |
| No real credential or personal data appears in tracked configuration. | Removed tracked Garage RPC secret; a project-scoped named volume generates and persists a 32-byte random RPC secret. Static secret configuration test passed. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: none for AR-004 acceptance. The project dev services are intentionally left healthy and running on loopback.

## Verification

| Command | Result |
|---|---|
| `docker compose -f infra/compose/compose.yaml config` | pass |
| `docker compose -f infra/compose/compose.yaml up -d --wait` | pass; clean development project started, PostgreSQL and Garage healthy |
| `docker compose -f infra/compose/compose.yaml ps` | pass; both services healthy with loopback-only ports |
| `Test-NetConnection 127.0.0.1 -Port 55432 -InformationLevel Quiet` | pass; `True` |
| `Test-NetConnection 127.0.0.1 -Port 53900 -InformationLevel Quiet` | pass; `True` |
| `docker exec atrisk-postgres-1 pg_isready -U atrisk -d atrisk` | pass; accepting connections |
| dynamic test Compose configuration | pass; no fixed host port and project-scoped volume names |
| `node --test scripts/infra/infra.test.mjs` | pass, 3/3 |
| `node --check scripts/infra/infra.mjs` | pass |
| `.task/bin/task.exe verify` | pass |
| `.task/bin/task.exe test-infra` | pass, exit 0; static tests 3/3 and S3 smoke Put/Get/Head/List passed |
| `docker ps -a --filter name=atrisk-test-d5ae4c1cdf1ead64` | pass; no test containers remain |
| `docker volume ls --filter name=atrisk-test-d5ae4c1cdf1ead64` | pass; no test volumes remain |
| `docker network ls --filter name=atrisk-test-d5ae4c1cdf1ead64` | pass; no test network remains |
| `gh pr checks 8 --watch` | pass; required GitHub Actions Verify succeeded. |
| `git diff --check` | pass |

## Change inventory

- Files changed in the runtime-proof branch: `.ai/tasks/AR-004-local-infrastructure.md` and this report; implementation code was merged in PR #5.
- Orchestrator-only bookkeeping: AR-004 status returned from `blocked` to `active` after runtime availability was restored, moved to `review` after independent review, and is marked `merged` in this follow-up branch.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Implementation PR: #5, merge commit `b59acf82fda547e84b99a00cf9d9a5601dac6f59`.
- Runtime-proof PR: #8, merged 2026-09-24; merge commit `f2bf9b6ef240559a1fd07c39717fa79186155c90`.
- Runtime-proof branch: `task/AR-004-runtime-proof`, based on main `6156389a42c5422ff49706cea6d02b271f02c99d`; runtime-evidence commit `ee66f0acb61cfcbe0ee7cd0f3d90396ca8850ca8`.
- Runtime acceptance: verified 2026-09-24; dev project `atrisk` remains running and healthy on loopback. Isolated smoke project `atrisk-test-d5ae4c1cdf1ead64` was removed with no leftover containers, volumes, or networks.
- `origin/task/AR-004-runtime-proof` verified at `ee66f0acb61cfcbe0ee7cd0f3d90396ca8850ca8` before the review-state update.
- Lifecycle-finalization branch: `task/AR-004-status-finalization`, based on merge commit `f2bf9b6ef240559a1fd07c39717fa79186155c90`; it records the final `merged` packet state through a follow-up PR.
- Worktree: clean at runtime-evidence handoff; final lifecycle/report changes are in progress.

## Assumptions and risks

- Garage's upstream v2.4.1 image is `FROM scratch` with no `USER` directive. Runtime verification confirmed the generated mode-0600 RPC secret was readable by Garage, the secret-init helper exited successfully, and Garage became healthy.
- SIGINT/SIGTERM cleanup is asynchronous and waits for the active child before removing the unique project; SIGKILL or forced host termination cannot run process cleanup handlers.
- First-start RPC secret generation is per Compose project and persists in that project's named volume; `task infra-reset` removes the development secret with other development volumes.
