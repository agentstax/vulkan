# Mobile review — doc site

2026-08-27. The sweep behind the "mobile friendly docsite" task. Working
doc for review; folds into TODO.md / a decision record and gets deleted
at close-out. All widths below are for a 390px phone viewport (iPhone
standard) with 360px as the floor; the site today has exactly two width
media queries (sandbox panel collapse at 760px, consumer-grid at 460px)
— everything else is desktop-only.

## The settled piece: the sandbox does not load on mobile

User call, this session: the sandbox is excluded from the mobile pass —
too large a download, and unusable at phone widths anyway.

The numbers and the mount:

- PGlite is ~16MB raw: pglite.wasm 9.6MB + pglite.data 6.0MB + ~1MB js,
  before compression. CodeMirror rides on top.
- The sandbox mounts exactly once — index.astro:43, inside the
  "Start Here" post — with `client:visible`.
- sandbox.svelte's `onMount` calls `databaseState.connect()`
  immediately, so **hydration is the download trigger**. Excluding the
  sandbox on mobile means gating hydration, not hiding pixels.

Proposed mechanism:

- Swap `client:visible` for `client:media="(min-width: 761px)"` —
  Astro's own viewport-gated hydration. Below the gate the island's JS
  never loads, so PGlite is never fetched. 761px matches the sandbox's
  existing one-column collapse (sandbox.css:45): below that width the
  panels already stack and the console is degraded anyway.
- Hide the server-rendered shell below the same width in sandbox.css.
  The shell (build-time highlighted SQL over seeded rows) is cheap to
  ship but shows dead controls (Run, Produce, Reset) when the island
  never hydrates.
- Rejected: keeping `client:visible` and hiding the island with CSS.
  `client:visible` watches the island element with IntersectionObserver;
  what a boxless (display: none) target reports is fragile ground to
  build the exclusion on. `client:media` is the platform's answer.
- website/CONVENTIONS.md ## Islands & loading currently sanctions only
  `client:visible` and `client:idle` — the amendment adding
  `client:media` for viewport-gated islands lands in the same change.
- Copy: the intro paragraph above the sandbox (index.astro:38-42) says
  "Edit the SQL and press Run" — it dangles when the console is gone.
  Proposal: a mobile-only replacement line in the sandbox slot, in the
  site's own voice (the era's "best viewed at 1024×768" energy), and
  the intro reworded so it reads true on both sides of the gate.

What the exclusion removes from the mobile pass: everything that only
renders inside the sandbox — produce-message (whose input compresses to
~42px at 390px, the sharpest squeeze the sweep found), consumer-card,
consumer-grid, add-consumer, sql-panel, sql-result, boot-notice,
database-progress. Several of the worst phone-width findings are in
that set and are fixed by the gate alone. consumer-grid's 460px media
query becomes unreachable and can be deleted.

## The frame math everything else falls out of

`.page-frame` is `min(1080px, calc(100% - 32px))` (BoardLayout.astro:93).
At 390px: 358px frame → 316px content after `.page-content` padding →
~286px inside a bordered, padded section row. The frame itself is
fluid; what breaks is the fixed-width content inside it.

## Findings, clustered

### 1. The board grid token

`--grid-board-columns: 34px 1fr 80px 210px` (global.css:199) + three
10px gaps = 354px of incompressible track against ~286px. Consumed by
board-row, sticky-row, announcement-row, thread-row, and the
board-section column-label band — so the index, whats-new, and every
board listing scroll sideways, and every title collapses to a ~100px
ribbon beside a 210px "last post" column. The 80px thread-count track
is an empty `<span>` on three of the four row types. One token is the
root cause; the fix is a collapsed row layout under the breakpoint
(title over meta, empty column dropped) in the five consuming
components.

### 2. The author column on posts

`.author { width: 150px; flex-shrink: 0 }` (thread-post.css:58,
error-post.css:30) leaves **~131px of body text at 390px** on every doc
and error page. Fix: under the breakpoint the author column becomes a
horizontal header row above the body. Also `.post-body` lacks
`min-width: 0`, so any wide child (table, long inline code) widens the
whole document instead of scrolling locally — that one-liner is needed
at all widths.

### 3. Missing flex-wrap / min-width: 0

Non-wrapping flex rows that overflow or shred at phone widths:

- visit-bar + version-select (nested nowrap rows, ~478px of min-content,
  on every page; the old-version notice makes it worse)
- board-nav (overflows at 360px; no wrap)
- board-footer (zero slack; holds the only board-style switcher)
- breadcrumb, and the thread-top / thread-return rows pairing it
  against adjacent-links / JumpTo (ThreadLayout.astro:83-87, 100-104;
  ErrorThreadLayout.astro same) — horizontal overflow on every thread
  page
- prev-next (two full thread titles side by side)
- thread-title-band (solved chip + h1 + Edit button; the button is the
  squeeze victim)
- post-header on both post components
- board-stats (~396px of min-content in 316px)
- board-search form (placeholder-driven input min-content)

Most of these are a `flex-wrap: wrap` or `min-width: 0` each.

### 4. Overflow safety in prose

