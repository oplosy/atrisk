# AR-501 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-501-web-shell-quality.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-001, ADR-009, ADR-011
- Owned paths: `apps/web/src/App.tsx`, `apps/web/src/App.test.tsx`, `apps/web/src/app/`, `apps/web/src/components/`, `apps/web/src/routes/root/`, `apps/web/src/styles.css`
- Shared paths changed and justification: `none`; the existing generated TypeScript contract is imported as the API type source and was not edited.

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | Semantic navigation has visible labels, `aria-current`, labelled as-of controls, keyboard focus outlines, responsive navigation, and browser back/forward handling in `App.tsx` and `RootRoute.tsx`. |
| AC-2 | `QualityBadge` renders icon plus explicit `Valid`, `Degraded`, or `Blocked` text; quality state is not conveyed by color alone. |
| AC-3 | `StatusPanel` covers loading, empty, stale, offline, unauthorized-proxy, server-error, and generic error states with visible copy and retry behavior. `useApiResource` preserves the last good payload and exposes `stale` after a failed retry. `ErrorBoundary` provides a refresh state. |
| AC-4 | `api.ts` imports `PortfolioPage` and `ErrorEnvelope` from `contracts/generated/typescript/contracts`; runtime guards reject malformed 200 responses and portfolio items missing or blank `id`/`name`, invalid `reporting_currency`, non-object `metadata`, or missing/blank `created_at`/`updated_at`. No client-side financial calculation or duplicate API model was added. Empty/missing portfolio data is blocked, not healthy. |
| AC-5 | Vitest covers shell navigation, evidence controls, quality language, malformed API payloads, stale retry behavior, and all API state variants (5 files, 21 tests). CSS includes desktop/mobile layouts, overflow-safe navigation, audited small-text contrast tokens, and reduced-motion behavior. Verified HTTP 200/title `AtlasRisk` in configured Vite preview, then actual installed headless Chrome captured `C:\tmp\atrisk-ar501\desktop-final.png` at 1440x900, `C:\tmp\atrisk-ar501\mobile-final-390.png` at 390x844, and `C:\tmp\atrisk-ar501\mobile-final-320.png` at 320x844. Chrome DevTools measurements showed document/body widths 1425/1425 at 1440 (15px scrollbar), 375/375 at 390, and 320/320 at 320; mobile nav bounds were x16..359 (390) and x16..304 (320), every mobile nav item had `scrollWidth === clientWidth`, and controls ended at x342 (390) / x287 (320), with no page-level horizontal overflow. |

## Stop-condition check

- Decision or scope conflict: the original packet omitted the existing `src/App.tsx`, `src/App.test.tsx`, and `src/styles.css` entry files. The orchestrator amended the packet and committed the scope update as `a4925e8` before implementation continued.
- Missing dependency, unsafe migration, or unavailable verification: `none`.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run` | pass; 5 test files, 15 tests |
| `npm run typecheck` | pass |
| `npm run build` | pass; Vite production bundle built |
| `npm run lint` | pass |
| `npm run format:check` | pass |

## Change inventory

- Files changed: web shell entry/test/styles plus `app/api.ts`, `app/api.test.ts`, `app/useApiResource.ts`, `app/useApiResource.test.tsx`, `components/ErrorBoundary.tsx`, `components/QualityBadge.tsx`, `components/StatusPanel.tsx`, `components/StatusPanel.test.tsx`, and `routes/root/RootRoute.tsx`.
- Schema/API changes: none; read-only portfolio query uses the existing generated contract types.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-501-web-shell-quality`
- Implementation SHA: `40b164a` (final viewport fix)
- Report-only commits follow the implementation commit; they do not change runtime code.
- Remote branch: not pushed; awaiting orchestrator push authorization
- Worktree: clean after commit

## Assumptions and risks

- Domain screens remain placeholders by design; AR-502 through AR-505 own domain UI work.
- The viewport screenshots are local verification artifacts under `C:\tmp`; they are not committed to the repository.
- The local API may be unavailable during development; that condition is surfaced as an explicit offline state and does not present healthy risk data.
