---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# binding_log retention: the consumer group janitor

## Context

binding_log is append-only ([0511] shape, renamed by [0570]): a blocked
declarer re-appends a waiting row every BindingRetryInterval, so a
long-blocked group grows the table without bound. Installed rows are the
set-change audit -- one per real change, kept forever, out of scope.

## Decision

Waiting rows older than a flat 7d TTL are deleted in one batched DELETE,
always keeping each declarer's newest waiting row -- a dead waiter stays
visible in listings, and growth is bounded by declarer count in practice.
classifyDeclaration reads only installed rows, so declareBindings is
unaffected; rows past the TTL cannot race an insert, so no lock
coordination.

The sweep runs as a new worker kind, consumer_group_janitor, under
pkg/consumergroup/janitor at OwnerSystem scope -- one worker row total,
hourly poll rate and sweep_batch_size 1000 in row metadata, a Debug line
with swept_count only on ticks that deleted rows. Naming pattern: each
domain's cleanup worker is its janitor -- the topic kind renamed
"janitor" -> "topic_janitor" (WorkerTopicJanitor), SQL owner comments
disambiguated as topicjanitor. / consumergroupjanitor..

waitingDeclarationTTL stays a package const, listed on the
hardcoded-config audit. No new index: the sweep keeps the table small
enough that its own scan is noise.

## Consequences

- Declared at RegisterSystem beside the metrics collector and cron
  scheduler; provisioned by the system manager and every consumer's
  embedded manager.
- [0571]'s per-topic split amends the sweep to one batched DELETE per
  topic's binding_log table per tick; the worker stays one row.
- Rejected: riding the cursor advancer (per-group rows multiply
  idle-fleet cost; the sweep isn't cursor work).
