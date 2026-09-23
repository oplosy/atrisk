---
id: AR-001
title: Establish repository governance
status: merged
phase: 0
depends_on: []
branch: task/AR-001-repository-governance
base_sha: ffbc5aef830a9845867911006c600af185a79404
owned_paths: [CONTRIBUTING.md, SECURITY.md, .github/, docs/plans/EXECUTION_PROTOCOL.md]
shared_paths: [AGENTS.md, .ai/]
adrs: [ADR-018, ADR-019, ADR-020, ADR-021]
---

# AR-001: Establish repository governance

## Outcome

Contributors and external AI agents have one enforceable Git, scope, security,
review, and handoff workflow before product code exists.

## In scope

- Contribution, security, issue, PR, CODEOWNERS, branch-protection, and secret rules.
- Finalize task lifecycle and worker report format without changing product ADRs.
- Document the one-time empty-repository bootstrap exception separately from the
  normal no-direct-main-push rule.

## Out of scope

- Application scaffolding, CI jobs, domain code, or remote branch mutation.

## Acceptance criteria

- [ ] PR template requires task ID, ADR impact, tests, migrations, and evidence.
- [ ] Security guidance forbids real API keys and personal financial fixtures.
- [ ] Branch policy requires PR checks and disallows force/direct pushes to main.
- [ ] Any AI can identify task authority, owned paths, and stop conditions.
- [ ] Documentation contains no conflicting Git instructions.

## Required verification

```text
git diff --check
rg -n "main|worktree|task packet|secret" AGENTS.md CONTRIBUTING.md SECURITY.md docs/plans .github
```
