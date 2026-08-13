---
status: accepted
date: 2026-08-08
phase: "14a"
---

# 0426 — Producer split into pure factory plus Register; the stored lifecycleCtx dropped

**Context.** The producer carried identity in its constructor and held a stored lifecycle context, so construction could fail operationally and shutdown was a piece of producer state.

**Decision.** `NewProducer(ds, cfg)` is a pure build step; identity moved to `Register(ctx, topic, version)`, which is stateless and returns the registered instance. The stored lifecycleCtx was dropped: shutdown is expressed by each produce call's own ctx, and the error family around the stored lifetime (`ErrShutdownRequested`, `DisableGracefulShutdown`) was deleted. `MetricEventProducer.Run` self-registers and drains, replacing its Register.

**Consequences.** One factory can register many independent lives, and a produce call carries the same contract as any database call — bounded only by the ctx it is given, with no separate admission gate. The trade: a caller producing on a background context is no longer refused during app shutdown.
