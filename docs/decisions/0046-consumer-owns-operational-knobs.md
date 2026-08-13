---
status: accepted
date: 2026-06-15
phase: "2"
---

# 0046 — The consumer owns the operational knobs; the datastore takes only connection params

**Context.** The operational settings existed in two places: the consumer and
a duplicated `PostgresDatastoreConfig`. The duplication made
`WithBatchLimit`/`WithMaxAttempts` silent no-ops — the values a caller set
were not the values the queries used.

**Decision.** The consumer is the single owner of `BatchLimit`, `MaxAttempts`,
and `WorkTimeout` and passes them into `ProcessMessages`. The consumer
datastore constructor takes only connection params, matching the producer
datastore. The duplicated `PostgresDatastoreConfig` is removed.

**Consequences.** Each knob has exactly one home, so a set value is always the
used value. Datastore constructors stay uniform across producer and consumer.
