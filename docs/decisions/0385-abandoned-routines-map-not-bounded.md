---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0385 — The abandoned-routines tracking map stays unbounded; no config knob

**Context.** `pkg/consumer/metrics/abandoned_routines.go` carried a comment proposing to bound its tracking map with a `ConcurrentBoundedRingBuffer`-style structure, on the premise that the map could grow without limit.

**Decision.** Not bounded — the premise didn't survive analysis. An entry exists only while its abandoned goroutine is still running (`Remove` fires when the routine finally returns), so the map exactly mirrors live abandoned goroutines: ~50 bytes per entry versus at least 2KB of goroutine stack plus whatever the zombie pins (the deserialized work message, the timeout ctx). Bounding the map would bound under 3% of the leak while the 97% can't be reclaimed anyway, and evicting a live key breaks the metrics: the routine's eventual `Remove` finds nothing, the otel `outstanding` counter drifts upward permanently, and the latency sample is lost.

**Consequences.** The config-vs-constant question dies with the bound — no bound, no knob. **Rejected:** the ring-buffer structure — `selfClearLatencies` can drop old samples harmlessly, but the map is a set of live keys, not a window of history, so the structure doesn't transfer. The analysis did surface a real unbounded-growth path, an `Add`/`Remove` ordering race, fixed separately.
