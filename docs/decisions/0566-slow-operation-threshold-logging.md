# 0566 -- Slow-operation threshold logging

status: accepted
date: 2026-08-21
phase: pre-v1

## Context

[0558] fixed level rules and steady-state silence but left the Postgres
log_min_duration axis open: slowness inside a healthy operation is
invisible -- Debug narration carries no durations and the [0559] ring
only surfaces on Error. Postgres (log_min_duration_statement), MySQL
(long_query_time), MongoDB (slowms), and Redis SLOWLOG all report only
operations past an operator-set duration: slowness, not occurrence, is
the event, and the threshold is the volume control.

## Decision

- One Warn line when an operation runs past its duration threshold, at
  the boundaries [0559] fixed. Warn, not the roadmap note's Debug: the
  default logger is WARN-and-up, so a Debug line would leave the
  threshold knob dead by default. The suppression window ([0564])
  storm-guards repeats.
- Boundaries: every produce entry point (ProduceFunc and ProduceInTx
  durations include the caller's own closure and transaction time,
  stated on the docs page), the per-delivery dispatch (CallSafely), and
  the worker tick.
- Timing is call-site code -- time.Since at operation end -- never a
  pipeline stage: stages see records; only the call site sees the
  operation end. The line logs on error paths too, because slowness is
  a separate fact from the returned error.
- Config: ProducerConfig.SlowProduceThreshold, and SlowDispatchThreshold
  on ConsumerConfig threaded through the three row configs into
  BaseConsumerConfig; 0 = unset = disabled on both. The tick gets no
  field: its threshold IS the row's poll_rate -- a slower tick is behind
  schedule by definition, and no caller could populate an override today.
- Three declared events VK0038-40 with hand-written pages. Produce's is
  unexported in pkg/producer/logs.go (VK0033 precedent -- the API
  package holds no vocabulary, the event stays package-local). New attr
  registry rows `duration` and `threshold`; the tick line reuses `rate`.

## Consequences

Operators get a greppable latency signal at default verbosity with zero
standing volume until a threshold is set (ticks: until one overruns its
poll_rate). Rejected: Debug level (dead under the default logger); a
tick threshold config field (nothing can set one yet); a pipeline stage
(no view of the operation end).
