---
status: accepted
date: 2026-08-23
phase: pre-v1
---

# 0584 — the site's SQL console runs the library's own SQL in PGlite

## Context

The homepage claims Vulkan is just Postgres tables you can query. A
console that proves it has one failure mode worth designing against: a
demo whose SQL is hand-written drifts from the library and quietly turns
the strongest claim into the biggest lie.

PGlite (Postgres 18.3 compiled to wasm) was spiked first: the baseline
DDL ran unmodified — partitioned message_log, XID8, `gen_random_uuid`,
partial indexes, FKs — and the real produce CTE ran verbatim including
idempotency dedupe, at ~750ms for the full schema and ~4ms per produce.

## Decision

The console runs a real Postgres in the reader's browser, seeded with the
library's own statements. Every statement lives in its own file under
`components/sql-console/sql/`, extracted byte-exact from the Go sources —
the `-- vulkan:` owner comment ([0560]) is what makes them findable — and
a vitest drift test asserts each literal appears verbatim in its Go file
AND that the counts match both ways, so a moved or edited query fails the
test rather than the reader's trust. Per-topic table names come from a TS
mirror of internal/topic's name funcs.

The console is honest about what it is doing, in both directions:

- Its shell is server-rendered at build time by running the same
  statements in Node, so the rows shown before hydration are real query
  output and a broken literal fails the build.
- Its status line reports only what it measures — row counts, and the
  duration of a run the reader triggered. Errors render Postgres's own
  message verbatim.

Loading follows [0582]'s rule: the island hydrates on visibility, the
CodeMirror editor swaps over the static shell on idle, and the ~5.2MB
wasm+data fetch happens on the reader's first Run behind an era-styled
progress bar.

## Consequences

- The schema demonstrated is the real one: 9 catalog tables, 10 per-topic
  tables, catalog seed rows, and three messages produced through the real
  CTE (including a keyed one, which lands a compaction_head row).
- The drift test is the standing guard — it is the reason a production
  query can move without silently breaking the site.
- Deferred: try-it links from doc pages into the console, idle prefetch
  of the wasm chunk, `transition:persist` to keep one instance across
  navigations, and the tier-2 idea of running Go wasm against PGlite
  through a pgconn DialFunc bridge.
