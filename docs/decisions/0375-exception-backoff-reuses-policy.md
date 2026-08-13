---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0375 — Exception retry backoff reuses retry.Policy instead of a separate config surface

**Context.** The exception retry timing lived in a package-level `backoff()` function that was not overridable, and making it configurable risked inventing a second backoff vocabulary beside `retry.Policy`.

**Decision.** `Retry` in `pkg/retry/retry.go` now embeds `*Policy` instead of duplicating its fields, and `Policy` gained `Delay(attempt int) time.Duration` (`BaseDelay * Exponent^attempt`, clamped to `[0, MaxDelay]`), extracted from `Wrap()`'s inline calculation. `NewDatastoreRetry`'s four call sites collapsed from five loose params to `(cfg.Retry, cfg.Logger)`. `MessageConsumerConfig` gained `Backoff *retry.Policy` (default `retry.NewDefaultRetryPolicy()`), threaded into `Datastore.RecordExceptionFailure`'s new `backoffPolicy` param and used as `backoffPolicy.Delay(exception.Attempts - 1)`; the package-level `backoff()` was deleted. `Backoff` is distinct from `ExceptionInitialBackoff`, which stays the first park's delay; `Backoff` is the curve for every retry after that.

**Consequences.** Known behavior change, confirmed acceptable: the old curve was quadratic (`(attempts-1)^2` seconds, ~18 attempts to the 5m ceiling); `Policy.Delay` is exponential (same `[1s, 5m]` envelope by default, ~9 attempts to the ceiling). One backoff-curve mental model everywhere was worth the faster ramp.
