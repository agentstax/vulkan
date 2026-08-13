---
status: accepted
date: 2026-08-01
phase: "13"
---

# 0507 — The public surface is organized by three audiences; plumbing types are demoted

**Context.** Before v1 locks the API, everything exported needed an owner and a reason. The trim sorts the surface by who touches it, and everything without an end-user reason to exist gets demoted to internal.

**Decision.** Three audiences: shared foundation (`logger.Logger`, `topic.Topic`, `datastore.NewPostgresDatastore`, `context.LifecycleContext`, `retry.Policy`, ...), standard user (produce and consume: `producer.NewProducer[M]`, `consumer.NewConsumer[M]`, the sub-consumers, the lifecycle sentinels, `common.MessageOptions`, ...), and operator (`admin.MessageAdmin`, the `maintain` package and its `Duty` implementations). Demoted on purpose: all `*Datastore` types and methods (including `FleetDuty`, `DutyMetadata`), `claimBuffer`, `rangeState`, `ConcurrentBoundedRingBuffer`, the metrics read-models (`ConsumerMetrics`, `QueueState`, `AbandonedRoutines`, `DutyState`, `Snapshot`), the cursor/lifecycle row surface (`ClaimedRange`, `LeaseRow`, `MessageException`, `MessageTerminal`, `MessageRow`, `DeliveryRow`, `ClaimedException`, `CursorRange`, `MessageConsumer.CursorPartialCommit`, `MessageConsumer.CloseOpenRanges`), and low-level retry (`NewRetry`, `NewDatastoreRetry`, `Retry`, `DatastoreRetry`, `IsTransientPgError`).

**Consequences.** The sub-consumers (`consumer.NewDeliveryConsumer[M]`, `consumer.NewExceptionConsumer[M]`) and the whole `maintain` surface (`FleetMaintainer`, `Maintainer`, `Janitor`, `WaterlineRoller`, `Scheduler`) deliberately stay public. Demotions in the excluded block are pending build work — the list is the contract, the lowering follows.
