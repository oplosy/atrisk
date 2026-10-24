# AR-703 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-703-simplify.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-001
- Owned paths: `apps/`, `internal/`, `risk-engine/`, `scripts/`, `test/`
- Shared paths changed and justification: `none`

## Result

`needs-review`

CI `Verify` (integration, contract, E2E) has not run on this branch yet.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 behavior-preserving, within owned paths | Diff touches only `apps/`, `internal/`, `risk-engine/src`; emitted routes, JSON bodies, error codes and SQL are unchanged. |
| AC-2 tests pass unchanged | No existing test was edited; one new test file covers the new `httpx` package. |
| AC-3 skipped findings listed | See "Skipped findings" below. |

## Fixes applied

- New `apps/api/handlers/httpx` (`WriteJSON`, `DecodeJSON`) replaces the byte-identical
  `writeJSON` in 8 handlers and the `decodeJSON` bodies in 5 (body limits 2 MiB / 4 MiB
  for risk and per-handler `writeError` kept).
- `apps/api/cmd/api/main.go`: the doubled `/api/v1` + `/v1` route table is registered
  through one `route` helper (same 34 patterns plus `/`).
- `internal/application/valuation`: FX non-positive rate is classified with a sentinel
  (`errors.Is`) instead of matching `"positive"` in the error text.
- `risk-engine` scenarios: `PreShockMetricsError` (subclass of `ScenarioValidationError`)
  replaces the `startswith("pre-shock")` message check; `not key` and `_TEMPLATES`
  simplifications.
- `internal/imports`: removed a dead `json.Marshal`, an unused `rawID` parameter, and
  folded three nullable-field blocks into `nullIfEmpty`.
- `internal/sources/{binance,fred}`: `minInt` replaced by builtin `min`.
- `risk-engine/jobs/postgres.py`: hoisted a function-local import.

## Skipped findings

- All efficiency findings (batching/`ANY($1)` queries, `CopyFrom`, worker pools, Shapley
  `deepcopy`, repeated canonical-JSON hashing): they change query shape, transaction
  timing, or numerics-adjacent code and need their own task with DB-backed tests.
- Shared SQL knowledge-clause builder (valuation/scenarios), shared source-client config
  and checkpoint helpers, shared `validUUID`/`nullableString`/cursor codec, `archive.SHA256Hex`
  reuse: need a decision on the shared package location and, for SQL, byte-identical SQL tests.
- Dead Binance/TCMB pagination code and `Config.MaxPages`: public surface; removal needs
  an owner decision.
- Valuation `TRY`/`USD` overrides, `formatDecimal` family, Python price-validity reuse:
  touch valuation or risk numerics.
- Postgres queue `@contextmanager` transaction helper, web guard/fetch consolidation,
  other unreachable-branch removals that guard input validation.
- Observed, not fixed: `timeline` handler returns a plain error for a bad `from`
  (falls into the 500 branch of `writeServiceError`).

## Stop-condition check

- Decision or scope conflict: no AR-703 packet existed; one was added to satisfy AGENTS.md.
- Missing dependency, unsafe migration, or unavailable verification: integration, contract
  and E2E suites need PostgreSQL/Garage and were not run locally.

## Verification

| Command | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./apps/... ./internal/... ./test/...` | pass |
| `go test ./apps/... ./internal/...` | pass (all packages) |
| `uv run --locked pytest` (risk-engine) | 65 passed |
| `uv run --locked ruff check src tests` | pass |
| `npm run typecheck` | pass |
| `task verify` | not run locally (`task` not installed); CI will run it |

## Change inventory

- Files changed: 8 API handlers, `apps/api/cmd/api/main.go`, new `apps/api/handlers/httpx/`,
  valuation and imports services, binance/fred clients, two risk-engine modules, task packet, this report.
- Schema/API changes: none.
- Generated artifacts: none.

## Assumptions and risks

- Route equivalence in `main.go` was checked by pattern count/handler mapping, not by an
  integration test.
