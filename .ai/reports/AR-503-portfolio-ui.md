# AR-503 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-503-portfolio-ui.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-010, ADR-011, ADR-012, ADR-016 (accepted decision register)
- Owned paths: `apps/web/src/features/portfolio/`, `apps/web/src/routes/portfolio/`
- Shared paths changed and justification: `apps/web/src/routes/root/RootRoute.tsx` mounts the route and suppresses the shell's duplicate evidence controls on `/portfolio`.

## Result

`merged` (PR #54)

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| CSV commit is preview-gated by content hash | `PortfolioRoute.test.tsx` covers disabled commit before preview, changed import context, and a server hash mismatch. Commit recomputes SHA-256 before request and reuses one idempotency key per preview. |
| Converted lines expose selected price and ordered FX path | Valuation line details show selected price revision/identity and ordered TRY and USD quote revisions with direction; blocked-line component test covers the display. |
| Incomplete valuation is prominent and not full NAV | A non-valid run displays an alert and never renders the Complete NAV label; component test covers blocked state. |
| Reconciliation displays tolerance and differences | Checkpoint comparison renders cutoff, absolute/relative difference, effective tolerance and version; checkpoint creation is limited to valid valuations. |
| Forms preserve input on recoverable errors | Manual snapshot error test proves exact quantity remains after a rejected request. All forms keep controlled state after API errors. |
| Portfolio context is isolated after switching | Three deferred-response tests prove delayed snapshot, valuation, and CSV snapshot-refresh results are ignored. Two more assertions verify existing error and success notices clear on switch. Implemented in `83be50a` and `93c0dba`; included in the reviewed and merged PR. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: no local API/database fixture is connected to the Vite preview, so real API-backed form submissions remain unverified and show HTTP 404. Hosted `task verify` remains pending; the `task` CLI is unavailable locally.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run` | pass, 30 tests in 6 files |
| `npm run typecheck` | pass |
| `npm run build` | pass |
| `npm run lint` | pass |
| `npm run format:check` | pass |
| `node C:\Users\mesut\.agents\skills\impeccable\scripts\detect.mjs --json` | pass, `[]` |
| `/portfolio` in local in-app browser | route and controls render; localhost API returns HTTP 404 because no API proxy/backend is attached |
| `/portfolio` in local browser at `http://127.0.0.1:4175/portfolio` | Desktop accessibility tree exposed the primary navigation, snapshot, CSV import, valuation, and reconciliation controls. With the real API unattached, the route returned HTTP 404 and kept CSV preview/commit disabled because no portfolio was available. |
| Browser flow with local fixture API | Headless Chrome intercepted the page's `fetch` calls in-page and served synthetic portfolio/account/instrument responses only; no database or production payload was used. A synthetic CSV was previewed: commit remained disabled before a valid same-hash preview, became enabled after preview, then committed against the fixture and locked again after success. This verifies UI behavior, not API/backend correctness. |
| Recoverable browser form error | The fixture returned HTTP 503 `Temporary server error` on manual snapshot submission. The error appeared in the alert and the controlled quantity input retained `12.345`. |
| Keyboard browser navigation | Starting at the brand link, two Tab presses focused “Information timeline”; Enter navigated to `/timeline`. |
| Desktop browser screenshot | [1280×900 Chrome capture](evidence/AR-503-portfolio-1280.png). The visual content viewport is 1265px because the desktop vertical scrollbar occupies 15px; horizontal scroll position is 0. |
| 320px browser viewport and screenshot | [320×844 Chrome capture](evidence/AR-503-portfolio-320.png), captured with device metrics override. `visualViewport.width`, document client width, and body width were 320px; horizontal scroll position was 0. The portfolio heading and controls wrap without clipping. |
| `task verify` | unavailable locally: `task` executable not found; hosted PR CI run 135 (`Verify`) passed, including database migration verification and `task verify`, for commit `2ba16f042bbcca041e7eda0f404985a2b2984f28` |
| Final hosted PR CI | pass: run 137 for PR #54 head `ebf3f64c6f47a0b4b33a57a5e8808fb71f5b31b6`; PR is merged. |

## Change inventory

- Files changed: portfolio API helper, route component, route stylesheet, component tests, shared root route, this report, and desktop/320px browser screenshots under `.ai/reports/evidence/`.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-503-portfolio-ui`
- Implementation commits: `61eab655c0c1e7a262a2bd05a0a32c9c11104460`, stale-context guard `83be50a`, and portfolio-switch notice cleanup `93c0dba`.
- Base SHA: `bfd36bd5e49a433dbd3657548019b18a4e2a0b58` (after AR-402 merge).
- Evidence/status commit SHA: recorded in the final handoff message.
- Remote branch: `origin/task/AR-503-portfolio-ui`, pushed; [PR #54](https://github.com/oplosy/atrisk/pull/54) merged at `ee9a61994c9b6aef5440f7b89bd6fbebdfce50f2` after hosted CI passed.
- Worktree: clean at final handoff.

## Assumptions and risks

- CSV preview and commit use the same file hash, portfolio ID, captured-at timestamp, and server token. The server remains the final validator of preview token expiry and import schema.
- Browser visual evidence covers desktop and 320px; hosted `task verify` and database migration verification passed. Local browser submissions used synthetic fixtures, not a connected API/PostgreSQL instance.
- This branch was developed in the primary checkout rather than a sibling task worktree; later task work should return the primary checkout to clean `main` and use an isolated worktree as prescribed by the execution protocol.
