---
status: accepted
date: 2026-08-26
phase: pre-v1
---

# 0592 — ClientRouter, without persisting the sandbox

## Context

The roadmap asked for `transition:persist` on the sandbox, to keep the live
PGlite instance across navigations — boot cost once per session instead of once
per page. `ClientRouter` was in website/CONVENTIONS.md's stack list but had
never been wired up, so every navigation was a full page load.

`transition:persist` cannot do that. Astro's swap matches the attribute on both
documents and drops anything the destination lacks:

    const newEl = newElement.querySelector(`[${PERSIST_ATTR}="${id}"]`);
    if (!newEl) continue;

The sandbox is on `/` and nowhere else, so navigating to a doc page destroys it
whatever directive it carries.

What does survive a soft navigation is the module registry: the swap is
`oldElement.replaceWith(newElement)`, so `window` and every evaluated module
outlive it. PGlite keeps its fetched `Response`s and its compiled
`WebAssembly.Module`s in module-level maps, which means `ClientRouter` alone
caches the expensive half.

## Decision

`<ClientRouter />` in `BoardLayout.astro` — the only layout that owns a `<head>`,
so one line covers all 80 pages. Nothing carries `transition:persist`.

- Returning to `/` now skips the 5.53 MB gzipped download and the 10 MB wasm
  compile. It still runs instantiate, initdb, the topic DDL and the seed.
- Skipping those too needs the `VulkanDatabase` itself alive, which means
  hoisting `DatabaseState` into a module singleton — and taking the consumer
  cards, `groups` and `nextConsumer` with it, or a returning reader gets
  "no runs yet" and "billing's cursor is at 0" over a database whose cursor is
  at 8. Deliberately not taken: it relocates the sandbox's state model and pins
  a live Postgres for the whole session.
- `DatabaseState.close()` is new, and the sandbox calls it from `onDestroy`. A
  full page load used to release the Postgres for free; a soft navigation does
  not, and each one holds a 128 MB wasm memory. `reset()` now goes through it
  rather than keeping its own copy of the teardown.
- The reduced-motion guard for `::view-transition-*` is ours, in global.css's
  base layer. Astro ships one in `viewtransitions.css`, but that file only
  reaches the build when a `transition:*` directive is used — with none, the
  cross-fade is the browser's own default and nothing gates it.

## Consequences

- Every page's initial JS grows by the router: 16.29 KB raw, 5.55 KB gzipped.
  That is the real price, and it is paid on all 80 pages for a sandbox saving
  on one — soft navigation site-wide is what earns it.
- The sandbox's `onDestroy` is now load-bearing rather than defensive. The
  teardown chain is real: `astro-island.disconnectedCallback` fires
  `astro:unmount` after the swap, `@astrojs/svelte`'s hydrator listens for it
  and calls `unmount(component)`, which runs `onDestroy`.
- Page-level `<script>` tags do not re-run across a swap unless marked
  `data-astro-rerun`. The tree has none today; one added later must account for
  it.
