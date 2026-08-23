# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Documentation-driven pass (picked up 2026-08-22)

Settled: docs drive implementation (page = the proposal, reviewed before
code); all pages rewrite to the REAL API; vocabulary per CONVENTIONS.md
## Vocabulary (one registry for code, comments, and docs); no performance
number without a benchmark record.

### DONE (2026-08-22)

- [x] Site triage: all 19 non-error pages read, verdicts issued.
  guides/migrations was already real (sidebar link added); errors/*
  untouched.
- [x] ALL page rewrites. Every non-error page except cloud now
  documents the real API; every Go sample compile-checked in the
  scratch module (produce/, consume/, txguide/, routing/, pagescheck/);
  site build green (74 pages); vocabulary sweep run over all rewritten
  pages.
  - quickstart — real path shown honestly: migrate init CLI, full
    producer/consumer programs, psql inspection with real per-topic
    table names, ProduceFunc as the transactional headline, manager-run
    upkeep aside. Postgres claim = "test suite runs against 17" (the
    old ">=14" was unsourced).
  - guides/transactional-enqueue — renamed guides/transactional-produce
    (banned verb in title + slug; 5 inbound links + sidebar updated).
  - concepts/streams — renamed concepts/fan-out ("stream" banned),
    title "Fan-out, Retention & Replay": fan-out + retention real
    (AllowDropPastCommitted default-blocks drops — the retention cliff
    is defused by default), new-group-reads-history real; rewind of an
    existing group -> proposed.
  - concepts/lifecycle — real cursor-path model: success writes no
    per-message row; delivery rows materialize on failure only
    (ready/inflight/deferred/dead); range leases + reclaim +
    quarantine; kill backstop; redrive -> proposed aside.
  - concepts/ordering — retitled "Ordering & Concurrency": id-order
    claims, no completion-order guarantee, per-key exclusivity =
    compaction key + ConcurrencyDefer (latest-wins, NOT FIFO); strict
    per-key FIFO -> proposed. OrderBestEffort/partition keys deleted.
  - concepts/queue-and-log — fusion thesis kept, restated on the real
    mechanism (cursor for success, delivery rows for failure).
  - concepts/routing — real binding model: []string at Register (whole
    set, nil = whole topic), `*` = any-run-of-characters wildcard
    (depth-crossing, no NATS `>`), installed/joined/waiting outcomes,
    forward-only changes; header matching -> proposed; old
    retroactive-routing pitch removed (depends on unshipped rewind).
  - concepts/architecture — real control-plane + per-topic families,
    cursor/delivery flow, poll-only claims (LISTEN/NOTIFY absent ->
    proposed), worker-fleet table, no performance numbers.
  - site roadmap page — Built section lists the shipped engine
    (log/queue split, routing, retention, compaction, fleet, VK codes,
    compat gate); Now = benchmarks + docs; Later = demand-driven list
    from docs/ROADMAP.md.
  - index + why-vulkan — real hero samples (ProduceFunc + Consume),
    honest steps (new-group-history instead of FromOffset), cards
    rescored, SQL on real tables, cloud labeled "planned", numbers
    stripped.
  - guides/dead-letters — real triage SQL (delivery/message_log/
    delivery_log joins), metrics-based watch step, redrive -> proposed
    with spec sketch.
  - guides/replay — split: new-group bootstrap = shipped; rewind of an
    existing group = proposal spec with open questions recorded.
  - demo — marked Proposed (command does not ship); reworded onto the
    real schema (committed-cursor scoreboard) and the existing
    failure-injection labs it would package.
  - compare/* — rescored to shipped behavior, proposed items labeled
    never checkmarked; all unbacked numbers stripped (tens-of-thousands
    msg/s, ~50k graduation) pointing at the benchmark pipeline instead.
- [x] Status markers: sidebar badges (demo "Proposed", replay "Partly
  proposed"); in-page Asides mark proposed sections on mixed pages.
- [x] Code fix: RoutingKey doc comment (options.go) now states the real
  reach — a keyless message matches no binding, only unbound groups
  receive it (matches the SQL + routinglab).
- [x] Code fix: worker_log added to deleteSystemTables' reverse-order
  drop list and to destroysystemlab's assertion list;
  destroy-system-lab green (drop + re-register both asserted).

### OPEN — deferred

- [ ] cloud page — USER 2026-08-22: resolve in the details as the site
  work proceeds, not as a standalone decision now (index/why-vulkan say
  "planned" when linking to it).
- Standing gate (not a task, user-confirmed 2026-08-22): site deploy
  (`just site-deploy`) — always ask first.
- SETTLED 2026-08-22 (user): the per-topic cursor-vs-lifecycle-choice
  pitch stays gone — ConsumerType is demoted to the point of deletion
  and is no longer part of the pitch. Never re-introduce it in docs or
  marketing.

### OPEN — docs mechanism brainstorm (2026-08-22, in progress)

Invented interactive machinery for the site; user verdicts so far:

- [x] PGlite feasibility spike — PASSED 2026-08-22 (scratchpad
  pglite-spike/): PGlite = Postgres 18.3 wasm; baseline DDL ran
  UNMODIFIED (partitioned message_log, XID8, gen_random_uuid, partial
  index, FKs); real producer.protectedInsert CTE verbatim incl.
  idempotency dedupe (duplicate -> 0 rows); ~730ms full schema,
  4ms/produce — live homepage seeding viable. Still open: tier 1 =
  replay real SQL literals drift-checked via `-- vulkan:` owner tags;
  tier 2 = Go wasm + pgconn DialFunc bridge to execProtocolRaw, one
  flagship page only.
- [ ] paste-your-log-line error/event pages — COMMITTED by user, not
  built (parse attrs, interpolate the reader's values into fix
  commands).
- [ ] compat verdict widget — COMMITTED by user, not built (pick build
  + target version, compute the real MinCompatibleVersion gate answer).
- [ ] inline "why?" toggles expanding decision records per-claim —
  LIKED (round two), not built.
- [ ] docs site themed as an old message board — homepage design at
  round 5, user-accepted starting point pending final look (canvas:
  https://claude.ai/code/artifact/01680050-91b7-4d50-824c-0bd3d4a27af5,
  page "Round 5 — current"; earlier rounds on the reference page).
  Settled design system: subSilver-blue chrome (band gradient
  #2166A0->#184E7C, border ramp 8FA3B5/A9BCCB/D8E1E9, stripes
  F3F6F9/E9EFF4, lacquer = 1px white inner highlight); the split =
  chrome speaks 2004 (Verdana meta/buttons, Trebuchet bands+wordmark),
  content speaks now (IBM Plex Sans; IBM Plex Mono for everything the
  database says); amber #F2A33C ONLY means new/act (glowing pixel
  folders, console top edge — caret is blue-gray); pixel icon dialect
  (volcano banner mark, folders, pin badge, announcement flag) with a
  hard rule learned by iteration: RECTANGLE-NATIVE silhouettes only —
  diagonal shapes (megaphones, bells) staircase at this grid and were
  rejected twice; every
  section on the 34/1fr/80/210 grid; visited-purple as feature;
  "Show what's new since then" (localStorage) replaced mark-all-read;
  dark mode later as "Board style" dropdown (palette donor = round-1
  C). Motion notes on canvas. "threads" (not "topic") = doc pages;
  Vulkan-powered real forum deferred as premature. NEXT: main pixel
  icons pass (user: later), then build the page in website/
  (docs-drive-implementation: canvas = the proposal).
- [x] Site stack — SETTLED 2026-08-23 (user): keep Astro, DROP Starlight
  (board skin replaces everything Starlight provides; content
  collections + Pagefind survive, both Astro-core/framework-agnostic).
  Islands = Svelte 5 (one island framework site-wide; springs/
  transitions fit the canvas motion notes; Preact considered, React
  rejected as weight). SQL console = EDITABLE (user: "what I would
  really want"): CodeMirror 6 + @codemirror/lang-sql in a Svelte
  island, lazy-loaded with the PGlite wasm chunk; schema-aware
  autocomplete fed the real Vulkan tables; CSS-themed to the board
  tokens (Plex Mono, blue-gray #8FA3B5 caret); real Postgres errors
  rendered in board style + "reset database" (re-run seed DDL);
  visitor SQL RESETS each load for now (persistence = maybe later).
  Rest of stack: ClientRouter view transitions (transition:persist on
  the console), nanostores for cross-island facts (last-visit),
  Pagefind styled as board search + one search router island (VK code
  / log line / full-text), Vitest for pure parsers, Playwright (not
  Cypress — wasm handling) for E2E, Storybook for Svelte components.
  Component rule: anything worth a Storybook story is a Svelte
  component; .astro files are page scaffolding/layout only (Storybook
  cannot render .astro). Build-time pipelines: Go->JSON export of the
  migration registry for the compat widget (never reimplement the
  gate in TS), `-- vulkan:` drift check failing the build when
  embedded SQL diverges from Go source. PGlite lazy-loads on console
  visibility only; no COOP/COEP needed. Motion behind
  prefers-reduced-motion.
  Frontend conventions — researched 2026-08-23 (report artifact:
  https://claude.ai/code/artifact/19817c04-9178-4427-8af9-961715e638c4),
  USER REVIEW: "mostly looks fine", final verdicts deferred until real
  code is in front of them (concerns not yet articulable — expect
  iteration during the build). Picks: Svelte 5 = official docs rules
  ($derived over $effect, callback props, snippets, .svelte.ts state
  classes, no stores) + eslint-plugin-svelte, exemplar bits-ui
  structure; Astro = official docs (strictest tsconfig, directive
  rulebook, content.config.ts + .describe() zod), exemplar
  withastro/astro.build (no LICENSE — patterns only); CSS = CUBE CSS
  vanilla (@layer reset/tokens/base/compositions/utilities, blocks =
  scoped styles, data-attr exceptions; Sass/Tailwind/Open Props/BEM
  rejected), exemplar madrilene/eleventy-excellent; Storybook = v9 +
  svelte-vite + addon-svelte-csf .stories.svelte only, exemplar
  temporalio/ui (Histoire rejected, stale); prose = Diátaxis
  page-purity + Google style + Vale in CI enforcing the ## Vocabulary
  table (GitLab substitution pattern, IgnoredScopes=code) +
  remark-lint, frontmatter enforced by zod only.
  Hardening additions (user round, folded into the report): strong
  typing everywhere (no-explicit-any as error, typed row shape per
  console query — TS sibling of db:-tagged *Data structs; field-
  explosion exception = extend platform types only); CSS anti-sprawl
  set — no raw values outside the tokens layer (stylelint +
  declaration-strict-value), semantic-tokens-only in components,
  ~80-line scoped-style cap, :global() as a sanctioned-crossing list,
  utilities need 3 call sites, closed z-index/spacing scales;
  folder-per-component with ecosystem suffixes (Component.svelte +
  Component.stories.svelte + state .svelte.ts + types.ts), styles
  stay INSIDE .svelte (external css loses scoping), no index.ts
  barrels.
  DISTILLED 2026-08-23 into website/CONVENTIONS.md (user-approved
  placement: separate file, root sections bind by reference, loaded
  via new website/CLAUDE.md; AGENTS.md + root ## Documentation
  amended to sanction it). Two defaults picked during distillation,
  flag for user: kebab-case filenames (matches bits-ui/temporal +
  the Go lowercase-file habit; PascalCase was the alternative), and
  nanostores DROPPED — one Svelte-only site needs no store library,
  shared state = .svelte.ts runes modules (one mechanism per fact).
  FIRST SLICE BUILT 2026-08-23, USER-ACCEPTED ("this looks good",
  incl. the sibling-css conversion): the Board
  Index section serves at /board BESIDE Starlight (removing Starlight
  would break the 74 live pages; it goes when the board build
  replaces the index). Files: src/styles/global.css (layer decl +
  only the tokens this chunk uses, two-tier), layouts/BoardLayout.astro
  (page frame + ground only — no banner/nav/ritual bars yet),
  components/pixel-folder + board-row + board-section (each with
  .stories.svelte), pages/board.astro (real data: thread counts +
  last-post from the docs collection, dates =
  helpers/last-commit-date.ts git fallback mtime), svelte.config.js,
  .storybook/. All rows render read-state folders — honest until the
  read-tracking island exists. Real-beats-mock: Troubleshooting = 53
  threads (mock said 25). Astro build 75 pages green; zero JS shipped
  for the slice (Svelte server-rendered; lone script = Starlight
  prefetch); headless-Chrome screenshot verified.
  Version pins forced by Astro 6: @astrojs/svelte@^8 (v9 wants Astro
  7), storybook@^10 + addon-svelte-csf@^5 +
  @sveltejs/vite-plugin-svelte@^6 (matching Astro's) + vite@^6.
  Storybook gotcha SOLVED: an Astro project has no vite.config.ts so
  nothing gives Storybook's builder vite-plugin-svelte — .storybook/
  main.ts viteFinal prepends svelte(); without it every plugin
  parses raw .svelte as JS ("Expression expected" from docgen/addon).
  CHROME SLICE BUILT 2026-08-23 (awaiting user review): banner + nav
  strip + last-visit bar + bottom bar wired into BoardLayout.astro.
  New components (each folder: .svelte + sibling .css + .stories):
  pixel-volcano (rect ramp tokens --volcano-rock-1..6 +
  --volcano-lava-*), board-banner (gradient #235684->#1B4569 — its
  own tokens, NOT the section band gradient; mark overhangs via
  margin-bottom -34px + --z-raised), board-nav (current prop ->
  aria-current styling; GitHub/Docs/Troubleshooting real hrefs,
  Search inert until Pagefind), visit-bar (lastVisitDate: string |
  null — layout passes null, the honest first-visit state until the
  read-tracking module; "Show what's new" is a button, appears only
  in the returning-visitor story), board-footer ("The team" ->
  GitHub contributors, "Delete board cookies" inert button, style
  chip inert). New tokens: chrome-bar #163C5E (+link/meta),
  chrome-pale #EAF1F7 (+text #45596B), lacquer-dim 0.12, wordmark
  shadow, volcano shadow, --z-raised (closed z-index scale started).
  Build 75 pages + Storybook build + eslint green; screenshot
  verified; still zero client JS.
  STICKIES SLICE BUILT 2026-08-23 (awaiting user review): Start Here
  section above the Board section. pixel-folder gained required
  `pinned` prop (viewBox 17x15, folder rects translate(0 1), red pin
  rects on top; glow still only from `updated`); new sticky-row
  component (cream row, STICKY label, board grid, real title + git
  date); board-section gained `threadCount: number | null` (band
  right-side "N threads", singular handled); board.astro stickies =
  quickstart + why-vulkan resolved from the docs collection (throws
  on a miss), folders read-gray + no glow honestly until
  read-tracking. New tokens: sticky cream/border/label-red + pin red
  ramp. Design's "pinned by brandon" flavor line NOT used — both
  rows show the real updated date. Build + storybook + eslint green,
  screenshot verified.
  PAGE-LOGIC SPLIT DONE 2026-08-23 (user pulled it forward, then
  asked for finer file organization): board.astro frontmatter is
  fetch-call-render only; route-local src/pages/_board/ holds one
  concern per file — model.ts (BoardRowData/StickyRowData row
  shapes, the model.go sibling), boards.ts (Board type + boards
  array + stickyIds curation), rows.ts (boardRows/stickyRows +
  entryFilePath helper). Rule recorded in website/CONVENTIONS.md
  ## Components. New page derivations (stats box etc.) land in
  _board/, never back in the frontmatter.
  HOMEPAGE-COMPLETE SLICE BUILT 2026-08-23 (awaiting user review):
  USER RESEQUENCED — the working PGlite/CM6 console moves to the END
  of main-page work; this slice templates it statically and lands
  every remaining homepage section. New components: thread-post
  (author column: name/3 stars/role/volcano avatar + body snippet),
  sql-console STATIC shell (presentational Props label/sql/columns/
  rows; keyword highlight via sqlSegments token renderer — no
  {@html}, svelte/no-at-html-tags stays on; Run inert; status shows
  row count ONLY — no ms claim, no "nothing leaves your browser",
  no "it actually runs" copy until PGlite is real), pixel-flag,
  announcement-row, board-stats (real computed stats: threads =
  docs.length, error and event codes = errors/ count, decision
  records = fs count of ../docs/decisions; legend + purple note),
  jump-to (inert era furniture); breadcrumb inline in board.astro.
  _board gains announcements.ts (3 real milestones w/ HISTORY.md
  dates + real hrefs), console.ts (template SQL/rows — replaced by
  the live console later), stats.ts, SiteStats in model.ts. New
  tokens: console set (title/button gradients, sql pale, header
  blue, null gray, amber edge, shadow), author column, avatar,
  rank star, flag = amber ramp. PGlite + CodeMirror deps INSTALLED
  but unused yet (settled stack; the console slice consumes them).
  Build + eslint + storybook green; full-page screenshot verified.
  LIVE CONSOLE SLICE BUILT + USER-ACCEPTED (2026-08-23, after review
  iterations: requestIdle named function, highlighted-sql/ component
  extracted from the orchestrator, swap comment):
  - sql/ dir (renamed from vulkan-sql, USER) (USER: one file per SQL statement): 33 files
    GENERATED byte-exact from the Go sources (scratch extractor,
    filters on `-- vulkan:` tag; a backtick in a Go comment is the
    trap) — create-system-tables/ (17), create-topic-tables/ (14
    templates + mirroring functions over interpolate.ts %s/%d +
    table-names.ts internal/topic mirror), protected-insert
    keyed/keyless; statements.ts per dir = the Go method's Exec
    order; sql/sql.test.ts (vitest, `npm run test`) asserts
    exact substring in the Go file PLUS literal-count equality
    both ways.
  - database.ts: createVulkanDatabase(onStage) — stages
    'downloading' | 'starting postgres' | 'creating tables'
    (coarse hooks settled: ~750ms init flat, the variable cost is
    the ~5.2MB gz wasm+data fetch; byte-progress deferred); full
    schema (all 9 catalog + 10 family tables), catalog seed =
    plain INSERTs, 3 messages through the REAL produce CTE (2
    keyless incl. ''->NULL routing key, 1 keyed -> compaction_head
    row); VulkanDatabase.run -> RunResult{columns,rows,
    affectedRows|null,durationMs|null,statementCount} rendering
    the LAST result set of db.exec.
  - _board/console.ts = build-time shell run in Node: shell rows
    are real query output; a broken literal FAILS the build. Shell
    holds ms until the viewer's own Run (user may flip).
  - island: sql-console client:visible; idle -> editor.ts (CM6
    dynamic chunk, EditorView.theme mirrors shell .sql, keyword-
    only highlight) swaps over the static pre, Run flips enabled;
    idle warm = database JS chunk only; first Run -> ConsoleState
    (sql-console-state.svelte.ts) creates PGlite behind
    console-progress era bar; error phase = PG's verbatim message
    in mono red panel. New components sql-result/ (table+status)
    and console-progress/ split out to keep css blocks near ~80.
  - conventions settled: `<name>-state.svelte.ts` wording landed
    in website/CONVENTIONS.md; eslint gains no-undef off for
    TS-parsed files + .svelte.ts parser coverage; astro.config
    vite.optimizeDeps.exclude pglite; tokens: lacquer-bright,
    surface-veil, progress veil/track/fill, console-error.
  - verified: drift test 3/3, build 75 pages (shell shows 3 real
    rows), eslint, storybook build, CDP browser test = click Run
    -> "3 rows · 0.7 ms" + post-run screenshot.
  - deferred to later slices: try-it links (state class makes a
    link = set sql + run()), wasm asset prefetch on idle,
    transition:persist once navigations exist.
  READ-TRACKING SLICE BUILT + USER-ACCEPTED (2026-08-23; design user-settled:
  sticky scope = its own thread; "Show what's new" button stays
  inert this slice; first visit = all amber):
  - storage model USER-REDESIGNED same day (replaces the first
    per-scope-map build): ONE append-only page-visit log in
    localStorage vulkan-board:visits — entries {href, visitedAt},
    the page the visitor is on. Everything derives from the log:
    visit bar = stamp at the END (captured before this load
    appends); scope visited = last matching entry scanning from
    the end (append-only = chronological, NO sort — the map
    model needed one because spread keeps a re-visited key's old
    position); amber = change date > that stamp, or no match.
    Cap 200 entries = TRUE front pop (slice(-max)) on record and
    load; ~12KB bound; evicted scopes read as unread again.
  - amber dims ONLY by visiting the scope (user requirement):
    rows carry build-time scopeHrefs (board.href + every member
    thread href, from the same boards.ts contains()); clicks
    record the DESTINATION page (onVisit(href) on board-row);
    sticky scope = [its own href]; tracked-visit-bar records
    location.pathname on mount — so inner pages start recording
    automatically once they ride BoardLayout (index-swap slice);
    until then deep links don't record (known gap).
  - container islands (wiring only — no stories; presentational
    children keep theirs): board-rows/, sticky-rows/,
    tracked-visit-bar/; board-row/sticky-row gained required
    onVisit callback props; board.astro loops ->
    <BoardRows client:idle>/<StickyRows client:idle>; SSR renders
    folders gray, hydration lights amber.
  - storage guarded behind typeof window (Node 25 ships its own
    warning localStorage global); timestamps via
    helpers/now-iso.ts nowIso() — a plain-TS home so
    svelte/prefer-svelte-reactivity doesn't flag new Date() in
    the .svelte.ts (user removed the inline disables).
  - CDP-verified: behavior test (all amber first visit -> visit
    Concepts -> only it dims) + cap invariant test (200 seeded +
    N appends evict exactly the N oldest; click lands at the
    end; page-load append recorded). One clean load appends
    exactly once.
  - verified: lint/build/storybook green; CDP behavior test =
    fresh profile all amber + "Welcome" -> click Concepts ->
    return: Concepts gray, others amber, stickies amber, bar
    shows the date; mixed-state screenshot taken.
  THREAD-PAGE DESIGN ROUND 6 USER-ACCEPTED 2026-08-23 as starting
  point (canvas page "Round 6 — thread pages"): a doc page = ONE post
  by the site, era author column (150px, brandon/Site Admin/Posts: 74
  real count) beside the rendered MDX; prose capped ~680px; H2 = Plex
  Sans 19px hairline underline; code blocks on console pale, Go
  keywords = the SQL keyword blue #184E7C, strings one quiet red
  #B03A2E; proposed asides = quote-box with outlined PROPOSED chip
  (chrome-colored, never amber/checkmark); title band = section
  gradient + "Edit this page"; era prev/next links top, named buttons
  bottom (real sidebar neighbors). Error threads: OP = the error
  itself (author VK000x, pixel-exclamation avatar, rank "Permanent
  error", verbatim log line with 3px pin-red left edge), declared fix
  = ACCEPTED ANSWER post by brandon (new green family #1E7B3C/#EAF4EC/
  #CBE3D2), [SOLVED] pale-green chip in the band, registry facts =
  one mono strip, paste-your-log-line search strip below. Every era
  button real: Edit -> GitHub edit URL, Report this thread ->
  prefilled GitHub issue, Copy link/error text -> clipboard.
  USER VERDICTS: no in-page TOC (revisit if a long page needs one);
  author column scrolls away (never sticky); VK-as-poster approved.
  NEXT SLICES (user-approved sequence 2026-08-23, homepage-style
  incremental — Starlight keeps serving the 74 pages until slice 4):
  1. prose dialect + ONE real thread — BUILT 2026-08-23 (awaiting
     review): /board/concepts/ordering/ renders the real MDX through
     layouts/ThreadLayout.astro (nests BoardLayout; owns the top row
     breadcrumb + era "View previous/next thread" links and the
     return row). New components (each with stories):
     thread-title-band (h1 + Edit this page -> GitHub edit URL),
     prev-next (ThreadLink | null both sides — ordering is LAST in
     Concepts so next=null, real order beats the artboard's guess),
     proposed-aside (PROPOSED chip quote-box; ordering.mdx now
     imports it instead of Starlight's Aside — scoped styles render
     fine on the live Starlight page too). thread-post EXTENDED
     (header: PostHeader | null = posted strip + Report this thread
     -> prefilled GitHub issue + frame border via data-framed;
     postCount: number | null = real docs.length); breadcrumb items
     now href: string | null (null = current page, plain text).
     MDX dialect = .thread-body block in global.css base layer
     (the sanctioned base-layer MDX styling): p/h2/h3/lists capped
     680px, inline code + pre on console pale, striped bordered
     tables, tab-size 4; sl-anchor-link hidden (Starlight-injected,
     dies with Starlight). CODE FENCES: Starlight expressiveCode
     DISABLED, markdown.shikiConfig = inline "vulkan-board" textmate
     theme (keyword families blue-bold #184E7C — keyword.operator
     stays ink, strings #B03A2E, comments faint italic) — the
     mechanism that survives the swap; Starlight pages pick it up
     light-on-pale (fine in light, mismatched in Starlight dark mode
     — temporary). .era-button utility added (3 call sites: jump-to
     Go converted, Edit this page, prev/next). thread.ts route-local
     facts (_thread/): board lookup, lastCommitDate, docs.length,
     edit/report GitHub URLs. Verified: build 76pp, eslint, vitest
     3/3, storybook build, screenshots (preview page vs artboard +
     Starlight collateral check).
  2. generalize to all non-error pages — BUILT 2026-08-23 (awaiting
     review): boards.ts RESHAPED — Board.contains(id) replaced by
     Board.threads(ids) returning member ids IN READING ORDER (the
     one source for membership, counts, scopeHrefs, and now
     prev/next; curated boards list ids explicitly — a missing id
     fails the build via boardEntries; Troubleshooting derives +
     sorts by code). threadData gains previous/next: ThreadLink |
     null from board position. pages/board/[...id].astro
     (getStaticPaths over board members, errors excluded until the
     variant slice) replaced the hand-placed ordering.astro — 17
     thread previews at /board/<id>/. index + roadmap belong to NO
     board: no thread page; roadmap's home = swap-slice decision.
     STARLIGHT COMPONENT SWEEP (all 15 board pages + ordering):
     proposed-aside RENAMED thread-aside with required label prop
     (chips NOTE/TIP/CAUTION/PROPOSED, one chrome treatment) — all
     20 Asides converted, "Proposed:" titles fold into the PROPOSED
     chip; Steps wrappers unwrapped to plain ordered lists;
     quickstart Tabs -> bold-labeled SQL blocks (a tabs island is
     undesigned era chrome — revisit on demand); cloud CardGrid ->
     h3 sections (content untouched — cloud copy is the open user
     decision; NOTE for that pass: cloud prose still says DLQ/
     stream/offset/partition key against ## Vocabulary). Only
     index.mdx + roadmap.mdx still import Starlight components
     (non-board; die at swap). Gotcha: an aside inside a list item
     must keep the closing tag at the item's indent or MDX fails
     the build. Verified: build 92 pages, eslint, vitest 3/3,
     storybook build, quickstart screenshot (steps + both asides +
     tab conversion + Go dialect).
  3. error thread variant — BUILT 2026-08-23 (awaiting review): all
     53 code pages render at /board/errors/VKxxxx/ via
     ErrorThreadLayout + board/errors/[code].astro. THREE kinds, not
     two (the pages revealed 10 metric pages beside 26 errors + 17
     events): frontmatter migrated BY-SCRIPT-RESHAPE of each page's
     own hand-written body (no generation from Go) — kind: error |
     event | metric, recovery (errors), level (events), consequence
     (verbatim, all), fix (errors; VK0003/18/19 legitimately
     fixless); zod extend on docsSchema with .describe() prose;
     _thread/code.ts codeThreadData validates presence per kind and
     derives the strings. Honesty rule fell out naturally: [SOLVED]
     chip + green ACCEPTED ANSWER post appear ONLY when a fix is
     declared. OP = the code as poster (mono red name, rank =
     Permanent/Transient error | Log event at <level> | Metric,
     pixel-exclamation avatar — one red mark for all kinds, the rank
     line carries the classification); log-line block composes from
     parts (error: "title -- fix [code]", event: level=WARN
     msg="title" code=VKxxxx, metric: the series name; red left edge
     on errors only); page body prose renders in the OP under the
     line (events/metrics have real bodies). New components (each
     with stories): pixel-exclamation, code-facts (registry strip),
     error-post, log-line, search-strip (inert Search until
     Pagefind, example = the composed line). Extended: thread-title-
     band (+code | null, +solved), thread-post PostHeader now a
     posted/accepted discriminated union (green strip); solved-green
     token family added. Copy-error-text buttons deferred with the
     other clipboard wiring. Vocabulary fix in passing: two "sit in
     the DLQ" lines (VK0041/VK0047) -> "sit dead-lettered". Verified:
     build 145 pages, eslint, vitest, storybook build, screenshots
     VK0005 (solved) + VK0028 (warn event).
  4. the swap: board at /, threads at final URLs, Starlight removed,
     Pagefind standalone, old-URL redirects, deep-link visit
     recording lands free (every page rides BoardLayout). Full
     verification checkpoint here.
  5. Astro 6->7 after (already sequenced below).
  "Show what's new" wiring rides a later slice.
  - [ ] Astro 6 -> 7 upgrade (USER 2026-08-23): wanted for
    @astrojs/svelte@9, sequenced AFTER Starlight removal — Starlight
    0.40 pins Astro 6; ripping it out first makes the upgrade a
    clean two-dep bump (astro + @astrojs/svelte).
  - [x] Component css moved to sibling files (USER 2026-08-23,
    supersedes styles-stay-inline): <style src="./<name>.css"> via
    svelte-preprocess (typescript: false — vitePreprocess keeps
    scripts), inlined pre-compile so scoping is identical (verified:
    hash-scoped selectors in dist, pixel-identical screenshot);
    ESLint floor added (eslint + typescript-eslint +
    eslint-plugin-svelte, `npm run lint`) with no-restricted-imports
    banning any .css import (.storybook/preview.ts exempt; .astro
    files unlinted until eslint-plugin-astro lands with the full
    verify stack); website/CONVENTIONS.md components rule amended.
  Lazy-loading approach (SETTLED 2026-08-23, user's initial-load
  concern): homepage ships static HTML/CSS only — every island
  declares its trigger (client:visible or client:idle), client:load
  BANNED; PGlite wasm + CM6 are dynamic imports = separate hashed
  chunks, never in the initial payload. Console shell is
  server-rendered with the example SQL as build-time Shiki static
  text + the spike's real result rows, so pre-hydration the console
  looks finished; CM6 swaps in place sized identically (zero layout
  shift), cue = Run button disabled->enabled. First-Run wait masked
  by a phpBB-style progress bar ("Connecting to database…") over the
  results area — the loader is era-themed furniture. Warm the cache
  via requestIdleCallback fetch after interactive (or pointer-toward-
  console); instantiateStreaming + immutable caching = visit two
  near-instant; transition:persist keeps the live instance across
  navigations (cost once per session). Pagefind lazy-loads its own
  index fragments; compat widget + log-line parser = plain
  client:visible islands. Guardrail: one Playwright test asserts the
  homepage's initial JS stays under a stated ceiling.
- No action unless revived: retry-curve slider playground (HESITANT —
  user finds it not unique); quickstart as verifiable psql transcript
  (EXPLORE, likely depends on the PGlite spike). REJECTED round two:
  your-deployment context panel, log-line-to-investigation-kit, schema
  atlas.

### Recorded findings — no action here, feed ROADMAP items

- Startup friction (feeds DefaultProducer/DefaultConsumer, ROADMAP
  Next): consumer needs a MessageAdmin + RegisterSystem just to
  GetTopic; topic.SchemaVersion(1) literal repeated 3x per program;
  Consume's cancellable-ctx requirement is a context.Background() trap;
  ConsumerConfig.Retry vs Message.Retry confusable; produce-only
  deployments silently get no upkeep without `vulkan manager run`;
  RegisterTopic wants &topiccontroller.TopicConfig{} (an import + empty
  struct for the common case; nil-ability unverified); pkg/common and
  pkg/topic names invite aliasing in user code.
- API ergonomics (feeds the 14b public-API pass): ProduceInTx accepts
  only a ProducerFunc — a static payload in a caller-owned transaction
  costs an inline 3-arg closure per topic; no value-taking form. No
  "start from now" option for a new consumer group — deep-retention
  topics force full history reads on every new group.
