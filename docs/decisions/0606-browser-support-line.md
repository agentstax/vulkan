---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0606 — the doc site's browser support line

## Context

Nothing declared what browsers the site supports; the real floor was
Vite's default build target, which floats per major — Vite 7 set
chrome107/safari16, the Vite 8 that Astro 7 bundles set
chrome111/safari16.4, so the floor had already moved once with no
deliberate decision. Usage research (caniuse-lite, 2026-08): Baseline
Widely Available — Chrome/Edge 121, Firefox 123, Safari/iOS 17.2 —
covers 87.0% of tracked global users, more than browserslist
`defaults` (84.7%), which spends its budget on Chrome 109 (the last
Windows 7 build, 0.58%), UC Browser (0.68%), and Opera Mini — traffic
a developer doc site rarely sees. Everything below the Baseline line
totals ~4-5%. A full feature inventory found no shipped feature broken
at that line and an existing graceful fallback on every enhancement
above it.

## Decision

- Two declared lines in website/CONVENTIONS.md ## Browser support:
  supported = Baseline Widely Available (the 30-month
  interoperability definition, re-read when the site is worked on);
  readable = the pinned build floor.
- `vite.build.target` pinned in astro.config.mjs to Vite 8's
  baseline-widely-available list (chrome111, edge111, firefox114,
  safari16.4, ios16.4) so a toolchain major cannot move the floor
  silently.
- Enhancements above the supported line stay sanctioned only with
  the fallback they already carry: view transitions (Safari 18,
  Firefox 144) fall back to Astro's instant swap;
  requestIdleCallback (Safari 18) to setTimeout; the PGlite sandbox
  to its boot notice with Reset as the retry.
- Below the floor: CSS's own error recovery and nothing else — no
  @supports, no polyfills, no legacy bundle. The structural cliff
  (@layer, :where(), :focus-visible) sits at Safari 15.4/Chrome 99,
  under 0.2% of usage.
- Rejected: browserslist `defaults` (less coverage, wrong audience);
  Baseline Newly Available (76%, trades real users for features the
  site does not use); compat lint tooling (eslint-plugin-compat,
  stylelint-no-unsupported-browser-features) — offered and declined,
  the inventory is a review-time job, not a standing gate.

## Consequences

- Upgrading Astro or Vite no longer changes what older browsers
  receive; raising the floor is an edit to the pinned list plus a
  successor to this record.
- The supported line is a moving definition (Baseline advances
  monthly); the pinned floor is not — the gap between them widens
  until a future decision re-pins.
