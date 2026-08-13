---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0424 — The consumer inversion: consumption loops are worker rows the manager spawns

**Context.** The manager originally lived inside consumers, which made the consumer a special case: its consumption loops were private machinery rather than workers like everything else.

**Decision.** Ownership was inverted. The manager was lifted out of consumers, and the consumption loops (message_consumer, exception_consumer, delivery_consumer) became `worker` rows the manager spawns like any other worker. The public `Consumer` is a higher-order construct that seeds the rows and runs a manager runner; a bare `ExceptionConsumer` is a standalone retry loop with a self-claim door. Sub-consumers became pure factories, and queue/poolLimiter moved off constructor signatures.

**Consequences.** The consumer is not special, and the rest followed for free: the daemon is the same manager pointed at a different row, deployment scope is one WHERE clause, and suspending a consumption loop is the same `target_instances` toggle as any worker.
