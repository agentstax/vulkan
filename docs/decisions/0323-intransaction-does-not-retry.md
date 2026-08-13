---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0323 — InTransaction does not retry

**Context.** `AppendMessage` auto-retries on a transient failure because its `producerFunc` is a narrow, documented-as-rerunnable piece of business logic. `InTransaction`'s closure is a much bigger surface: multiple `ProduceInTx` calls plus arbitrary caller side effects between them.

**Decision.** `InTransaction` runs the closure exactly once. A caller who wants retry-on-blip wraps their own loop around it.

**Consequences.** No silent rerun of arbitrary code from an unpredictable point. It also made the ambiguous-commit classification question for mixed `SkipIdempotency` across targets moot — nothing to classify when nothing retries; the lab confirms a genuine commit-time failure surfaces completely unclassified regardless of the `SkipIdempotency` mix.
