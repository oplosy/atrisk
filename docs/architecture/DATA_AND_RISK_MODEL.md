# Data and Risk Model

## Time semantics

AtlasRisk preserves three clocks instead of calling every timestamp `published`:

1. **Observation time:** the economic period or market timestamp represented by
   the value.
2. **Source knowledge time:** when the upstream source says the value became
   available or valid. This may be absent for sources that do not expose it.
3. **System knowledge time:** when AtlasRisk successfully persisted the exact raw
   payload and normalized revision.

`source-as-of(T)` uses source knowledge time and is available only when the source
provides defensible vintage metadata. `system-as-of(T)` uses system knowledge time
and always answers what this installation had actually captured by `T`. The UI
labels them explicitly.

## Append-only ingestion model

Core entities:

- `data_source`: provider identity and adapter version
- `dataset`: provider dataset/release identity
- `series`: source code, unit, frequency, seasonal adjustment, timezone, and
  freshness policy
- `ingestion_run`: request window, adapter version, outcome, and coverage
- `raw_object`: content hash, object key, media type, byte length, retrieval time
- `observation_revision`: series, observation time, exact value/text, source
  knowledge time, system knowledge time, raw object, and quality flags

An observation revision is immutable. Re-fetching identical content is idempotent.
A changed value creates a new row. `latest` is a view/projection that selects the
newest eligible revision; it is not a table updated over the historical record.

For FRED/ALFRED, source real-time intervals and vintage dates are preserved. For
TCMB or manual inputs without a trustworthy publication timestamp,
`source_known_at` remains null and `knowledge_time_basis` records
`first_observed_by_system`; AtlasRisk does not fabricate publication precision.

## Raw archive identity

Raw objects use a SHA-256 content address and immutable object key. Metadata binds
the object to request URL template, non-secret parameters, response headers,
adapter version, and ingestion run. Secrets and API keys are removed before
persistence. Normalized records retain the raw-object identifier.

## Instrument and portfolio model

- `instrument`: stable internal identity, type, native currency, external IDs,
  and lifecycle status
- `account`: broker/manual identity without credentials
- `portfolio`: reporting container and default base currency
- `position_snapshot`: immutable as-of snapshot and source
- `position_line`: instrument, quantity, optional cost basis, and supplied risk
  attributes such as duration
- `price_revision`: instrument quote, timestamp, source/system knowledge clocks,
  and raw provenance
- `fx_quote_revision`: base, quote, timestamp, clocks, and provenance
- `reconciliation_checkpoint`: externally stated NAV and difference from the
  AtlasRisk valuation

Manual corrections create a new snapshot or revision linked to the superseded
entry. They do not edit history.

## Valuation policy

- Persist quantities, monetary values, prices, and FX rates as `NUMERIC(38,18)`.
- Compute exact line value as quantity multiplied by selected price.
- Select only quotes at or before the valuation cutoff and within the
  instrument-specific freshness policy.
- Convert through a deterministic path chosen from configured direct pairs, with
  USD as the only V1 bridge currency. Never choose a path silently by best rate.
- Persist every selected `price_revision_id` and ordered `fx_quote_revision_id`
  path on the valuation line.
- Produce TRY and USD reports from the same native valuation snapshot.
- Default reconciliation tolerance is the greater of 0.01 base-currency units or
  one basis point of external NAV; an account may override it explicitly.

A line with no eligible price is `unpriced`. A portfolio containing a required
unpriced line is `blocked`, not partially presented as complete.

## Data quality model

Every required input is classified as:

- `fresh`: within its declared availability/freshness policy
- `stale`: present but older than policy permits
- `missing`: no eligible revision exists
- `partial`: an expected period/window is incomplete
- `suspect`: validation or source-consistency rule failed
- `revised`: a later value differs from the previously known value

Every calculation has one of three states:

- `valid`: all required inputs satisfy policy
- `degraded`: calculation is useful but optional inputs are stale/partial
- `blocked`: a required input or mapping is absent/suspect

The API returns structured reason codes and affected series/instruments. The web
UI must render the state next to the result and must not use success styling for
`degraded` or `blocked`.

## Risk calculation policy

Risk functions are deterministic and pure for a versioned input bundle.

- Valuation base: both TRY and USD
- Return type: log return
- Alignment: common weekday observation grid for cross-asset calculations
- Default timezone/cutoff: UTC daily close; source-specific close is normalized
  and retained
- Volatility: 63-observation sample standard deviation, annualized by `sqrt(252)`
- Correlation: 252-observation pairwise window with at least 60 overlapping valid
  returns; coverage is included with every coefficient
- Drawdown: peak-to-trough decline of the reconstructed base-currency NAV series
- Concentration: position weights, top-1/top-5 share, and Herfindahl-Hirschman index
- Gross leverage: sum of absolute exposures divided by NAV
- Net leverage: signed exposure divided by NAV

No forward fill is allowed for tradable prices when computing returns. Weekend
crypto observations are excluded from cross-asset matrices in V1 so business-day
assets do not acquire artificial zero returns. Crypto-only analysis may use a
separate seven-day calendar and `sqrt(365)`, explicitly labeled in the result.

Macro series are evidence and scenario context. AtlasRisk does not automatically
infer that a macro series is a tradable risk factor or causal driver.

## Scenario and attribution model

- `scenario`: stable identity and owner
- `scenario_version`: immutable factor shocks, mappings, assumptions, and units
- `stress_run`: engine version, scenario version, portfolio snapshot, market-data
  snapshot, state, and timestamps
- `stress_position_result`: pre/post value and P&L per position
- `stress_factor_result`: factor contribution and method
- `stress_metric_result`: pre/post volatility, correlation, leverage, and other
  non-P&L measures

Spot and linear fixed-income positions are fully revalued under the scenario.
Position P&L must sum exactly to total P&L within recorded decimal tolerance.
Factor attribution uses Shapley allocation across the small V1 factor set when
factors interact. Any unsupported/nonlinear remainder is shown as an interaction
residual; it is never hidden or assigned arbitrarily.

Volatility and correlation shocks change post-shock risk metrics. They do not
invent cash P&L for spot holdings. Instruments whose scenario mapping is missing
are listed and may block the run according to the scenario's coverage policy.

## Decision journal model

A `decision` records thesis, alternatives, evidence, invalidation conditions,
horizon, risk budget, and intended action without executing it. Creating the
decision seals references to:

- portfolio snapshot
- market/macro data snapshot and cutoff semantics
- valuation result
- relevant scenario versions and runs
- engine and contract versions

Reviews and outcomes are append-only `decision_review` records. Corrections are
linked amendments, not edits to the original evidence.
