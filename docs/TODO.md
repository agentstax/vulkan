# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Consumer-flow sandbox on the board index (picked up 2026-08-24)

The homepage SQL console grows into the whole produce/claim path: produce a
message, tick a consumer, watch the cursor move. Extends the console's
mechanism [0584] rather than standing beside it.

### Design APPROVED 2026-08-24

Mockup (the approved layout, built by overlaying the sandbox on the real
dist page): https://claude.ai/code/artifact/3d58ee63-d602-435b-bab8-f0b5be65a878
The mockup generator that produced it is gone: the built page now renders
the sandbox itself, so `npm run build` and a browser are the check.

- It lives on the board index, in the slot the console held (inside the
  Start Here hero post). No separate sandbox thread — one home.
- The page layout does not change. The sandbox renders INSIDE the existing
  `.sql-console` frame and reuses the console's own parts: title bar,
  editor-area + highlighted `pre.sql`, result table, status bar.
- Order inside the frame: title bar (label, meta, Reset sandbox) ->
  produce strip (text field + Produce) -> two panels side by side
  (message_log_1, cursor_1) -> consumer cards, three across, with the
  add-consumer control beneath.
- Grid tracks are `minmax(0, 1fr)` and panels carry `min-width: 0` with
  `overflow-x: auto` on the SQL and result areas -- a `pre` and a nowrap
  table refuse to shrink, and plain `1fr` pushed the console out of the
  section box.

### Settled with the user 2026-08-24

- It is a HARNESS, not a simulation: the SQL is Vulkan's own, extracted
  byte-exact and drift-tested like the console's; only the loop that calls
  it is the page's. The hero paragraph says so. Statements the flow needs,
  all already `-- vulkan:` tagged: messageconsumer's
  freshClaimMessagesWithCursor / claimMessages / readMessages / commit /
  partialCommit / deliveryStatement, consumergroup.registerGroup.
- A consumer is a GROUP MEMBERSHIP, not an anonymous box. Adding one asks
  new group (its cursor starts at 0, so it replays everything already
  produced) or join an existing group (the two split messages, claiming
  disjoint ranges off one cursor).
- No "reset cursor to 0" button: rewinding a group is not a Vulkan verb
  (Proposed on the replay thread). Reading it all again = declare a new
  group. Reset sandbox re-seeds the database and is labeled a page
  control, not an API.
- One tick = one claim, one handler call, one commit. Consumers ran
  step-by-step off a per-card Run button until 2026-08-24, when the user
  replaced that button outright with an auto-run toggle [0587]; the tick
  itself is unchanged, only what calls it.
- Each panel owns a default query and re-runs it after any produce or tick
  ONLY while the visitor has not edited it; once edited it marks itself
  stale and waits.
- PGlite is one connection, so consumers take turns and true contention
  cannot be staged; the page says so.
- Deferred (user): the delivery_1 panel and a per-consumer "fail the next
  message" toggle -- recorded in ROADMAP.

### Build phases (user-ordered 2026-08-24)

Visuals first, then integration. Placeholder data is FINE inside phase 1 --
it is corrected in phase 2, before anything stands as finished.

**Phase 0 -- claim-path spike, headless. DONE 2026-08-24 [0585].** The gate
proves on the first poll, so Tick runs the real claim path unchanged. A
produce lands in the NEXT claim (no wait-a-tick caveat), two claims on one
cursor come back disjoint, caught up reads low == high. The fence can be
explained on the page but never shown firing -- staging it needs a second
backend holding a write open.

**Phase 1 -- visuals. DONE 2026-08-24.**

- Producer strip inside the console frame.
- cursor_1 panel BESIDE the existing message_log_1 panel -- the console
  already renders message_log_1, so this extends it rather than rebuilding
  it. Its honest default is the empty state: no groups, no cursor rows.
- Consumer cards, three across.
- Add / remove consumer widgets (new group or join an existing one).

Landed with it, beyond the drawing: each panel owns a PanelState and runs
its own SQL against one shared DatabaseState (ConsoleState deleted, the
console holds the singleton inline); a per-panel Run button; ChromeButton
gained a `tone` (primary | quiet). Still visual: Tick, Remove, Add,
Produce and Reset all take no-op handlers, and the `auto re-runs` chip
claims something nothing does yet.

**Phase 2 -- integration, in dependency order.** A cursor row does not
exist until a group does, so the group work precedes the cursor panel
going live:

1. Producer form produces for real (the produce CTE already runs in
   PGlite).
2. message_log_1 auto-updates after a produce.
3. Add consumer registers the group -- consumergroup.registerGroup and
   the cursor row it creates.
4. cursor_1 auto-updates after any produce or tick.
5. Tick = one claim, one handler call, one commit. **DONE 2026-08-24
   [0586].** Nine statements extracted, drift now counted per
   `-- vulkan: <owner>` tag so unrun verbs need no case. Commit frees the
   lease and writes nothing else -- under the default `failures` log mode
   a success is never collected, so deliveryStatement / logStatement /
   partialCommit wait for the fail-the-next-message toggle.
6. Regenerate the build-time shell from the new seed. **DONE 2026-08-24 --
   nothing to do.** `_board/console.ts` runs `createVulkanDatabase` in Node
   at every build, so the shell cannot go stale against the seed; the built
   page already carries the eight seeded rows and billing's cursor at 0.
   The seed's own fix (one compaction key per order) landed with [0586].
