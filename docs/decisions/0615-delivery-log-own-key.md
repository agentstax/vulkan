---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0615 — delivery_log is keyed by its own id, not by attempt

## Context

`delivery_log`'s primary key was `(consumer_group_id, message_id,
attempt)`, presuming one event per attempt. The table's own DDL comment
says one row per delivery *event* and lists `deferred` and `expired` —
events that happen on the way to a run. A retry claim spends an attempt
number before the run; a claim that never runs the handler (key busy at
the gate, superseded) hands the number back and logs its event under
it, so the next claim takes the same number and its outcome collides:
`duplicate key value violates unique constraint "delivery_log_N_pkey"`,
the row stuck `inflight`, the consumer instance stopped. Reproduced
with the real verbs on 2026-08-29.

The counters are consistent and stay: `attempts` is the number of runs
(the cursor path writes 0, a retry claim takes the next number, a claim
that never runs hands it back, a delay ran so it keeps its number —
[0614]). Rejected on the way here: writing no log row for gate
deferrals (DeliveryLogMode is the one switch for whether outcomes are
logged; one silently unlogged kind breaks it), and never handing a
number back with a separate `failures` column for the budget (a third
counter to carry what `attempts` already means).

## Decision

- `delivery_log_<id>` gets `id BIGSERIAL PRIMARY KEY`, the sibling of
  `message_log`'s key — the `_log` kind is append-only event history
  [0611], and an event history is keyed by event, not by the thing it
  is about.
- `attempt` stays a column: the run the event belongs to. A run can
  carry more than one event — `deferred` (claimed, handed back at the
  gate) then `failure` (claimed again, ran).
- `CREATE INDEX <table>_attempt ON (consumer_group_id, message_id,
  attempt)` keeps the per-message history walk and the triage joins
  on the key they used.
- No verb changes: `RecordDeferred` / `RecordSuperseded` keep handing
  the number back and logging under it.

## Consequences

- The composite key's accidental guard against recording one run
  twice is gone; the queue row's lease token is the real guard.
- Baseline DDL edit + sandbox mirror (pre-v1); deliveryloglab gains
  the re-deferral scenario as its assertion.
