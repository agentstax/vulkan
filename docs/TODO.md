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
Regenerate with `npm run build && npm run mockup` in website/ — the
generator is website/scripts/build-sandbox-mockup.mjs, writing the
gitignored .mockup/homepage-sandbox.html. It reads the console island's
scoped-class hashes off the built page, so it survives component edits;
delete it once the sandbox ships and the page IS the mockup.

- It lives on the board index, in the slot the console holds today (inside
  the Start Here hero post). No separate sandbox thread — one home.
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
- Consumers run STEP-BY-STEP: one tick = one claim, one handler call, one
  commit. No timer.
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
7. Rename what the build outgrew, once the shape has settled: the
   `sql-console` component is the sandbox, `produce-strip` produces a
   message rather than a strip of anything, and `_board/console.ts`
   feeds both panels. Names, folders, stories and the `consoleLabel` /
   `ConsoleShell` vocabulary move together.

**Phase 3 -- details.**

- Panel SQL editing (CM6 in each panel, reusing the console's editor.ts
  chunk) and the edited-query-never-clobbered rule.
- Reset sandbox re-seeds.
- Stories per component, `just site-verify`, a browser run (produce ->
  tick -> cursor advances), and a look at the homepage's initial JS now
  that the island is bigger.
- Whatever phase 1 and 2 surface.
- Not in scope, tracked in ROADMAP: auto-run on a timer, the delivery_1
  panel, the fail-the-next-message toggle.
