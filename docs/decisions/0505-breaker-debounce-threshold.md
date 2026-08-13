---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0505 — The breaker's trip threshold is a conservative debounce, not statistics

**Context.** Classification is the user's word — the trip logic is not inferring a failure rate, it is debouncing the chance that one transient blip happened to be classified systemic.

**Decision.** Trip when the streak spans at least N non-empty all-systemic ticks AND at least M cumulative systemic failures within it; exception retries count toward the streak. M is a fixed-default knob (~10) folded into the trip-threshold config, deliberately NOT derived from `BatchLimit`, whose default of 1 would neuter the debounce exactly where it is needed. The bias is conservative on purpose: reconciliation makes a slow trip cheap (pre-trip damage is refunded retroactively), while a false trip is unrefunded pure loss (healthy consumption paused) — so demand more evidence, not less.

**Consequences.** Accepted corner: a trickle-traffic group (1 msg/hour) may effectively never trip — it has no write-storm problem, and its wrongful-dead-letter exposure is a handful of messages.
