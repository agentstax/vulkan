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
  NEXT: first vertical slice in website/ — remove Starlight,
  BoardLayout.astro, token stylesheet, one real component folder
  (thread-row + story) — small on purpose so the user can react
  before building wide.
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
