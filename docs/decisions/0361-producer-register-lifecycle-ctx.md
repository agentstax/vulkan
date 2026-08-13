---
status: superseded
date: 2026-07-28
phase: "13"
---

# 0361 — MessageProducer.Register(ctx) captures the instance lifetime and gates Produce

**Context.** Producers had no lifetime at all — no ctx, no Run, no Close — while the presence/heartbeat design needs a lifetime goroutine per producer, and graceful shutdown needs an owner for "accept nothing new, drain what you hold". A per-call ctx cannot carry instance truth: HTTP request ctxs survive SIGTERM, and background jobs pass `context.Background()`.

**Decision.** `MessageProducer.Register(ctx)` mirrors the consumer's lifecycle: the ctx scopes the instance, stored as `lifecycleCtx`. One gate helper (`lifecycleErr`) guards `Produce`/`ProduceFunc`/`ProduceInTx`: not registered returns `ErrNotRegistered`, cancelled returns `ErrShutdownRequested`. Wind-down means drain: the batcher worker stays deliberately ctx-blind, so queued work commits and the worker exits when the queue empties, with zero new batcher code.

**Consequences.** Wind-down attempts succeed — the opposite of the circuit breaker's stop-the-buffer posture, where breaker-open means attempts fail. Superseded 2026-08-08: producer lifecycle capture was dropped by the user — `Register(ctx, topic, version)` became stateless, shutdown is observed per produce call, and `ErrShutdownRequested` plus the producer's `DisableGracefulShutdown` were deleted.
