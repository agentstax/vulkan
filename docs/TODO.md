# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Doc site — mobile-friendly pass

Picked up 2026-08-27 from THOUGHTS; design settled and built the same
day [0602]. Sweep findings in mobile-review.md (repo root; deleted at
close-out).

- Settled (user): sandbox excluded on mobile; the whole intro+sandbox
  homepage post is removed below the gate with no replacement — a
  dedicated hero section (future work) is the all-widths answer for
  that slot; cookie notice hidden on phones; breakpoints 640px
  collapse + 761px gate; verification is manual, no Playwright yet.
- Built: sandbox-post route-local island on `client:media`;
  conventions amendments (island trigger row, breakpoint rows); the
  640px collapse across the grid-token consumers, author columns, and
  banner/frame; the flex-wrap and overflow-wrap passes; overlay
  geometry + `--z-notice`; `pointer: coarse` control and input sizing.
  Checks: prettier, eslint, stylelint, astro check, vitest, astro
  build all pass; svelte-check carries only the pre-existing
  board-stats story drift.
- Remaining to close out: user's manual pass at 390/640/761px (and
  confirming no PGlite request below the gate in the network log);
  then the HISTORY.md entry, delete mobile-review.md, and drop the
  THOUGHTS.md line.
