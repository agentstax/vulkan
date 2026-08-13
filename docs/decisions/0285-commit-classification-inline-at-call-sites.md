---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0285 — Commit() classification lives inline at each call site; Wrap passes pre-classified errors through

**Context.** `SkipIdempotency` broke the assumption that an error's shape alone decides retryability: the same ambiguous commit error is safe to retry only when the call carries an idempotency key. Retry-safety became a property of caller context, which a central classifier cannot see.

**Decision.** `Commit()` classification moved inline to each package's own call site (`classifyTransient`/`classifyCommit`-shaped `if`/`else`), and `DatastoreRetry.Wrap`'s default classifier was changed to pass an already-classified error through untouched instead of re-deciding it.

**Consequences.** One shared `Wrap` coexists with per-package inline commit classification — the wrapper stays generic while the context-dependent judgment sits where the context is known. Cost: classification logic repeats in `producer`/`consumer`/`topic` rather than living in one place. **Rejected:** keeping classification centralized in `Wrap` — it cannot know whether the failed call was idempotent.
