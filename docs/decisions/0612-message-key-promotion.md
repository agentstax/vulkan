---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0612 — the message key is promoted out of compaction

## Context

The key does double duty: compaction's winner selection
(`compaction_head`, `compaction_rank`) and per-key delivery
serialization (`key_lease`, taken when `Concurrency = Defer`). But
the public API makes it reachable only through the compaction opt-in
— `ProduceOptions.Compaction *CompactionOptions{Key, Rank}` — so a
user cannot ask for strict per-key serialization while keeping every
message: defer drags compaction's supersede-and-drop semantics along.
Kafka's shape is the precedent: the key is a message property; what
uses it (compaction, ordering) is policy on top. Surfaced while
naming `key_lease` in [0611]: a lease named for compaction is a
consume-side lock that has nothing to do with compaction.

## Decision

- The key becomes a message-level concept named message key.
- `ProduceOptions` gains a top-level `MessageKey string`;
  `CompactionOptions` keeps `Rank` and loses `Key`
  (`NewCompactionOptions` loses its key param). `Compaction` set
  without `MessageKey` errors at produce time.
- `common.MessageRow.CompactionKey` → `MessageKey`, wire tag
  `compaction_key` → `message_key`. Column `compaction_key` →
  `message_key` in `message_log` and `message_key_lease`;
  `compaction_head`/`compaction_rank` keep their names — rank is
  rank within compaction.
- `Concurrency = Defer` requires only a message key, not compaction:
  serialized-by-key delivery with full history becomes expressible.

## Consequences

- The produce hot path changes shape for every keyed caller; the
  read model and CLI `--output json` documents change key names.
  Pre-v1, no wire-compat machinery — docs, examples, and labs sweep
  in the same change (grep labs for `->>'compaction_key'`).
- Defer-without-compaction is a new behavior path: the key-lease
  claim logic must not assume a compaction head exists. Its labs
  (deferlab siblings) extend to the uncompacted case.
- Doc-site compaction pages rewrite around "the message key", with
  compaction as one of its two uses; the produce page is the
  proposal surface before implementation (docs drive public-surface
  work).
- [0611]'s `message_key_lease` rename lands with this change, never
  before it.
