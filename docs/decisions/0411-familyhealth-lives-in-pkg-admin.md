---
status: accepted
date: 2026-08-06
phase: "14a"
---

# 0411 — FamilyHealth and VersionHealth live in pkg/admin, not as a pkg/metrics composition

**Context.** The retire verdict for a topic version reads consumer-group lag and topic compaction state — data pkg/metrics-style code already exposes — so a metrics-side home looked plausible, and a working `pkg/metrics.HealthMetrics` was actually built.

**Decision.** It was deliberately reverted: `FamilyHealth`/`VersionHealth` live in `pkg/admin`. The retire verdict is intrinsically `DestroyTopic`'s own question — admin's lifecycle verb — not a general metrics concern the way a running consumer's live gauges are. It reads through `consumer/metrics.ConsumerMetricsDatastore` and `topic/metrics.TopicMetricsDatastore` the same way any other caller would, rather than owning its own copy of that data.

**Consequences.** One read path for the underlying facts (no second derivation of lag or compaction state), and the verdict sits next to the verb it gates. **Rejected:** `pkg/metrics.HealthMetrics` — built, then reverted for the ownership reason above.
