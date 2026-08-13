---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0393 — Binding changes are forward-only; history the FanOut mark has passed stays as routed

**Context.** Under the old full-rescan FanOut, changing a group's bindings retroactively materialized deliveries for old messages, because every tick re-walked the whole log against the current bindings. That behavior was an accident of the rescan, not a designed feature.

**Decision.** With the marked scan, binding changes apply forward-only: messages the mark has already passed keep the deliveries they were routed (or not routed) at the time. Documented on `Bind`/`ClearBindings`.

**Consequences.** `Bind` after the fact no longer backfills old messages into a group — callers needing history must replay it explicitly. In exchange, the mark can be trusted as hardened territory and ticks stay flat-cost.
