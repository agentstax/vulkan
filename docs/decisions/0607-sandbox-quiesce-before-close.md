---
status: accepted
date: 2026-08-28
phase: pre-v1
---

# 0607 — the sandbox quiesces before closing its database

## Context

The browser-support spot check [0606] surfaced a shipped bug: leaving
the homepage after the sandbox booted could freeze the destination
page until a hard reload. PGlite is a synchronous wasm build; a
statement reaching a Postgres that has already exited leaves
execProtocolRawSync spinning inside pgl_setPGliteExitStatus on the
main thread — reproduced in pure Node on 0.5.6 and 0.5.8, where
close() with a query in flight never returns. The island's onDestroy
stopped the timers and closed the database, but a straggler operation
could still reach it: Svelte's teardown can flush a panel's scheduled
run after onDestroy, and the old close nulled `connecting` first, so
a late connect() would even boot a second 128 MB database nobody
would ever close.

## Decision

- Every statement-running verb on DatabaseState goes through one
  private `perform`, which registers the operation in
  `pendingOperations` synchronously — before its first await — so
  close() sees it the moment it exists. The first fix attempt
  registered after `await connect()` and the freeze survived it:
  an operation parked on that await was invisible to the drain.
- close() sets `closing` first, refusing new operations (a straggler
  run gets a rejected promise its caller's own error path already
  handles), drains pendingOperations with allSettled in a loop —
  a settling operation can have started another — and only then
  shuts the database down. `closing` clears at the end so reset()
  reconnects.
- The Playwright flow test "leaving a booted sandbox keeps the next
  page alive" pins the regression in all three engines.

## Consequences

- Navigating away from the sandbox now costs at most the tail of one
  consumer tick before the wasm heap is released.
- PGlite's close-with-statement-in-flight deadlock is still there
  for any embedder; worth an upstream report with the Node repro.
