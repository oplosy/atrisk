# AtlasRisk Agent Rules

These instructions apply to every human or AI contributor.

## Authority and source order

1. The assigned `.ai/tasks/AR-*.md` task packet is the scope authority.
2. Accepted ADRs in `docs/decisions/` are architecture authority.
3. `docs/product/PRODUCT_DEFINITION.md` defines product scope and exclusions.
4. `docs/plans/EXECUTION_PROTOCOL.md` defines Git, worktree, and handoff rules.
5. If sources conflict, stop and report the conflict. Do not silently choose.

## Before changing files

- Work only from a task packet whose status is `ready`.
- Read the task packet, every ADR it references, and the files it owns.
- Confirm the current branch is `task/AR-NNN-slug`, never `main`.
- Confirm the worktree is clean before starting.
- Do not broaden scope, change an accepted decision, or refactor adjacent code.
- A worker must not spawn more agents. Delegation belongs to the orchestrator.

## Architecture invariants

- Raw source payloads and normalized observation revisions are append-only.
- `source as-of` and `system as-of` are distinct query semantics.
- Latest-value views are projections, never destructive updates of history.
- Financial amounts use exact decimal storage. Floating-point conversion is
  allowed only inside documented numerical calculations.
- Every valuation records the price and FX quote path used.
- Every risk or stress run records input snapshots, parameter version, engine
  version, data-quality state, and deterministic output.
- Missing or stale required data cannot produce a healthy/green result.
- No order execution, custody, broker credentials, or automated trading logic.
- LLM output cannot enter deterministic valuation or risk calculations.

## Change discipline

- Prefer the smallest complete change that satisfies the task acceptance criteria.
- Add or update tests with behavior changes.
- Do not edit generated files by hand.
- Do not commit secrets, API keys, personal portfolio data, or production payloads.
- Use UTC `timestamptz` at persistence boundaries and preserve source timezone
  metadata when the source supplies it.
- Use English for code, APIs, schema names, commits, and task artifacts.

## Verification and handoff

- Run every command required by the task packet.
- Run `task verify` before handoff once that target exists.
- Record commands, results, changed files, assumptions, and unresolved risks in
  a report based on `.ai/REPORT_TEMPLATE.md`.
- Commit with Conventional Commits and include the task ID, for example:
  `feat(ingestion): add FRED vintage adapter [AR-103]`.
- Push only the task branch. Never push directly to `main`.
- Leave the worktree clean and report the commit SHA and remote branch state.
