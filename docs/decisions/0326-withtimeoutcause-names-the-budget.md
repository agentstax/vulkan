---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0326 — Nested timeouts use context.WithTimeoutCause naming the budget that fired

**Context.** `pkg/consumer/consumer.go` has three nested `context.WithTimeout` call sites (`WorkTimeout`, `AckMargin`, `ShutdownTimeout`); when one fired, the caller saw a generic `context.DeadlineExceeded` with no hint of which budget expired.

**Decision.** All three sites use `context.WithTimeoutCause`, each naming its specific budget.

**Consequences.** Timeout errors identify the responsible budget. `errors.Is` classification is unaffected because `Cause()` is a separate accessor from `Err()`.
