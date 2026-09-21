# Product Definition

## Product statement

AtlasRisk is a single-user, self-hosted decision-support system. It preserves
what data was available at each decision point, values a manually maintained
portfolio in TRY and USD, computes deterministic risk measures, runs repeatable
stress scenarios, and binds the resulting evidence to an investment journal.

It is not a signal generator, brokerage terminal, accounting ledger, or execution
system.

## Primary user and operating model

- One owner operates one installation.
- The default deployment binds to localhost and runs through Docker Compose.
- V1 has no multi-tenancy, role hierarchy, sharing, or public registration.
- Remote exposure requires an external TLS reverse proxy and OIDC-aware access
  proxy. AtlasRisk will not invent a password system in V1.
- The system is useful without an LLM or any paid AI API.

## V1 user journeys

### 1. Inspect the information timeline

The user selects a macro or market series and sees observations, revisions,
release/knowledge times, ingestion times, freshness, and raw-source provenance.
The user can switch between latest, source-as-of, and system-as-of views.

### 2. Record and reconcile a portfolio

The user records positions manually or imports a documented CSV template. The
system values each line in native currency, TRY, and USD. Every selected price
and FX conversion is inspectable. A reconciliation checkpoint compares AtlasRisk
NAV with a user-entered broker/manual total and displays the absolute and relative
difference.

### 3. Understand portfolio risk

The user sees exposure, historical volatility, correlation, drawdown, gross/net
leverage, weight concentration, and data coverage. Unsupported or stale inputs
produce `degraded` or `blocked`, never a healthy result.

### 4. Run and explain a stress scenario

The user selects a versioned scenario, runs it against an immutable portfolio and
market-data snapshot, and receives total loss, position-level loss, factor
attribution, interaction residual, and post-shock risk metrics.

### 5. Record and review a decision

The user records thesis, evidence, invalidation conditions, horizon, risk budget,
and expected scenarios. AtlasRisk binds immutable snapshots to the decision. A
later review is appended; it does not rewrite the original decision.

## Supported V1 instruments

- Cash and currency balances
- Binance Spot crypto instruments
- Manually priced spot instruments with current and historical price CSV data
- Fixed-rate bonds only when duration and optional convexity are supplied

Derivatives, options Greeks, futures margin, short borrow mechanics, tax lots, and
corporate actions are outside V1. A recorded instrument without sufficient price
or factor data remains visible but its risk computation is blocked.

## V1 data sources

- TCMB EVDS macroeconomic and FX series
- FRED/ALFRED macroeconomic series and vintages
- Binance Spot public market-data endpoints only
- User-supplied portfolio and price-history CSV files

ECB, broker synchronization, private exchange endpoints, and TradeLedger
integration are deferred. The domain model preserves stable account, instrument,
portfolio-snapshot, and decision identifiers so they can be connected later.

## Default stress templates

Templates are versioned data, not hard-coded formulas. Users may copy a template
and create a new version; a prior run always references its original version.

1. **TRY depreciation:** USD/TRY increases 25%; other cross-currency moves are
   derived from explicit FX paths.
2. **Rates up:** TRY curves shift +500 basis points and USD curves +200 basis
   points. Fixed-rate bonds use supplied modified duration and convexity.
3. **Risk-off:** crypto prices fall 40%, equity-like instruments fall 20%,
   applicable volatility multipliers become 2.0, and off-diagonal correlations
   move toward 0.75. Unmapped instruments are reported, not silently treated as
   zero-risk.

## Success evidence

- A revised source value creates a new observation revision and the older value
  remains queryable under both source-as-of and system-as-of semantics.
- A portfolio valuation can be reproduced from stored position, price, and FX
  quote identifiers and reconciled within a configured tolerance.
- A required stale or missing input changes the run state and visible UI warning.
- Stress loss reconciles from total to position and factor attribution, with any
  nonlinear interaction reported explicitly.
- A decision review can reconstruct the exact data, portfolio, scenario, and
  engine versions that were attached when the decision was recorded.

## Explicit exclusions

- Trade recommendations, forecasts, and automatic order execution
- Broker/exchange credentials and private trading APIs
- LLM-generated prices, returns, shocks, or risk numbers
- VaR/CVaR, Monte Carlo simulation, optimization, and backtesting in V1
- Multi-user SaaS, mobile applications, and real-time tick processing

The term `LLM-assisted analysis` replaces the ambiguous draft term `Jev`. If
`Jev` was intended to name a different technology, that requires a new decision.
