---
status: accepted
date: 2026-08-01
phase: "13"
---

# 0509 — The retry surface is trimmed to Policy and the two error types

**Context.** `pkg/retry` exported more than users need: `NewDefaultRetryPolicy`, `IsRetryable`, and `RetryableFunc` existed for internal wiring, while the config convention already fills unset policies.

**Decision.** `retry.NewDefaultRetryPolicy`, `retry.IsRetryable`, and `retry.RetryableFunc` are demoted. Users leave config `Retry` fields nil and `WithDefaults` fills them. What stays public: `retry.Policy` (with `WithDefaults`/`Validate`) and the two error types — `retry.RetryableError`/`retry.PermanentError` with `NewRetryableError`/`NewPermanentError`, each carrying `Error` and `Unwrap`.

**Consequences.** The public retry vocabulary is exactly what a `consumerFunc` author needs (classify an error retryable or permanent) plus one tuning struct; default construction stops being a public entry point.
