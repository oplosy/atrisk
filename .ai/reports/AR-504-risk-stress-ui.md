# AR-504 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-504-risk-stress-ui.md`
- Packet status at start: `blocked` on AR-306; resumed as `active` after PR #53 merged.
- Referenced ADRs: ADR-011, ADR-013, ADR-014, ADR-015 (accepted in `docs/decisions/README.md`)
- Owned paths: `apps/web/src/features/risk/`, `apps/web/src/routes/risk/`
- Shared paths changed and justification: `apps/web/src/app/api.ts` adds a generic request-init path so the risk feature can submit an idempotent POST while preserving shared error handling; existing root-route mounting changes remain from the initial implementation.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Explicit lifecycle and quality | `lifecycleLabel` and route tests cover queued, running, retryable, permanent, cancelled, and completed healthy/degraded/blocked states. The route polls active jobs and surfaces returned error/reason codes. |
| Reconciled total, positions, factors, residual | Strict parser accepts server Shapley attribution only when decimal evidence is present. A server `reconciles: true` claim is downgraded when output/quality/attribution/position state is blocked or degraded, or required decimal values are incomplete. A visual contribution waterfall is paired with an accessible exact-value table; the UI does not recompute risk values. Tests cover contradictory and malformed attribution. |
| Coverage and calendar | Pre/post metric tables display observations, required observations, overlap, state, calendar, annualization factor, drawdown, leverage, and concentration when present. |
| Unmapped instruments | Dedicated warning lists every `unmapped_instruments` entry; blocked positions remain visible and no full-portfolio total is shown. |
| Accessible dense views | Semantic tables use captions and header scopes, scroll on narrow screens, and attribution/provenance details use native `<details>`. At 320px, document/body width stayed 320px with no horizontal overflow; keyboard Tab reached Snapshot ID with visible focus. |
| Run submission and immutable template version | Form requires account, snapshot, and valuation IDs; selects a supported template and sends versioned configuration. It sends no client-authored positions or pre-metrics and uses an idempotency key. Recoverable errors preserve values and reuse the key for the same request. |
| Async selection consistency | Deferred POST tests confirm a late create response cannot replace a different selected run and that a retry after that stale response reuses the same idempotency key. |
| Sealed valuation provenance | Drawer includes account/snapshot/valuation IDs, valuation state/cutoff/knowledge timestamps/result hash, and source-provided price revision and TRY/USD FX quote lineage. |

## Stop-condition check

- Decision or scope conflict: none. The UI follows the sealed-input boundary and consumes server-generated attribution; no browser-side valuation or risk formulas were added.
- Missing dependency or unavailable verification: AR-306 PR #53 and AR-503 PR #54 are merged and included in the rebase. API-backed browser submission and hosted `task verify` remain pending. The PostgreSQL service reports running, but its listener (`5432`), Garage listeners (`3900`/`3902`), and API listener (`8080`) are closed; no service or Docker container was started.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run` | pass: 49 tests in 9 files, including selection/idempotency races, contradictory attribution, and provenance coverage |
| `npm run typecheck` | pass after review fixes |
| `npm run build` | pass after review fixes; Vite transformed 43 modules |
| `npm run lint` | pass after review fixes |
| `npm run format:check` | pass after review fixes |
| Hosted PR CI | Initial run 145 failed at web formatting due CRLF transferred from Windows; remote web files were normalized to LF. A new Verify run is pending after this report update. |
| Independent code review | pass after latest fixes: stale-success retries retain their idempotency key; status contradictions fail closed; arithmetic reconciliation remains engine-owned. |
| `node C:\Users\mesut\.agents\skills\impeccable\scripts\detect.mjs --json apps/web/src/features/risk/riskApi.ts apps/web/src/features/risk/riskApi.test.ts apps/web/src/features/risk/riskResult.ts apps/web/src/features/risk/riskResult.test.ts apps/web/src/features/risk/scenarioTemplates.ts apps/web/src/routes/risk/RiskRoute.tsx apps/web/src/routes/risk/RiskRoute.test.tsx apps/web/src/routes/risk/risk.css` | pass, `[]` |
| `git diff --check` | pass |
| `/risk` local browser, 1280×900 | Scenario creation, template selector, configuration disclosure, existing-run inspection, and empty state rendered; screenshot was visually inspected during this session. |
| `/risk` local browser, 320×844 | Form controls stack; viewport, document, and body widths were 320px; horizontal overflow was false. |
| Browser keyboard check | Tab navigation reached Snapshot ID with a visible focus indicator. |
| API-backed browser scenario | not run: no local API, PostgreSQL, or Garage listener is available, and Docker was not started. AR-306 is now in the branch base; component/API contract tests cover submission, retry, server attribution, and malformed-result behavior. |

## Change inventory

- Files changed: shared API helper, risk API reader/submission, versioned template presets, result/attribution parser, risk route/styles, tests, and this report.
- Schema/API changes: no backend schema changes; the client targets AR-306's sealed-run contract and never sends positions or pre-metrics.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-504-risk-stress-ui`
- Base SHA: `ee9a61994c9b6aef5440f7b89bd6fbebdfce50f2` (`main` after AR-306 PR #53 and AR-503 PR #54 merged).
- Implementation commit: `5b5099aee129f3e22cfabf192a163f0b2b7415c5` (`feat(risk): add sealed scenario and attribution UI [AR-504]`).
- Contract-fixture fix: `8579863b88d3d29ac501a7690966f5052823ca13` adds the now-required `valuation_id` to risk-run test fixtures.
- Remote branch: `task/AR-504-risk-stress-ui` is published on `oplosy/atrisk`; [PR #57](https://github.com/oplosy/atrisk/pull/57) is open against `main`. Its content tree is sourced from reviewed local commit `adc012be3f9b1db1b8817f83c6e53cdc502021fa` and applied to remote `main` at `ac8a92d3b85bab739f612928657f3dea59083eaa`. GitHub API Contents commits were used because the local GitHub CLI token is invalid; the remote commit graph therefore differs from local worktree history. Web files were normalized to LF after hosted CI run 145 identified CRLF from the Windows transfer. The local worktree remains clean; the latest hosted verification and API-backed browser submission testing are pending.
- Worktree: clean after the review-fix commit; no upstream tracking branch is configured.

## Assumptions and risks

- Template presets mirror the risk-engine's versioned template settings. The server-stored scenario content hash and version remain authoritative.
- Unmapped instruments remain blocked and visible; no inferred mapping is created.
- Before delivery: run API-backed submission/result browser checks when API/PostgreSQL/Garage are available and run hosted `task verify` after an authorized branch push/PR.
