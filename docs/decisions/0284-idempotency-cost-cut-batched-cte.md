---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0284 — The idempotency check's measured cost was cut with a batched CTE and a per-call opt-out

**Context.** The naive idempotency-check implementation — a separate round trip before the `message_log` insert — was measured at 20-36%+ extra disk and 15-30% lower throughput, too much to impose unconditionally on every publish.

**Decision.** Two cuts: the check and the insert were combined into one batched data-modifying CTE (one round trip instead of two), and a per-call `SkipIdempotency` opt-out lets callers who tolerate duplicates skip the claim write entirely.

**Consequences.** The steady-state overhead is bounded (verified with the real sweep cadence running); `SkipIdempotency` genuinely double-publishes and is the caller's informed choice. The opt-out also forced a deeper change: retry-safety now depends on caller context, not just error shape, which moved commit classification inline to call sites (recorded separately).
