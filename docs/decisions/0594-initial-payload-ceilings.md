---
status: rejected
date: 2026-08-26
phase: pre-v1
---

# 0594 — initial-payload ceilings are NOT built

## Context

[0582] settled Playwright into the stack for one job: assert the homepage's
initial JS under a declared ceiling, failing the build on regress. It was never
installed, and no second flow test was ever planned.

Two things were wrong with that framing, and both survive the rejection as
facts. The homepage is not where payload regressions land — ClientRouter [0592]
cost 16.29 KB and the footer island [0593] another 1.6 KB, both paid on ALL 80
pages, which a homepage ceiling with headroom absorbs silently. And a browser
measurement has to answer "initial means everything before WHICH moment?",
because the homepage's sandbox boots PGlite in `onMount` — 5.53 MB across four
assets. A ceiling that depends on when the measurement stopped counting is a
flaky ceiling.

## Decision

Built green, then reverted on the code-to-gain ratio — not on a defect. Nothing
ships: no ceiling, no check, and Playwright stays in the stack list.

What the reverted build was, so it is not re-derived: `scripts/initial-payload.mjs`,
~240 lines, run as the last step of `npm run build`. It walked every built page
for what the HTML names (script `src`, island `component-url`/`renderer-url`),
closed that over STATIC imports by regex over minified ESM, added the page's
inline script bytes, and gzipped each chunk to a per-page total. Two ceilings
across all pages naming none — heaviest for a new island anywhere, lightest for
growth every reader pays. Plus an 11-case vitest file over the parsing, an
eslint globals block for `scripts/**`, and two rule-file edits.

That is a lot of machinery, a hand-written import-graph walk among it, standing
behind one number that changes a few times a year and is visible in any build's
chunk list. The same judgement as [0591]: the code-to-gain ratio lost.

## Consequences

- The measurement itself was right and its numbers are kept on the roadmap
  item: counting inline scripts (Astro's island bootstrap and hydration
  directives, and [0593]'s pre-paint script) adds ~2.46 KB gzipped per page, so
  every earlier reading undercounted. The homepage is 34.76 KB gzipped /
  88.15 KB raw, not the 32.30 / 82.17 recorded before.
- Reviving this needs a cheaper shape than a hand-written walk, or a reason
  beyond one number — a real flow-test surface that Playwright would serve
  anyway, or an off-the-shelf checker that is one config file.
- Vite's manifest would have made the walk exact and short, and is not
  available: Astro overrides `vite.build.manifest` and emits nothing. Anyone
  reaching for it again can stop there.
- Dropping Playwright from website/CONVENTIONS.md's stack list was approved
  during this round and is reverted with the rest — the row's only justification
  was that a written check had replaced it.
