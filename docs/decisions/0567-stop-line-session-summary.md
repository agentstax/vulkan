---
status: accepted
date: 2026-08-21
phase: pre-v1
---

# 0567 -- Stop line as session summary

## Context

[0562] fixed the stopped line as bound identity and nothing else, so a
session ends without numbers -- "it was flaky" has no evidence. Nothing
in-process holds instrument state: the whole metrics pipeline is DB
truth polled by the collector onto __system.metrics, and a single
instance's session contribution is unrecoverable from any query. OBS
renders its outputs' internal counters at stop; the OTel SDK is the
accumulate-and-export template (in-process cumulative instruments, a
periodic reader, a final in-memory collect at shutdown).

## Decision

- The stopped line becomes the session summary: bound identity, session
  `duration`, and the instance's lifetime counters -- every counter
  printed even at zero, `<verb>_count` registry keys. Emitted on EVERY
  exit including fatal-error teardown (the error is still returned,
  never logged; the line reports the transition and the numbers). The
  line reads memory only, never the database -- metrics never hold up a
  shutdown, and error teardown often means the database is unreachable.
- The counters are metrics machinery, not a log-side struct: atomic
  counter fields plus a Snapshot() read-model on the instance-side
  metrics producer (pkg/metrics/producer), the object already threaded
  into the message/exception consumer provisioners and already running
  in the instance's errgroup -- zero new threading; it outgrows its
  abandoned-events name at build. Bumps happen at the worker layer from
  facts in hand (the outcomes slice after Commit returns nil, the
  reclaim fact off ClaimedRangeData), never inside datastores.
- Transport splits by shape: counter bumps are in-memory only; a flush
  tick in the producer's Run loop produces current totals as
  KindCounter Measurements -- the Kind declared for running totals,
  first used here; otelvulkan already maps KindCounter to a monotonic
  observable. Event-shaped metrics keep their per-event produce.
- Instance-emitted measurements carry instance identity as a series
  attribute: a session uuid minted per Consume call for the session
  counters (one session spans several worker rows); worker-scoped
  metrics may carry their worker_instance id. Dead sessions' series
  freeze and age out with topic retention.
- Counter set, exact names settled against the row statuses at build:
  claimed, success, superseded, ready, deferred, dead, reclaimed,
  quarantined, abandoned, lease_lost. The CONVENTIONS start-line
  amendment lands with the build.
- The producer's produced counter alone waits on presence (13d): no
  producer lifecycle by prior decision, so its stopped line is the
  presence heartbeat goroutine's stop.

## Consequences

Every teardown ends with evidence, including database-down ones, and
Prometheus gains per-instance counters with no otelvulkan change.
Rejected: querying the topic at exit (a round trip for data the process
already holds, and a DB dependency at teardown); per-bump produce
(doubles the write load of every consumed message); counting in the
log pipeline (Debug narration is level-filtered); a counters home in
consumergroup vocabulary (pure read-models only).
