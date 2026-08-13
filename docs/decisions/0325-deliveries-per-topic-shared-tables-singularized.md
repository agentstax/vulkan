---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0325 — deliveries went per-topic, and the six remaining shared tables were singularized, before the audit table shipped

**Context.** The new attempt audit table was about to be created. Making it a shared table would have repeated the shared-table blast-radius mistake already corrected once for messages, and `deliveries` itself was still shared.

**Decision.** `deliveries` became per-topic (`delivery_<topic_id>`) first, so `delivery_log_<topic_id>` could be per-topic from the start. Alongside, the six remaining shared tables were renamed to singular: `cursors` to `cursor`, `leases` to `lease`, `bindings` to `binding`, `topics` to `topic`, `latest_keys` to `latest_key`, `idempotency_keys` to `idempotency_key`.

**Consequences.** One round of schema churn accepted so the audit table never inherits shared-table blast radius; per-topic table families and singular shared-table names become the standing conventions.
