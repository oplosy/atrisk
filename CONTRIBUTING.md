# Contributing to AtlasRisk

AtlasRisk changes are delivered through a task packet, a short-lived task
branch, a reviewable pull request, and recorded verification evidence. The
repository is in the planning and foundation phase, so no contributor may
invent application scope or reopen an accepted architecture decision inside an
implementation task.

## Authority and scope

Use these sources in order:

1. The assigned `.ai/tasks/AR-*.md` packet is the scope authority.
2. Accepted ADRs in `docs/decisions/` are the architecture authority.
3. `docs/product/PRODUCT_DEFINITION.md` defines product scope and exclusions.
4. `docs/plans/EXECUTION_PROTOCOL.md` defines Git, worktree, and handoff rules.
5. `AGENTS.md` applies the same rules to every human and AI contributor.

Before editing, the packet must have `status: ready`. Read the packet, every
referenced ADR, and each file in its owned paths. Work only in the packet's
`owned_paths` and explicitly listed `shared_paths`. A worker must not spawn
agents, broaden scope, refactor adjacent code, or change an accepted decision.

Stop and report instead of guessing when:

- the packet is ambiguous or conflicts with an accepted ADR;
- an unrecorded architecture, API, schema, or migration decision is required;
- an active task owns a path that the change would need;
- a required dependency, verification command, or safe migration is missing;
- the current branch, worktree, or repository state violates the protocol.

## Branches and worktrees

The protected trunk is `main`. Keep the primary checkout on a clean `main`,
fast-forward it before starting work, and create the task branch from the exact
current `main` SHA:

```text
task/AR-NNN-short-slug
```

Use one sibling worktree per active writer, as specified in
`docs/plans/EXECUTION_PROTOCOL.md`. Do not work on `main`, use a long-lived
`dev` branch, share a writable worktree between writers, force-push, or push
directly to `main`. Push only the task branch after local verification. The
orchestrator owns review, integration, and merge.

### Empty-repository bootstrap exception

The normal no-direct-main-push rule has one historical bootstrap exception. If
the remote repository has no commit, a repository owner may explicitly approve
one seed commit to `main` so that a pull request has a base branch. This
exception is limited to creating the initial repository history; it does not
authorize feature, documentation, or follow-up pushes to `main`. AtlasRisk's
normal protected-trunk workflow applies immediately after the seed exists.

## Changes and commits

Keep changes minimal and task-scoped. Use English for code, API, schema, commit,
and task names. Do not commit API keys, access tokens, broker credentials,
personal financial data, production payloads, or generated files edited by hand.
Use synthetic, deterministic fixtures for tests; see `SECURITY.md`.

Use Conventional Commits and include the task ID:

```text
feat(ingestion): add FRED vintage adapter [AR-103]
```

Behavior changes require tests. Persistence changes require migration checks
from an empty database and the previous schema. Run every command in the task
packet and run `task verify` once that target exists.

## Pull requests

Every pull request must use `.github/pull_request_template.md` and include:

- the task ID and packet link;
- changed owned paths and any shared-path justification;
- ADR impact, including an explicit `none` when no ADR changes;
- tests and verification commands with results;
- migration impact and evidence, or an explicit `none`;
- evidence for each acceptance criterion and remaining risks.

Reviewers check the packet, ADRs, actual diff, verification evidence, secret
hygiene, and scope before merge. A PR is not complete merely because a branch
contains code; it requires review, required checks, a clean handoff report, and
merge by the orchestrator.

## Task lifecycle and handoff

Tasks move through:

```text
draft -> ready -> active -> review -> merged
```

`blocked` may be entered from `active` or `review` only with a concrete
blocking condition. Only the orchestrator changes task lifecycle status. A
worker returns the fields in `.ai/REPORT_TEMPLATE.md`, including acceptance
evidence, commands and results, changed files, commit SHA, remote branch state,
worktree state, assumptions, and unresolved risks.
