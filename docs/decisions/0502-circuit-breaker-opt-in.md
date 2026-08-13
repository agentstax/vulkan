---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0502 — The consumer gets an opt-in circuit breaker for systemic downstream failure

**Context.** A high-throughput topic whose `consumerFunc` calls one external dependency that dies for an hour keeps retrying at full claim throughput the whole outage — every claimed message fails, gets parked/retried/dead-lettered, hammering deliveries at close to peak rate exactly when the datastore is least able to absorb it. Attempts, backoff, and dead-lettering are per-message tools; a dead dependency is a systemic failure — ~100% of traffic failing for one shared reason. Per-message backoff cannot express "the dependency is down": each message's retry clock is independent and fresh arrivals attempt immediately.

**Decision.** A circuit breaker as the aggregate signal no per-message mechanism can synthesize. Surface: a sparse handful of fields on `MessageConsumerConfig` (trip threshold, cooldown, probe size, quorum), opt-in — silently changing failure semantics under an existing consumer would be surprising. The design freezes now because it shapes the config; the trip/cooldown implementation ships in its own later phase.

**Consequences.** Harms it stops: wrongful dead-lettering (an hour outage versus a 3-attempt budget permanently dead-letters messages that would succeed at minute 61 — worst case is the instant-fail bad deploy grinding the whole backlog to the dead-letter state in minutes), a guaranteed-wasted write storm of delivery/delivery_log/lease churn for provably doomed attempts, and hammering a browning-out downstream. Observability: per-instance and group state logged, metered on the existing Meter surface, and queryable on the consumer; held traffic reads as lag, never as failures; held messages are protected by the retention drop floor unless the user opted into `AllowDropPastCommitted`.
