---
status: accepted
date: 2026-08-18
phase: 14b
---

# Every worker kind carries a controller layer

## Context

Janitor and waterline were pkg/worker/<kind>/datastore with no controller:
the executions called datastore publics directly with unvalidated params
(DropExpiredPartitions/SweepExpiredPartitions take 6 each), while the alert
kinds carry controller/datastore. The alternative -- sanctioning
datastore-only kinds because the execution is the only caller and its
inputs come pre-resolved -- was considered and rejected (user-settled):
uniform kind shape wins over the pass-through cost.

## Decision

- A worker kind is kind root (definition/execution/provision/metadata) ->
  controller -> controller/datastore, matching the alert kinds and the
  domain template. The controller owns validation; the datastore trusts
  inputs.
- Janitor gained JanitorController (drop.go, sweep.go, idempotencykey.go,
  keylease.go mirror the datastore verbs); waterline gained
  WaterlineController (AdvanceWaterline). Executions call the controller.
- Kind roots import their own controller as <kind>controller
  (janitorcontroller, waterlinecontroller) since bare `controller` is the
  worker machinery import there; the definition/execution field is named
  controller (the producer.Producer precedent).
- cronscheduler adopts the same shape in the produce-transaction-seam
  chunk, which already restructures it.
- Labs keep driving the kind datastores directly (they assert on
  internals); only their import paths moved.

## Consequences

- AdvanceWaterline keeps its (int64, error) return: seven labs assert on
  the committed position; the production roll discards it because the lazy
  roller doesn't care where committed landed.
- The two-statement non-transactional advance is reconfirmed correct and
  now states why at the private: the SELECT is one consistent snapshot and
  the UPDATE's GREATEST makes a stale (too-low) target a no-op.
