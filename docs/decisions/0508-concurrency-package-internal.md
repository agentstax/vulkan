---
status: accepted
date: 2026-08-01
phase: "13"
---

# 0508 — The concurrency package goes internal; consumers build queue and pool from ConsumerConfig

**Context.** `concurrency.Queue`, `PoolLimiter`, `PressureQueue`, and `WorkerPoolLimiter` were exported, and consumer constructors took a queue and a pool limiter as params — forcing users to assemble internal machinery and leaking `consumer.Buffered` through `NewPressureQueue[consumer.Buffered]`.

**Decision.** The whole `concurrency` package is hidden. Consumers build their queue and pool internally from `ConsumerConfig`; the constructors drop the two params, and the `consumer.Buffered` leak goes with them.

**Consequences.** Users tune concurrency through config fields, not by constructing internals; the package's types stop being API the library must keep stable.
