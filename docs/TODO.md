# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Doc site — layered error handling [0597]

Four rungs, separate tasks, in this order. Rungs 1–2 edit the
sandbox/database files the THOUGHTS.md sandbox refactor wants to
restructure — re-check sequencing against that refactor when picking
each up.

- [ ] Rung 1 — cover the unhandled failure points (~60–100 lines, no new
      machinery):
  - Read DatabaseState's boot-failure status (set at
    database-state.svelte.ts:122, read nowhere) and render a dedicated
    "could not start" panel in sandbox.svelte — today only
    `=== 'connecting'` is ever tested, so the overlay vanishes and the
    buttons re-enable.
  - .catch the CodeMirror dynamic import in sql-panel.svelte:60-69
    (void'd async IIFE; a lost chunk is an unhandled rejection and a
    silently read-only box).
  - Cover SearchState.search() after a successful Pagefind load
    (board-search-state.svelte.ts:46-58) — a throw leaves phase stuck on
    'searching' forever.
  - try/catch AutoRunner.fire() (auto-run.ts:47-49) so a future throw
    cannot kill the timer chain silently.
  - Replace the `String(caught)` fallbacks (sql-panel-state.svelte.ts:84,
    sandbox.svelte:128,159,202) — `[object Object]` is reachable.
- [ ] Rung 2 — component-fallback tier (~80–120 lines): shared
      failed-snippet component, `<svelte:boundary>` around island
      innards, and a story for every failure state (sandbox boot
      failure, database-progress failure, editor-missing sql-panel,
      search unavailable).
- [ ] Rung 3 — global nets + site notice (~150–250 lines): one bundled
      layout module registering 'error' / 'unhandledrejection' /
      'vite:preloadError'; the site-notice component with banner and
      modal (reload-required only) states plus stories; reload-once
      sessionStorage guard. The full-page face was built then cut in the
      round [0598] — no trigger exists on a prerendered shell.
- [ ] Rung 4 — conventions + message sweep (~40 lines of rules plus
      string rewrites): the website/CONVENTIONS.md ## Errors section
      (tier ladder, allowed surfaces, the reader-SQL vs site-machinery
      split, failure states are stories); rewrite the internal thrown
      strings in sandbox/database.ts ("-- was Register called?",
      "consumer group not found: …") to the problem+fix grammar; leave
      reader-typed SQL errors verbatim.
