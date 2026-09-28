---
id: AR-501
title: Build the web shell and quality language
status: ready
phase: 5
depends_on: [AR-107]
branch: task/AR-501-web-shell-quality
base_sha: 7a562ff3d77fd60b6d60163ba28df30b3cb81396
owned_paths: [apps/web/src/App.tsx, apps/web/src/App.test.tsx, apps/web/src/app/, apps/web/src/components/, apps/web/src/routes/root/, apps/web/src/styles.css]
shared_paths: [apps/web/package.json, apps/web/src/generated/]
adrs: [ADR-001, ADR-009, ADR-011]
---

# AR-501: Build the web shell and quality language

## Outcome

The React application has accessible navigation, a coherent non-generic visual
system, generated API integration, and explicit loading/empty/error/quality states.

## In scope

- Responsive shell, routes, tokens, typography, focus states, semantic components,
  cutoff/as-of controls, quality badges/banners, error boundary, and query setup.
- WCAG AA contrast and reduced-motion behavior.

## Out of scope

- Domain screens or client-side financial calculations.

## Acceptance criteria

- [ ] Keyboard users can reach and understand all shell controls and focus states.
- [ ] Valid/degraded/blocked are distinguishable by text/icon, not color alone.
- [ ] Empty, loading, stale, offline, unauthorized-proxy, and server-error states exist.
- [ ] No business calculation or duplicated API type is implemented in the client.
- [ ] Desktop and mobile component tests/screenshots show no overflow or hidden status.

## Required verification

```text
npm test -- --run
npm run typecheck
npm run build
npm run lint
npm run format:check
```
