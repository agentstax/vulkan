---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0362 — NewMessageProducer takes the raw PostgresDatastore and builds its datastores internally

**Context.** The producer constructor was asymmetric with `NewMessageConsumer`: callers had to build a producer datastore themselves just to hand it back, and labs only ever constructed one for that purpose.

**Decision.** `NewMessageProducer[T](t, ds, cfg)` takes the raw `*datastore.PostgresDatastore` plus the renamed `MessageProducerConfig` and constructs `producerDatastore` and `topic.TopicDatastore` internally, matching `NewMessageConsumer`. `NewProducerDatastore` went unexported. `pkg/topic` exported `TopicDatastore`/`NewTopicDatastore` rather than growing a one-off package-level check function.

**Consequences.** The `Message` type parameter is no longer inferable — call sites name it explicitly. Exporting the topic datastore was judged fine ahead of the public-API-to-root restructure. The topic identity later moved out of the constructor into `Register(ctx, topic, version)` (2026-08-08); the internal-datastore-construction shape stands.
