---
status: rejected
date: 2026-08-26
phase: pre-v1
---

# 0591 — the sandbox's Postgres is not prefetched

## Context

The roadmap carried a PGlite wasm prefetch: a `requestIdleCallback` fetch of
the sandbox's wasm after interactive, so its first statement is near-instant.
It was built, measured, and reverted.

The assets are four, not the one chunk the item named: `pglite.wasm`
(10.09 MB raw / 3.39 MB gzipped), `pglite.data` (6.30 MB / 1.86 MB),
`initdb.wasm` (395 KB / 145 KB), and the PGlite JS chunk (602 KB / 138 KB) —
17.4 MB raw, 5.53 MB over the wire, plus a 10 MB wasm compile.

Two findings from reading the code first:

- `sandbox.svelte` already boots the database in `onMount`, and the one
  sandbox on the site sits at the fold of the homepage under `client:visible`.
  The observer fires on about the first frame, so `client:idle` would fire
  LATER — queued behind the page's three other idle islands.
- What actually delays the download is a serial module waterfall: the island
  chunk and the Svelte runtime, then `database.ts`, then the 602 KB PGlite
  chunk, each a round trip, and only then does `PGlite.create()` ask for the
  16 MB.

## Decision

Nothing prefetches. The sandbox keeps booting in `onMount` off
`client:visible`, and PGlite keeps fetching and compiling its own files.

The version that was built and reverted, for anyone tempted to rebuild it: a
page-level `<script>` on the homepage that `compileStreaming`s the three files
on idle, handed to `PGlite.create()` through its `pgliteWasmModule`,
`initdbWasmModule` and `fsBundle` options — which it does skip its own download
for. It worked. It was reverted on the ratio, not on a defect.

What it cost, against a saving of two round trips and a 602 KB parse on a
5.53 MB download:

- A module reaching into `node_modules` by relative path for its `?url`
  imports, because the package's `exports` field does not name the files and
  the package-specifier form fails to resolve.
- The first page-level `<script>` in the tree, load-bearing on being written
  above `<BoardLayout>` — written below it, Astro renders the tag at the end of
  the body instead of hoisting it into `<head>`.
- A second place that knows how PGlite starts, holding a compiled 10 MB
  `WebAssembly.Module` and a 6 MB Blob for a page the reader may never scroll
  into, with its own failure-and-retry rule so **Reset sandbox** still works.

## Consequences

- The gain was never measured in a browser — it was reasoned from the chunk
  graph and the built output. Reopening this needs a real measurement first,
  not another estimate.
- `<link rel="prefetch">` is not the cheaper way back in: the site sets no
  cache headers, and it would fetch everything twice under `astro dev`.
- The sibling `transition:persist` item is the better lever and is untouched.
  `ClientRouter` is in the stack list but is not actually wired up, so PGlite's
  module-level caches die on every navigation — keeping the live instance
  across pages would save a whole boot, where this saved part of one.
