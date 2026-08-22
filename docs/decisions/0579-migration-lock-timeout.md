---
status: accepted
date: 2026-08-22
phase: pre-v1
---

# 0579 — migration txn steps run under lock_timeout; timeout retries

## Context

An ALTER takes ACCESS EXCLUSIVE; queued behind one long-running query it
blocks every later query on that table (locks grant in queue order), so
a quick migration can become an outage on a live fleet. The ROADMAP
carried this as a docs-only note ("should use LOCK TIMEOUT for any ALTER
migration commands"), but a doc rule is a per-author memory burden the
machinery can carry instead. The house pattern already exists twice:
ddlLockTimeout = 2s + SET LOCAL lock_timeout in producer create-ahead
and topic-janitor dropPartition, with 55P03 reclassified Transient there
(VK0018) because IsTransientPgError does not list lock_not_available.

## Decision

- runStepWithTx executes SET LOCAL lock_timeout = 2s right after Begin,
  before Validate — one site covers Up and Down, Validate reads, and the
  recordSuccess insert (weak locks, harmless).
- migrate's datastore declares its own ddlLockTimeout = 2s const
  (duplication over abstraction, matching the two existing sites). No
  config field — nothing tunes it yet.
- A 55P03 on the txn step path is reclassified via a new declared
  Transient error (next VK serial at build time; docs page in the same
  change), so the step retries under DatastoreRetry's existing bounded
  schedule. Safe: the step is one txn, so a timeout rolls back Validate
  + Apply + recordSuccess atomically — the same guarantee as the
  deadlock retries steps already ride, and steps are already required
  idempotent.
- NoTxn steps get no cap and keep fail-fast: SET LOCAL needs a txn, a
  session-level SET would leak the setting to the pool at Release, and a
  lock_timeout expiry mid CREATE INDEX CONCURRENTLY leaves an INVALID
  index no automatic re-run should touch.
- 2s stands: lock_timeout caps only queue wait, never DDL execution, and
  the cap is exactly the stall inflicted on queries queued behind the
  ALTER. Blocker durations are bimodal — normal traffic drains
  sub-second (2s wins); a stuck session holds for minutes (no timeout
  wins; the run should stop so the operator finds the blocker).
  Patience lives in the retry schedule, not the constant.

## Consequences

- Built pre-v1 though inert today (no ALTER steps exist pre-v1 —
  baseline DDL edits in place); release-era step authors inherit it.
- The Migration doc comment's authoring rules gain one line: txn steps
  run under a 2s lock_timeout and a wait past it retries; NoTxn steps
  get no cap.
- The website migration docs page (shared with the compat-matrix item)
  states the behavior; the new error's page lands with its declaration.