7. Rename what the build outgrew. **DONE 2026-08-24.** Folders, files,
   idents, css classes, story titles and prose moved together:
   `sql-console` -> `sandbox`, `produce-strip` -> `produce-message`
   (matching `add-consumer`'s verb-phrase shape), `console-progress` ->
   `database-progress` (it reports the `DatabaseStage` boot, not a
   console), `_board/console.ts` -> `_board/sandbox.ts` with
   `sandboxLabel` / `sandboxTopic` / `sandboxShell` / `SandboxShell`,
   and `editor.ts`'s `consoleTheme` -> `editorTheme`.
   Deliberately KEPT: the `--color-console-*`, `--shadow-console` and
   `--console-title-gradient` tokens, and `sql-panel` / `sql-result` /
   `highlighted-sql`. The container is a sandbox; the panels inside it
   are still SQL consoles, and the tokens name that chrome. Don't
   re-flag them.

**Phase 3 -- details.**

- Panel SQL editing + the edited-query-never-clobbered rule. **DONE
  2026-08-24.** Both panels mount CM6 off the one editor.ts chunk, so
  `editable` is gone from SqlPanel's props. PanelState gained `edited`
  (the doc differs from the query the panel shipped with) and `stale` (a
  write landed that this panel did not run). `runAt` marks stale instead
  of running once edited; typing back to the default resumes auto re-runs.
  The chip reads `auto re-runs` / `edited` / `edited · behind`, the last
  in amber via two new tokens (`--border-panel-behind`,
  `--color-panel-behind-text`).
- Reset sandbox re-seeds. **DONE 2026-08-24.** `DatabaseState.reset()`
  closes the handle, drops the single-flight promise, rebuilds and bumps
  the revision; the sandbox puts the cards back to the one seeded card
  and re-reads the groups. It deliberately does NOT touch the editors --
  rebuilding the database does not invalidate a query the visitor wrote,
  and an edited panel simply goes `behind` until they Run. Proved
  headlessly: 9 rows / 2 groups / billing at 1 -> 8 rows / billing only /
  cursor 0, and the fresh database ticks.
- Homepage initial JS. **DONE 2026-08-24.** 60.5 KB raw / 22.4 KB
  gzipped, of which 40.1 KB is the shared Svelte 5 runtime -- the sandbox
  island itself is 13.5 KB / 4.9 KB gz. PGlite (588 KB js + 9.8 MB wasm +
  6.1 MB data) and CodeMirror (309 KB) are dynamic-only and appear
  nowhere in index.html, which two editors did not change.
- Auto-run replaces the manual Run button. **DONE 2026-08-24 [0587].**
  Every consumer starts with auto-run on and ticks about once a second
  (`1000ms ± 150ms`, the next run scheduled after the previous resolves).
  `AutoRunner` is plain TS -- no reactive state, so vitest can load it,
  unlike a `.svelte.ts` module -- and carries fake-timer tests including
  stop-mid-run. `ChromeButton` gained `pressed: boolean | null`; the
  on-state is muted amber off `[aria-pressed='true']`, no second `data-`
  attribute. The global in-flight lock is gone: a card's `disabled` now
  means the database is unavailable, nothing else.
- One payload shape everywhere. **DONE 2026-08-25.** Seed and produce both
  write `{"order_id": <id>, "desc": <text>}`; order numbers run 4001.. from
  a `seedOrders` array of eight short descriptions and continue from there
  on every produce (`VulkanDatabase.nextOrderId`, which a Reset rebuilds
  along with the database). Four digits so the order number never reads as
  a second spelling of the message id. The card prints the whole payload --
  the `text`-field unwrap is gone.
  GOTCHA: jsonb has no key order. Postgres sorts object keys by LENGTH then
  bytes, so `desc` (4) comes back before `order_id` (8) no matter how the
  literal was written. The card names the two fields when it renders, which
  is why it leads with `order_id` while the message_log_1 panel -- showing
  the raw column -- leads with `desc`.
- New result rows flash on entry. **DONE 2026-08-25.** `@starting-style`
  plus a transition on `.result td`: a new cell fades up out of
  `--color-row-enter` (amber, the board's new-or-act colour). Two things
  make it fire only for genuinely new rows:
  - SqlResult keys its `{#each}` by ROW CONTENT (`JSON.stringify(cells)`
    plus an occurrence suffix for duplicates), not by index. Index keys
    reuse every `<tr>` when a row is prepended, so nothing is ever rendered
    fresh and nothing reaches `@starting-style`.
  - The declarations sit on the cell, not the row: a `td` background paints
    over the `tr`'s, so the `nth-child` stripe keeps its own and neither
    rule has to outrank the other.
  Consumer-card lines got the same entry, on `.out-line`. A card's lines are
  only ever PREPENDED, so they key from the end
  (`consumer.lines.length - index`) -- exact here, and the shape guess the
  result table could not make.
  Both sit inside `@media (prefers-reduced-motion: no-preference)`, matching
  database-progress; with no transition declared the starting values are
  never used, so reduced motion means no flash rather than a jump. Rows in
  the server-rendered shell animate once on page load -- `@starting-style`
  fires for elements in the initial HTML too.
- STILL OPEN: a browser run (produce -> the clocks claim it -> cursor
  advances, then Reset). Everything below the UI is proved headlessly, but
  nothing has driven the actual page.
- STILL OPEN: the `edited` and `edited · behind` chip states have no
  story -- they are only reachable by typing into a CM6 editor that
  mounts on idle. Adding a prop to force them would be a field with no
  production reader, so the gap is deliberate; a Playwright flow is the
  right home if the stack ever gains one.
- Whatever phase 1 and 2 surface.
- Not in scope, tracked in ROADMAP: the delivery_1 panel, the
  fail-the-next-message toggle, declaring a binding so routing_key
  selects.