- `.thread-body` tables have no scroll wrapper (global.css:503) — the
  4-column decision-record index table is a guaranteed 2.5× blowout.
  Per website conventions wide content should scroll in its own box.
- Inline `code` has no `overflow-wrap` (global.css:458) — one long
  identifier forces page-level scroll.
- fix-line's value/placeholder segments: same, no break opportunity.
- Caught-message renderers lack `overflow-wrap: anywhere`: site-notice
  `.notice-detail`, island-boundary `.fallback-detail` — the containers
  meant to contain failure are the ones an unbroken chunk URL breaks.
- search-result excerpts: same gap; Pagefind excerpts carry VK codes
  and identifiers.

### 5. Fixed overlays

- cookie-notice: fixed bottom bar measuring ~191px (~29% of viewport)
  at 390px, no body bottom padding to scroll clear of it, no
  `env(safe-area-inset-bottom)` — it permanently covers the footer and
  the JumpTo control that ends the board pages. Behavior stays as
  settled [0599][0600]; this is geometry only.
- site-notice: `.notice-box { max-width: 44ch }` resolves against 16px
  Verdana to ~392px on a padding-less `inset: 0` veil — wider than a
  390px screen, and the left overflow of a centred fixed element is
  unreachable. The veil has no `overflow-y: auto` (a tall detail clips
  the buttons), and the top banner covers the site header with no
  compensating offset.
- z-index tie: cookie-notice and site-notice both sit at
  `--z-raised: 2`, so source order alone lets the consent bar paint
  over the failure alert. The closed z scale needs a named step for
  page-level notices.
- accept-all-modal: veil is `overflow: hidden` with a centred box — in
  landscape the action buttons clip with no scroll path, and Escape is
  the only exit (keyboard-only). The meme layer mixes percentage
  `left` with fixed pixel widths, so at phone widths the images stack
  behind the box with thirds sliced off.

### 6. Touch targets and type

- `.era-button` is ~20px tall (global.css:579) and is the face of Go,
  Search, prev/next, Edit this page, Dismiss, Clear. chrome-button's
  close tone is a hard 16×16px. Every select (board-style switcher,
  jump-to, version-select) is ~15-20px tall. copy-button and the nav
  links are ~13px. Everything interactive is under half the 44px
  touch minimum, several targets 6-8px apart.
  Proposal: padding bumps under `@media (pointer: coarse)` so the
  desktop aesthetic is untouched.
- iOS focus zoom: any input under 16px font zooms the page on focus —
  board-search's 12px input and log-line-paste's 11.5px textarea both
  trigger it. Inputs go to 16px under the coarse-pointer query;
  suppressing zoom via maximum-scale is an accessibility harm and is
  not proposed.

### 7. Banner and nav

- The 42px letter-spaced wordmark overflows its column below ~390px
  and cannot wrap; the banner's 64px side padding and non-shrinking
  76px volcano leave it ~192px.
- The volcano's `margin-bottom: -34px` + raised z-index hangs it over
  the first nav link's tap area — a desktop bug too, fatal on touch.
- The nav row has no wrap and overflows at 360px.

### 8. Hover-only affordances

- compat-matrix cell explanations live only in `title` attributes —
  no touch path to "migrate the database up first".
- Prose links and copy-button underline on hover only — colour is the
  sole affordance on touch.

## Already sound

MDX `pre` blocks and highlighted-sql scroll locally; log-line wraps;
compat-matrix has a real scroller and wrapping legend; the page frame
width is fluid; the pixel SVGs are viewBox-driven; add-consumer and
cookie-answer already wrap. The frame, not the drawings, is the
problem.

## Proposed order of work

1. Sandbox gate: `client:media`, shell hide, replacement copy,
   conventions amendment. (The settled piece; also shrinks the rest.)
2. Breakpoint + layout collapse: the grid token's five consumers, the
   author column, the flex-wrap/min-width pass. Kills horizontal
   scroll site-wide.
3. Overflow safety net: table scroll wrapper, inline-code and
   caught-message overflow-wrap, excerpt wrap.
4. Fixed-overlay geometry: cookie-notice offset + safe area,
   site-notice width/scroll/header offset, the new z step,
   accept-all-modal veil scroll + meme sizing.
5. Touch targets + input font sizes under `pointer: coarse`.
6. Banner/nav + hover affordances.

## Settled (user, 2026-08-27) and built the same day — [0602]

- Breakpoints: 640px layout collapse + 761px sandbox gate, declared
  in website/CONVENTIONS.md ## CSS.
- The sandbox slot: the whole intro+sandbox post is removed below the
  gate, no replacement copy — a dedicated hero section (future work)
  will be the all-widths answer. Built as
  src/pages/_components/sandbox-post/, one island on
  `client:media="(min-width: 761px)"`.
- Cookie notice: hidden below 640px, geometry untouched above it.
- Verification: manual, at 390/640/761px; Playwright deferred.

Everything under "Findings, clustered" above shipped except: the
compat-matrix title-attribute duplication (the visible legend already
carries the explanations, left as is) and produce-message's nowrap
row (unreachable below the gate; only its 16px coarse-pointer input
fix landed, for tablets).
