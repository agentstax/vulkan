---
status: accepted
date: 2026-07-28
phase: "13"
---

# 0380 — The datastore interfaces are deleted in favor of the concrete types

**Context.** The consumer's `Datastore[Message]` interface had the same disease that already killed the producer's during the batching work: `NewConsumerDatastore` returns the concrete type and `NewMessageConsumer` constructs it internally from a concrete `*datastore.PostgresDatastore`, so nothing could ever substitute an alternative implementation. An unsubstitutable interface is dead API surface, not a seam. Options were: fix the coupling plus the `pgtype.UUID` token leak into a real seam, thin it to what testing needs, or collapse it.

**Decision.** Collapse — removed for now. Deleted `Datastore[Message]` (its interface-only doc comments merged onto the concrete methods: Commit's initial-backoff semantics, PartialCommit's lease-narrowing, the disableDeliveryLog notes) and the equally dead one-method `Consumer[Message]` interface, same fate as the producer's `Producer[Message]`. The concrete type is exported as `ConsumerDatastore[Message]` so labs can name it in helper signatures.

**Consequences.** Exporting the concrete type also deleted routinglab's `bindable` workaround interface, whose whole premise was the concrete type being unexported. If a real seam is wanted after the cleanup, it gets designed as one then: substitution actually possible, token/UUID types not leaking pgx, thinned to what a second implementation or tests genuinely need.
