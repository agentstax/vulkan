---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0294 — The retry.Wrap success-after-retries bug was fixed with a plain early-return, not a classification rewrite

**Context.** The fault-isolation lab, written to confirm an already-working guarantee, found a bug in code the phase never touched: `pkg/retry.Retry.Wrap` correctly classified a nil error as non-retryable but conflated "not retryable" with "permanent failure," returning `errors.Join(err1, ..., nil)` for a call that succeeded after N retries. `errors.Join` only discards nil elements — it does not collapse the whole result to nil — so every caller's `if err != nil` treated retry-then-recover as a hard failure; for `CursorClaim` a DB blip that cleared on retry still killed `Process`'s poll loop, the exact opposite of the shipped guarantee.

**Decision.** A plain early-return — `if err == nil { return nil }` ahead of the `IsRetryable` check — because success was always meant to short-circuit before classification runs; "is this permanent" is a meaningless question for a nil result. Not a rewrite of the retryable/permanent split.

**Consequences.** Verified across all four shapes (retry-then-succeed, immediate success, exhausted retries, immediate permanent failure) and the full existing lab suite with no other changes needed — the bug was local to `Wrap`'s missing early-return, not a design flaw in the classification model. The find is also the standing argument for exercising guarantees end-to-end in labs rather than trusting them on inspection.
