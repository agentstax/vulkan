---
status: accepted
date: 2026-08-16
phase: "14"
---

# 0525 — ProduceBatch: one call, N messages, one transaction

**Context.** The metrics collector and alert handlers batch by spawning a
goroutine per message so the producer's batcher collapses the concurrent
singles -- too heavy to teach users as THE batching answer. Survey:
Kafka/PubSub batch transparently client-side (Vulkan has that in
pkg/producer/batcher); SQS/pgmq expose an explicit batch verb beside the
single send. controller.AppendMessageBatch -- the batcher's own flush verb
-- already commits N appends in one transaction.

**Decision.**
- ProducerInstance.ProduceBatch(ctx, items ...*ProduceItem) returning
  ([]*ProduceResult, error), a sibling of Produce/ProduceFunc/ProduceInTx.
- ProduceItem{Message, Options} + NewProduceItem is a producer-layer
  shape, not an alias of controller.Append: Append REQUIRES an
  IdempotencyKey, ProduceItem FORBIDS one -- different contracts, so the
  per-layer shape ladder (datastore AppendData -> controller Append ->
  producer ProduceItem) applies, not the alias pattern.
- At least 1 item; one transaction, all-or-nothing; results in argument
  order; the controller's failedIdx joins the error text.
- Caller IdempotencyKeys rejected: one hot key would stall the whole
  shared transaction. A fresh v7 is minted per item (AppendMessageBatch
  requires keys for its internal rerun dedupe), so like unkeyed single
  Produce, a caller retrying an ambiguous commit double-publishes.
- A ProduceBatch never collapses with concurrent singles or other batches
  -- the caller already assembled the batch.

**Consequences.** Atomic multi-message publish is first-class; the
goroutine fan-outs in collectConsumerGroup and both produceCheckSummary
methods become one call each. Rejected: variadic bare messages under one
ProduceOptions (the motivating callers vary RoutingKey/CompactionKey per
message); SQS-style per-entry partial results (miserable to consume);
feeding N entries through the batcher (a MaxSize split breaks
atomicity); caller keys in batches (revisitable -- additive later).
