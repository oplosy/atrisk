# AR-503 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-503-portfolio-ui.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-010, ADR-011, ADR-012, ADR-016 (accepted decision register)
- Owned paths: `apps/web/src/features/portfolio/`, `apps/web/src/routes/portfolio/`
- Shared paths changed and justification: `apps/web/src/routes/root/RootRoute.tsx` mounts the route and suppresses the shell's duplicate evidence controls on `/portfolio`.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| CSV commit is preview-gated by content hash | `PortfolioRoute.test.tsx` covers disabled commit before preview, changed import context, and a server hash mismatch. Commit recomputes SHA-256 before request and reuses one idempotency key per preview. |
| Converted lines expose selected price and ordered FX path | Valuation line details show selected price revision/identity and ordered TRY and USD quote revisions with direction; blocked-line component test covers the display. |
| Incomplete valuation is prominent and not full NAV | A non-valid run displays an alert and never renders the Complete NAV label; component test covers blocked state. |
| Reconciliation displays tolerance and differences | Checkpoint comparison renders cutoff, absolute/relative difference, effective tolerance and version; checkpoint creation is limited to valid valuations. |
| Forms preserve input on recoverable errors | Manual snapshot error test proves exact quantity remains after a rejected request. All forms keep controlled state after API errors. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: no local API/database fixture was connected to the Vite preview; the 320px browser check and hosted CI remain unverified. `task` CLI is unavailable locally, so `task verify` could not run.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run` | pass, 26 tests in 6 files |
| `npm run typecheck` | pass |
| `npm run build` | pass |
| `npm run lint` | pass |
| `npm run format:check` | pass |
| `node C:\Users\mesut\.agents\skills\impeccable\scripts\detect.mjs --json` | pass, `[]` |
| `/portfolio` in local in-app browser | route and controls render; localhost API returns HTTP 404 because no API proxy/backend is attached |
| `task verify` | unavailable: `task` executable not found |

## Change inventory

- Files changed: portfolio API helper, route component, route stylesheet, component tests, shared root route, this report.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-503-portfolio-ui`
- Commit SHA: task branch HEAD; exact SHA is reported in the handoff.
- Remote branch: not pushed; no branch-specific authorization yet.
- Worktree: clean after commit expected.

## Assumptions and risks

- CSV preview and commit use the same file hash, portfolio ID, captured-at timestamp, and server token. The server remains the final validator of preview token expiry and import schema.
- Browser evidence at 320px and real API-backed import, valuation, and reconciliation flows are still required before merge.
- This branch was developed in the primary checkout rather than a sibling task worktree; later task work should return the primary checkout to clean `main` and use an isolated worktree as prescribed by the execution protocol.
