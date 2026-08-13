---
status: accepted
date: 2026-07-16
phase: "11"
---

# 0321 — Multi-topic atomic publish exposes the transaction via a closure, not a declarative PublishAll

**Context.** `WorkProducer.Produce` opens and owns its own transaction around exactly one `AppendMessage`, so there was no way to publish to multiple topics atomically, or to combine a publish with the caller's own business writes in one transaction.

**Decision.** Package-level `producer.InTransaction(ctx, ds, func(ctx, tx pgx.Tx) error)` opens one transaction, runs the caller's closure, commits on nil and rolls back otherwise. `WorkProducer[T].ProduceInTx(ctx, tx, producerFunc, opts)` — the tx-taking sibling to `Produce` — is called once per target inside that closure.

**Consequences.** The caller can interleave arbitrary business writes and side effects across topics in a single transaction; a second target's error rolls back every target, not just itself. The cost is `pgx.Tx` appearing in a public producer signature. **Rejected:** a closure-free, declarative `PublishAll(Target(p1, fn1, opts1), ...)` that never exposes `pgx.Tx` — costs too much flexibility, since interleaving arbitrary caller writes between targets is the whole point of exposing the transaction.
