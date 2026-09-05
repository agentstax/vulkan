---
status: superseded
date: 2026-08-21
phase: pre-v1
---

# Metric declarations join the shared VK registry

Superseded by [0647].

## Context

The stop-line work ([0567], [0568]) needed metric declarations -- name,
kind, unit, description -- so the session flusher, otelvulkan's # HELP,
and `vulkan explain` share one source. The first build put a name-keyed
registry in pkg/metrics: a second registry mechanism beside diagnostic's
code registry.

## Decision

- diagnostic.Metric{Code, Name, Kind, Unit, Description} is the third
  declaration kind beside Error and Event: diagnostic.NewMetric
  registers in the one code registry (shared VK serial space),
  KindMetric, Metrics() lister, Docs() derived from the code.
- Kind and unit are plain strings on the diagnostic side --
  infrastructure cannot import pkg/metrics; a tools/conventions walk
  holds every declaration to the pkg/metrics vocabulary (reserved
  prefix, Kind/Unit validators, banned words in descriptions).
- GetMetric(name) scans the code registry: a Measurement on the wire
  carries only its name, and Measurement gains no code field -- it is a
  user-facing wire type. No second name-keyed map; the registry fills
  once at init and stays small.
- The session counters declare as VK0042-VK0051; `vulkan explain`
  resolves a metric by code, full name, or stop-line attr key
  (ready_count -> the declared name's last segment), each with a
  hand-written docs page.
- VK0052 "abandoned-routine events dropped" declares the durable loss
  both when a batch fails to land and when the queue cap drops events
  (counted at enqueue, reported by the next flush tick).

## Consequences

- One serial space for everything explain renders; the completeness
  walk's regex covers NewMetric, and pkg/consumer + pkg/metrics are now
  linked into the conventions binary (the walk had silently missed
  VK0041).
- Session-counter flush and abandoned-event drain merged into one
  MetricsProducer.Run tick loop (ProducerConfig.SessionFlushRate,
  default 30s); events land up to one tick late, and every drop is
  reported under VK0052.
- Evaluated and kept both read paths: ConsumerGroupSnapshot stays a
  live DB read (a level -- what exists now), never rerouted through the
  topic heads; the session series are flows (what one process did) and
  neither derives the other. The heads remain the cache for
  staleness-tolerant readers (otelvulkan, Prometheus).
