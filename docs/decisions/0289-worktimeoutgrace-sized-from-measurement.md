---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0289 — WorkTimeoutGrace defaults to 100ms, sized from a measured scheduler latency

**Context.** The timeout race needs slack beyond `WorkTimeout` before the caller gives up, but the default should reflect what the slack is actually for, not a guess.

**Decision.** `WorkTimeoutGrace` — a new config field, default 100ms, validated `> 0`, folded into the `ShutdownTimeout` inequality. The sizing rests on a measurement: Go's scheduler wakeup latency from a context deadline to a blocked goroutine's channel send is p99 under 1ms even under artificial scheduler pressure (2000 trials). Almost none of the 100ms is scheduling risk; it is discretionary slack for the callee's own cancellation-response time (for example a pgx cancel-request round trip), sized to roughly one same-region network hop.

**Consequences.** A cooperative `consumerFunc` that reacts promptly to cancellation returns cleanly inside the grace window instead of being counted abandoned; the measurement is on record for anyone tempted to shrink the default toward the sub-millisecond scheduler floor.
