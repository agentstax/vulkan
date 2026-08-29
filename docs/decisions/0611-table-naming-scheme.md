---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0611 — table names are `<root>_<kind>`

## Context

Table names grew one at a time and the seams show: `topic` holds
config while `message_log` holds data, `delivery` reads as the
mainline path though the mainline never writes it (success and
superseded write only `delivery_log` rows), and `lease` vs
`key_lease` leaves the difference to a schema read. Reviewed on a
live tbls/Liam ERD snapshot (`just schema-diagram`).

## Decision

Every table name is `<root>_<kind>`: the leading words name the
resource a row is about, the trailing word the table's kind —
`_config` (declared state, written by declaration verbs),
`_config_log` (that config table's declaration trail), `_log`
(append-only event history; an event root stands alone with no
sibling table — `message_log`, `delivery_log`, `migration_log`),
`_queue` (mutable work rows), `_lease` (expiring locks, prefixed by
what is leased), `_instance` (live copies), `_cursor`/`_head`
(singleton runtime state). A table 1:1 with another resource's rows
carries that owner's name (`consumer_group_cursor`). FK columns keep
the resource's noun (`topic_id`), never the table's name.

Renames: `system`→`system_config`, `topic`→`topic_config`,
`topic_log`→`topic_config_log`, `consumer_group`→
`consumer_group_config`, `worker`→`worker_config`, `worker_log`→
`worker_config_log`, `cron_job`→`cron_job_config` (its
`next_scheduled_time`/`last_scheduled_time` and the due index split
out to a new 1:1 `cron_job_cursor` — the scheduler's position in the
schedule, sibling of `consumer_group_cursor`), `delivery`→
`exception_queue` (rows are deliveries off the mainline path —
ready-to-retry, deferred, dead; the exception consumer's own noun),
`cursor`→`consumer_group_cursor`, `lease`→`claim_lease`,
`key_lease`→`message_key_lease` (rides on [0612]), `binding`→
`binding_config`, `binding_log`→`binding_config_log`. Unchanged:
`worker_instance`, `migration_log`, `message_log`, `delivery_log`,
`idempotency_key`, `compaction_head`.

## Consequences

- `message_key_lease` ships only with [0612]'s column/API rename;
  until both land the table stays `key_lease`.
- CONVENTIONS ## Tables gains the naming rule when the rename ships,
  not before. Sweep: internal/topic table-name funcs, every SQL
  literal, labs' inline interpolations, scripts/database/tbls.yml,
  doc-site prose. Pre-v1: baseline DDL edited in place.
- `system_config`/`consumer_group_config` are identity-only today;
  uniformity chosen over precision.
- The `cron_job_cursor` split changes scheduler SQL: the due scan
  joins back to config for `schedule`/`suspended`, and a schedule
  edit becomes a two-table write (recompute next). Per-fire churn
  moves off the near-static config rows.
