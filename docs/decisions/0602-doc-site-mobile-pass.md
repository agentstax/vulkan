---
status: accepted
date: 2026-08-27
phase: pre-v1
---

# 0602 — doc site mobile pass: two breakpoints, sandbox gated off phones

## Context

The site was built desktop-first: two width media queries existed in
the whole tree, and a three-agent sweep found the same failure
everywhere — fixed-width tracks
(`--grid-board-columns`, the posts' 150px author column) forcing ~460px
of content into the ~286px a 390px phone leaves inside the page frame,
plus non-wrapping flex rows, unwrapped wide content, sub-20px touch
targets, and fixed overlays sized past the viewport. The sandbox is a
special case: its island boots PGlite in onMount, so hydration itself
is a ~16MB download, and a SQL console is unusable at phone widths
regardless.

## Decision

- Two declared breakpoints, stated in website/CONVENTIONS.md ## CSS
  (media queries cannot read custom properties, so they are convention,
  not tokens): **640px** — layout collapse (board rows, author column,
  the cookie notice's removal); **761px** — the sandbox gate, matching
  the sandbox's existing one-column panel collapse.
- The sandbox never loads below the gate: the homepage's whole
  intro-plus-sandbox post is one route-local component hydrated with
  `client:media="(min-width: 761px)"` and CSS-hidden below it — the
  island's JS, and therefore PGlite, is never fetched on mobile. No
  replacement copy; a dedicated hero section (planned) will be the
  all-widths answer for that slot. `client:media` joins
  `client:visible`/`client:idle` as a sanctioned trigger in
  website/CONVENTIONS.md ## Islands & loading, for islands whose
  content is viewport-gated.
- The cookie notice does not render on phones (hidden below 640px) —
  the joke stays a desktop bit rather than a bar eating a third of a
  phone screen.
- Touch-target and input sizing corrections apply under
  `@media (pointer: coarse)`, not a width query, so the desktop
  aesthetic is untouched on fine-pointer machines: control padding
  bumps, and inputs at 16px so iOS stops zooming the page on focus.
- Site-notice outranks the cookie notice: the z scale gains a named
  step above `--z-raised` so the failure surface always paints over
  the consent bar.

## Consequences

- Everything rendered only inside the sandbox (produce-message,
  consumer cards, sql panels, boot/progress faces) is out of mobile
  scope by construction; consumer-grid's unreachable 460px query is
  deleted.
- Rejected: hiding the sandbox with CSS while keeping
  `client:visible` — what IntersectionObserver reports for a boxless
  target is fragile ground for a 16MB gate; `client:media` is the
  platform's own answer. Also rejected: replacement mobile copy in
  the sandbox slot (superseded by the planned hero) and
  `maximum-scale` viewport clamping (an accessibility harm).
- Verification is manual (user, at 390/640/761px); the Playwright
  no-PGlite-below-the-gate assertion waits until Playwright exists.
