---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0613 — column naming rules

## Context

The [0611] table review's column audit, run over the live tbls
snapshot (`just schema-diagram`), found the same fact spelled
differently across tables: lease expiry as `until` / `expires_at` /
`lease_until`, an attempt's timestamp as `attempt_at` /
`attempted_at`, schedule instants as `_time`, and `pattern` meaning
the derived POSIX regex in `binding` but the declared NATS-style
form in `binding_log`. The user's opaque document is `payload` on
messages but `data` on cron jobs, though the cron job's data becomes
the produced job request.

## Decision

The rules:

- Instants end `_at`: past events as past participles (`created_at`,
  `declared_at`, `attempted_at`), expiry always `expires_at`, and a
  lower-bound gate ends `_after` (`can_run_after` stays — "any time
  after T" is information `_at` loses).
- A fact about the row itself is bare; a fact about an attached
  concept carries that concept's prefix (`claim_lease.token` vs
  `exception_queue.lease_token`); `last_` marks latest-of-many.
- Singular = ordinal (`attempt`), plural = running count
  (`attempts`, `reclaims`) — the logging registry's rule, extended
  to columns.
- Durations are BIGINT nanoseconds ending `_ns`.
- The user's opaque document is `payload` everywhere.

The renames: `binding_log.attempt_at`→`attempted_at`,
`lease.until`→`expires_at`, `delivery.lease_until`→
`lease_expires_at`, cron_job's `next/last_scheduled_time`→
`next/last_scheduled_at` (moving to `cron_job_cursor` per [0611]),
`binding.display`→`pattern` with `binding.pattern`→`pattern_regex`
(so `pattern` means the declared form everywhere), and
`cron_job.data`→`payload` — a public-surface change:
`cron.CronJob.Data` and `cron.JobRequest.Data` become `Payload`,
wire tag `data`→`payload`, plus the declare path's field.
`JobRequest.ScheduledTime`→`ScheduledAt` (wire `scheduled_at`)
rides the instants rule. `alert.Alert.Data` stays — the alert
message's own field, an open candidate, not cron payload plumbing.

## Consequences

- Unchanged and deliberate: `head_id`, the cursor's role-named
  message ids, `error`/`last_error`, `token`, `can_run_after`.
- The `_at`/`_after` and `_ns` rules are machine-checkable — a
  tools/conventions walk over the schema (or tbls `schema.json`)
  should enforce them so drift cannot recur.
- Ships with the [0611]/[0612] rename sweep; the cron `payload`
  rename sweeps docs, examples, and labs with it (grep labs for
  `->>'data'`).
