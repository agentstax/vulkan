---
status: accepted
date: 2026-08-01
phase: "14"
---

# 0400 — Connection-death SQLSTATEs are retryable; resource-exhaustion, corruption, and misconfiguration codes are not

**Context.** A connection killed mid-query (SQLSTATE 57P01/57P02/57P03 — admin/crash shutdown, cannot-connect-now) was classified permanent (zero retries) despite the outcome being genuinely ambiguous: "did that commit land before the connection died" deserves the same bounded retry budget every other transient blip gets.

**Decision.** `pkg/retry.IsTransientPgError` classifies those three codes retryable alongside 40P01, after auditing every `DatastoreRetry.Wrap` call site (~21 across consumer/producer/topic/metrics) for retry-safety under ambiguous commit — every write self-consumes its own re-entry (a token/status guard a retry can't re-match) or carries an idempotency key with ON CONFLICT DO NOTHING, so retrying an attempt that actually landed is a no-op. A follow-up audit added 10 more codes: 40001 (serialization_failure — currently dead code since nothing uses SERIALIZABLE, kept for correctness), 08000/08001/08003/08006/08007/40003 (connection never established or died mid-request, or Postgres explicitly can't say whether it committed), 57P05 (idle_session_timeout), 53300 (too_many_connections — never got a connection, zero ambiguity), and 57014 (query_canceled — safe only because our own ctx-driven cancellation is already filtered by the existing context.Canceled guard).

**Consequences.** **Rejected:** 08004/08P01 — can mean permanent misconfiguration (bad credentials, protocol mismatch). **Rejected:** 40002 — deterministic constraint violation. **Rejected:** the 53xxx/58xxx resource-exhaustion and I/O codes — retrying masks a real operational emergency instead of surfacing it. **Rejected:** the XX corruption class — retrying is actively dangerous. **Rejected:** 25P02 — post-failure noise the batch resolver's poison-eviction already handles; adding it would double-handle or mask the real error. Two fixes surfaced alongside: consumer `dropPartition`'s bare `DROP TABLE` became `DROP TABLE IF EXISTS`, and producer `classifyBatchFailure` stopped wrongly evicting an innocent caller on a mid-read connection kill (the IsRetryable-first guard covers this class). Verified: 8/8 pg_terminate_backend-mid-consume runs recovered transparently (previously ~50% died), plus a 32-case classification table covering every addition and exclusion.
