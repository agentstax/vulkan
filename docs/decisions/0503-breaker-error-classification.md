---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0503 — Breaker input is user error classification, recorded as error_class on the delivery row

**Context.** `consumerFunc` is opaque to the library — it cannot know what "the same dependency" is or which failures share a cause. Only the user can say whether an error means the dependency is down or this particular message is at fault.

**Decision.** The user marks an error systemic (a recognized dependency-down error, composing with the planned named-errors taxonomy) versus this-message's-fault; only systemic errors count toward tripping. The classification is recorded at park time as an `error_class` enum column on the delivery row — an enum, not a bool, with values coordinated with the named-errors taxonomy — and is what later reconciliation matches on.

**Consequences.** **Rejected (out of scope):** keyed or multi-dependency breakers (per-tenant webhook endpoints) — a 5%-of-traffic dead tenant never trips an aggregate breaker, and fixing that needs per-dependency tagging plus skip-this-tenant-while-processing-others, which is per-message scheduling: the user-initiated-defer feature's territory, not a breaker.
