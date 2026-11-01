# Onboarding Notes

Quick path for new contributors joining the risk platform.

## Start here

1. Read `README.md` for the project overview.
2. Skim `AGENTS.md` and `CONTRIBUTING.md` for working conventions.
3. Look at `risk-engine/` for the core evaluation loop and `contracts/` for the data contracts between services.
4. Run the local test suite before changing anything, to see the current baseline.

## Things people tend to trip on

- Go code lives under `risk-engine/` and `internal/`; the module boundary matters.
- Contract changes must go through `contracts/` first, code second.
- `Taskfile.yml` is the entry point for most chores — prefer it over raw commands.
