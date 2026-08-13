---
status: accepted
date: 2026-08-12
phase: "14a"
---

# 0466 — `__system.job_requests` runs with `DeliveryLogModeAll` so successes leave rows

**Context.** Derived job-request status needs a positive record that a group ran a request successfully. On the normal delivery path, success leaves no `delivery_log` row, so "this group succeeded on this request" would be unknowable after the fact.

**Decision.** The `__system.job_requests` topic is configured with `DeliveryLogModeAll`, so successful deliveries also write a `delivery_log` row, giving the status derivation its success records.

**Consequences.** One extra row per successful delivery — acceptable here because the topic produces at most one message per job per minute. This stays a per-topic setting: hot topics do not pay it, and their success path keeps writing nothing.
