---
status: accepted
date: 2026-08-26
phase: pre-v1
---

# 0593 — the night board, chosen from the footer

## Context

[0583] kept visited-purple links and deferred dark mode to a "Board style"
dropdown, not a system toggle. The footer already rendered the chrome for it —
`Board style: Vulkan Classic ▾` as an inert chip — and the donor was left open.

The tokens layer was ready: all 42 component stylesheets carry zero raw colours
(stylelint's `declaration-strict-value` enforces it), so no component has to
change for a second palette to exist.

## Decision

A second board style, `night`, selected by a real `<select>` in the footer and
carried on `<html>` as `data-board-style`.

- The donor is the era's dark-board convention rather than the site's own
  volcano: near-black content ground under navy chrome, silver text, accents
  lifted until they carry. Amber is NOT part of the palette swap — "amber means
  new-or-act and nothing else" is [0583]'s rule, and a style may not give that
  mark a second meaning, so the night sheet inherits the classic amber
  primitives untouched and only darkens the grounds they sit on.
- A style is its own primitive sheet plus a re-point of the semantic names the
  components already consume; the primitive names stay honest about their own
  colours (`--surface-coal`, `--band-slate-start`, `--link-sky`) instead of one
  hue-named sheet lying under two palettes. Everything structural — the grid,
  the z-index scale, the fonts — is style-independent and stated once.
- Shiki is the ONE thing a token swap cannot reach: it highlights at build time
  and would bake one style's colours into every fence. Both themes now ship
  with `defaultColor: false`, so Shiki writes `--shiki-light` / `--shiki-dark`
  per token and the base layer picks the side. That is a board style read
  outside the tokens layer, and website/CONVENTIONS.md records it as the one
  sanctioned crossing.
- An `is:inline` script in `BoardLayout`'s `<head>` puts the style on `<html>`
  before the first paint, and again on `astro:after-swap`: ClientRouter [0592]
  copies the incoming document's `<html>` attributes, and the incoming document
  is the static build, which carries none. It re-reads storage each time, so a
  choice made in the footer survives the next navigation.
- Until a reader chooses, `prefers-color-scheme` picks the opening style. Once
  they choose, the stored value outranks the operating system for good — the
  system seeds, it does not toggle.

## Consequences

- Contrast in night matches the style it mirrors rather than improving on it:
  ink-faint reads 3.50 against its ground in both sheets, the SQL comment 3.68
  against classic's 3.31, the null cell 2.70 against classic's 1.99. Raising
  any of them is one change touching both sheets, never a night-only fix that
  makes the two styles disagree.
- `BoardFooter` is an island now (`client:idle`) — a `<select>` in static HTML
  does nothing. Cost is 1.6 KB raw / 0.8 KB gzipped on every page; the Svelte
  runtime was already there for the visit bar.
- A third style is another primitive sheet plus another ~65 semantic
  re-points, kept in sync by hand. Two is the settled menu.
- Storybook gets the style as a toolbar global rather than a second story per
  component: every component has as many looks as the board has styles, and
  doubling 40-odd story files to say so is not a checklist, it is noise.
