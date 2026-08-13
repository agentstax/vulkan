---
status: accepted
date: 2026-07-12
phase: "9"
---

# 0282 — Datastore blips are retried via pkg/retry with explicit retryable/permanent classification

**Context.** A transient database error mid-claim killed the consumer outright, because nothing between the datastore and `Process`'s poll loop distinguished "try again" from "give up."

**Decision.** New `pkg/retry` — `Retry`/`DatastoreRetry` with explicit `RetryableError`/`PermanentError` classification and context-aware exponential backoff. Every `Datastore[Message]` method was split into a public method wrapping a same-named private one, so retry plumbing stays invisible at call sites.

**Consequences.** A transient failure clearing within `MaxRetries` is invisible to callers. Classification is explicit, not inferred — errors must be marked retryable or permanent at the point that knows. The public-wrap-around-same-named-private split became the datastore-wide convention.
