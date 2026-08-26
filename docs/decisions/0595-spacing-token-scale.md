---
status: accepted
date: 2026-08-26
phase: pre-v1
---

# 0595 — spacing token scale: exact values, pixel-value names, one tier

## Context

website/CONVENTIONS.md has declared spacing a closed token scale since
the CSS rules were written, but every component stylesheet still carried
raw px. The values in use are hand-tuned pixel-era spacing: every
integer 1–14, then 16, 18, 20, 22, 24, 26, 32, 34 — 22 distinct steps
across 36 component stylesheets, three layout scoped blocks, and the
global base/utilities layers.

## Decision

- The scale IS the set in use: `--space-1` … `--space-34`, each step
  named by its pixel value, defined once in the tokens layer beside the
  z-index scale. No snapping to a sparser ladder — the sweep is
  behavior-preserving; merging 9 into 8 or 13 into 12 is a visible
  design change that would need eyeballing every spot, and nothing asked
  for a redesign. Consolidation stays available later as its own pass,
  now enumerable in one place.
- Pixel-value names, not size names (`--space-sm`) — with 22 steps a
  size ladder is meaningless, and the naming rule is the thing it
  literally is. The name spells the value, so a step's meaning never
  drifts from its label.
- One tier, like `--z-raised`: spacing is structural, stated once,
  style-independent — a board style never redefines it.
- Scope is the spacing properties only: margin, padding, gap and their
  longhands. Positioning coordinates (the pixel-art components'
  top/left), border widths, and sizes stay raw px — they are drawing and
  sizing, not spacing.
- Enforcement: `/^margin/`, `/^padding/`, `/gap$/` join the
  declaration-strict-value property list; `0` and `auto` join
  ignoreValues. The one negative use writes
  `calc(-1 * var(--space-34))`.

## Consequences

- Zero visual change; full site verify floor green.
- A new spacing value cannot enter silently — stylelint fails the build
  until a step is added to the scale deliberately. Sabotage-checked:
  the rule catches raw px in shorthands mixed with tokens
  (`margin: var(--space-8) 0 3px`) and in logical properties
  (`padding-inline`).
- The layout `.astro` scoped blocks are swept but outside stylelint's
  `src/**/*.css` glob — review carries them.
- The scale is wide (22 steps). That is the honest state of the design,
  not a target; shrinking it is a future design decision, not cleanup.
