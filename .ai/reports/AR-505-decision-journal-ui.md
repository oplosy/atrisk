# AR-505 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-505-decision-journal-ui.md`
- Packet status at start: `active` (activated by the orchestrator after the ready gate)
- Referenced ADRs: ADR-002 (decision support), ADR-007 (append-only history), ADR-022 (LLM enrichment deferred)
- Owned paths: `apps/web/src/features/journal/`, `apps/web/src/routes/decisions/`
- Shared paths changed and justification: `apps/web/src/App.tsx`, `apps/web/src/routes/root/RootRoute.tsx`, and `apps/web/src/App.test.tsx` were changed only to register and test the approved `/decisions` route. The merged AR-504 `/risk` files from `origin/main` were preserved while resolving the route-shell merge.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `DecisionsRoute.tsx` renders a required-evidence finalization preview for portfolio snapshot, valuation run, and risk run references, with the sealed manifest shown after server validation. Draft POST is blocked until all three IDs exist, and empty optional evidence rows are omitted from the request; covered by `DecisionsRoute.test.tsx`. |
| AC-2 | Original thesis/evidence, sealed evidence, and later review/amendment events use separate labels and sections in `DecisionsRoute.tsx`; reconstruction is covered by the timeline rendering path. |
| AC-3 | `EVIDENCE_INTEGRITY_FAILURE` and `EVIDENCE_INCOMPLETE` responses set a blocking error, disable finalization, and explicitly state that no latest-data fallback is used. Covered by the integrity-block test. |
| AC-4 | Intended action is rendered as a journal statement with the visible boundary text: `This is a journal statement, never an executable control.` No execution API or control is present. Covered by the draft-boundary test. |
| AC-5 | Drafts are recovered from the versioned local-storage key, required fields are validated before the first POST, and saved drafts/evidence are locked against divergence or duplicate POSTs. Covered by the draft/recovery, pre-save validation, and evidence-lock tests. |

### Browser evidence

- CUA desktop pass on `http://127.0.0.1:5173/decisions`: the page rendered the journal shell, the no-execution boundary, three populated immutable evidence references, and the finalization preview listing portfolio snapshot, valuation run, and risk run IDs.
- After reload, the same page showed `Recovered local draft` and restored the entered decision/evidence values. Repeated `Tab` navigation moved focus through the accessible navigation controls.
- Independent browser verification at a 320px viewport measured `viewport/document/body = 320px` with no horizontal overflow. The screenshot API timed out, so no image artifact was captured. Responsive rules are present in `apps/web/src/routes/decisions/decisions.css`.
- Finalization integrity failure and historical reconstruction use deterministic fetch mocks in `apps/web/src/routes/decisions/DecisionsRoute.test.tsx`; no backend or Docker service was started for browser evidence.

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: `task verify` cannot run because the Task CLI is not installed; `uv run --locked ruff ...` cannot run because the locked environment does not expose `ruff`. Docker-backed checks were not started, per instruction.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run apps/web/src/routes/decisions apps/web/src/App.test.tsx` | pass; 2 files, 8 tests |
| `npm test -- --run` | pass; 10 files, 55 tests |
| `npm run typecheck` | pass |
| `npm run build` | pass; Vite production build |
| `npm run lint` | pass |
| `npm run format:check` | pass |
| `node scripts/verify/check-go-format.mjs` | pass; 80 files |
| `go vet ./apps/... ./internal/...` | pass |
| `go test ./apps/... ./internal/...` | pass |
| `node --test scripts/verify/check-generated.test.mjs scripts/verify/check-scope.test.mjs` | pass; 7 tests |
| `node --test test/contract/contract.test.mjs` | pass; 11 tests |
| `node --test contracts/jobs/contract.test.mjs` | pass; 4 tests |
| `node scripts/verify/check-generated.mjs` | no content drift; generator reported only stale Git stat entries, cleared by refreshing the index |
| `uv run --locked ruff format --check src tests` | unavailable; `ruff` executable not found |
| `task verify` | unavailable; `task` executable not found |

## Change inventory

- Files changed: journal API/client and route files under `apps/web/src/features/journal/` and `apps/web/src/routes/decisions/`, plus approved route registration/test files. Reconstructed decisions hydrate the original server evidence into the preview. `origin/main` AR-504 risk files are present through the required merge.
- Schema/API changes: none; the UI consumes the existing decision, evidence, review, amendment, and timeline endpoints.
- Generated artifacts: no generated content changed; generated-file verification passed after Git index refresh.

## Git state

- Branch: `task/AR-505-decision-journal-ui`
- Implementation commit: `6340e69` (API-contract validation/evidence-lock fix)
- Prior report commit: `da55def`
- Current code HEAD: `fd3d9e3` (blank optional evidence omission and request-body regression test)
- Remote branch: published as `task/AR-505-decision-journal-ui` through the GitHub Contents API; the remote commit graph is a file-by-file transfer from the verified local task tree
- Worktree: clean

## Assumptions and risks

- Browser evidence is complete for desktop, keyboard navigation, draft recovery, finalization preview, and independent 320px no-overflow measurement. Mocked browser reconstruction/integrity-error screenshots remain unavailable because this worker browser surface has no page-mutation/mock injection API; deterministic Vitest fetch mocks cover both states.
- Hosted CI and PostgreSQL-backed runtime verification are pending on the pull request. The local browser pass covered the rendered draft and preview, recovery, keyboard focus, and 320px overflow; reconstruction and integrity-block behavior have deterministic Vitest coverage but were not browser-tested against a live backend.
