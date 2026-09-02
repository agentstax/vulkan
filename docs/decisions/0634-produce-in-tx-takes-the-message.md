---
status: accepted
date: 2026-09-02
phase: "pre-v1"
---

# 0634 — ProduceInTx takes the message

**Context.** The produce surface was `Produce` (value) / `ProduceFunc` (closure) / `ProduceInTx` (closure): the caller-owned-transaction form had no value-taking shape, so a static payload inside `InTransaction` cost a closure per topic, and playground scenario 02 read as nesting for its own sake. Every closure also spelled `_ string` for an idempotencyKey param almost no caller uses — since [0622] a caller who needs to know the key supplies it through `ProduceOptions.IdempotencyKey`, so the param's one legitimate use already has a mechanism. River's `InsertTx(ctx, tx, args, opts)` is the precedent for the value form.

**Decision.** The four verbs complete the {value, closure} × {producer-owned tx, caller-owned tx} table: `Produce(ctx, message, options)` and `ProduceFunc(ctx, producerFunc, options)` stand; `ProduceInTx(ctx, tx, message, options)` becomes value-taking; today's closure form is renamed `ProduceFuncInTx(ctx, tx, producerFunc, options)`. `ProducerFunc` drops to `func(ctx context.Context, tx Tx) (*Message, error)`. Pre-v1, both signature changes edit call sites in place.

**Consequences.** The multi-topic `InTransaction` block is a statement per topic instead of a closure per topic; the closure forms remain for payloads computed from reads inside the same transaction. What the types still cannot say — produce last, because a produce holds a lock on consumer progress until commit — stays a docs answer, stated on the produce-in-tx page. `InTransaction` remains no-retry (user-settled). **Rejected:** a `KeyFromContext` ctx accessor mirroring `MetaFromContext` — a second read path for a fact the caller already owns through `ProduceOptions.IdempotencyKey`.
