---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0608 — Playwright covers the editor swap and the initial-JS ceiling

## Context

The flows suite [0607] left two gaps. The CodeMirror mount had no
test — the 2026-08-28 sandbox refactor checked it by hand. And
website/CONVENTIONS.md ## Islands & loading promised a Playwright
initial-JS ceiling that nothing enforced; the build-time import-graph
walk that once enforced a ceiling was rejected on code-to-gain [0594],
which named two obstacles for any revival: "initial" must be a moment
(the sandbox starts booting PGlite in onMount), and a ~240-line walk
is too much machinery for one number.

## Decision

- Editor swap: a flow test waits for `.cm-editor` inside the first
  `.sql-panel`, asserts the static `pre` shell left `.editor-area`,
  then types into the editor and asserts the panel chip leaves
  "auto re-runs" — proof the swap wired setSql, not just that a
  div appeared.
- Initial JS: a ~20-line flow test sums script response bytes on the
  homepage at a 640px viewport — below the 761px sandbox gate the
  island never hydrates, so networkidle is a stable moment and the
  sum is the JS every phone reader pays. Raw (decompressed) bytes,
  measured 82,080; ceiling 96,000, so an island-sized chunk fails
  the build while small growth passes. A second assertion rejects
  any PGlite chunk request below the gate.
- The CONVENTIONS sentence now describes this check and keeps the
  import-graph walk rejected [0594].

## Consequences

- Accepted blind spot: below the gate the sandbox island's own chunk
  never loads, so a CodeMirror static import inside that island is
  invisible; covering it means counting on desktop before an unstable
  moment — the flakiness [0594] warned about.
- The ceiling is per-homepage; growth every page pays (a ClientRouter
  [0592], a footer island [0593]) still lands here, since the
  homepage loads those same shared chunks.
