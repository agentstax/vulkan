---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0369 — The payload generic is named Message, cascading Work* type names to Message*

**Context.** The producer/consumer generic type param was `WorkType`, while the datastore files already used `Message` as their own internal generic param name — an inconsistency, and a vocabulary choice worth researching before v1.

**Decision.** `Message` everywhere the param means "the caller's payload type". Landscape research: job-queue libraries (River, Oban, Faktory, Sidekiq, Asynq, BullMQ) converge on `Job`/`Task`; log/pub-sub systems (Kafka, RabbitMQ, NATS, SQS, GCP Pub/Sub, pgmq) converge on `Message`. pgmq — the closest same-category Postgres-native project, with zero Kafka lineage — independently landed on `Message`, which tipped this away from reading as "just copying Kafka": the architecture (partitioned topics, log compaction, routing-key bindings, fan-out, cursor-based consumption) is log/pub-sub shaped, not job-execution shaped. Cascaded renames: `WorkProducer` to `MessageProducer`, `NewWorkProducer` to `NewMessageProducer`, `WorkConsumer` to `MessageConsumer`, `NewWorkConsumer` to `NewMessageConsumer`, `WorkConsumerConfig` to `MessageConsumerConfig` — `WorkProducer[Message]` would have read inconsistently.

**Consequences.** `pkg/concurrency`'s own `WorkType` param (`Queue[WorkType]`/`PressureQueue[WorkType]`) was deliberately left alone — its fate was tied to the queue/pool-limiter decision, not this one.
