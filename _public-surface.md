# Vulkan public surface

Everything a user can touch, by audience. Trimming settled 2026-08-01; demotions
in the collapsed block are pending build work.

<details>
<summary>Excluded on purpose (exported today, internal / to demote)</summary>

All `*Datastore` types + methods (incl. `FleetDuty`, `DutyMetadata`) · `claimBuffer` · `rangeState` ·
`ConcurrentBoundedRingBuffer` ·
metrics types (`ConsumerMetrics`, `QueueState`, `AbandonedRoutines`, `DutyState`, `Snapshot`) ·
parked cursor/LIFECYCLE surface (`ClaimedRange`, `LeaseRow`, `MessageException`, `MessageTerminal`,
`MessageRow`, `DeliveryRow`, `ClaimedException`, `CursorRange`, `MessageConsumer.CursorPartialCommit`,
`MessageConsumer.CloseOpenRanges`) · low-level retry (`NewRetry`, `NewDatastoreRetry`, `Retry`,
`DatastoreRetry`, `IsTransientPgError`) ·
whole `concurrency` pkg (`Queue`, `PoolLimiter`, `PressureQueue`, `WorkerPoolLimiter` — consumers
build queue + pool internally from `ConsumerConfig`; constructors drop the two params, and the
`consumer.Buffered` leak in `NewPressureQueue[consumer.Buffered]` goes with them) ·
whole `migrate` pkg + `topic/migrations.Registry` + `system/migrations.Registry` (users migrate via
`admin.MigrateTopic(s)`/`MigrateSystem`; the CLI keeps access through `internal/` — the import-path
prefix rule spans the nested module) ·
`retry.NewDefaultRetryPolicy` / `retry.IsRetryable` / `retry.RetryableFunc` (users leave config
`Retry` fields nil and `WithDefaults` fills them; only `Policy` + the two error types stay public) ·
`consumer.ConsumerType` + `CURSOR`/`LIFECYCLE` constants + `ConsumerConfig.Type` (`NewConsumer`
defaults to cursor consumption; LIFECYCLE is reached via `NewDeliveryConsumer` directly).
</details>

---

## Shared foundation — 3 types · 2 errors · 3 funcs · 1 struct + configs

Types:
- `logger.Logger`
- `topic.SchemaVersion`
- `topic.Topic`

Errors:
- `topic.ErrTopicNotFound`
- `errors.ErrLifecycleContextNotCancellable`

Funcs:
- `datastore.NewPostgresDatastore`
- `logger.NewDefaultLogger`
- `context.LifecycleContext`

Struct methods:

`datastore.PostgresDatastore`:
- `Close`

Configs (`WithDefaults` · `Validate`):
- `datastore.PostgresConnectionConfig`
- `retry.Policy`

---

## Standard user (produce & consume) — 6 types · 3 errors · 9 funcs · 7 structs + configs

Types:
- `producer.Tx`
- `producer.ProducerFunc`
- `producer.TransactionFunc`
- `consumer.ConsumerFunc`
- `consumer.MessageMeta`
- `common.ConcurrencyPolicy`

Errors:
- `errors.ErrNotRegistered`
- `errors.ErrAlreadyRegistered`
- `errors.ErrShutdownRequested`

Funcs:
- `producer.NewProducer[M]`
- `producer.InTransaction`
- `consumer.NewConsumer[M]`
- `consumer.NewMessageConsumer[M]`
- `consumer.NewDeliveryConsumer[M]`
- `consumer.NewExceptionConsumer[M]`
- `consumer.MetaFromContext`
- `retry.NewRetryableError`
- `retry.NewPermanentError`

Struct methods:

`producer.Producer[M]`:
- `Register`
- `Produce`
- `ProduceFunc`
- `ProduceInTx`

`consumer.Consumer[M]`:
- `Register`
- `Consume`
- `WithBatchLimit`
- `WithWorkTimeout`
- `WithQueueMargin`
- `WithAckMargin`
- `WithClaimPollRate`
- `WithMessageRetry`

`consumer.MessageConsumer[M]`:
- `Register`
- `Consume`
- `Drain`

`consumer.DeliveryConsumer[M]`:
- `Register`
- `Consume`
- `DeliveryClaim`

`consumer.ExceptionConsumer[M]`:
- `Register`
- `Consume`
- `ExceptionClaim`

`retry.RetryableError` · `retry.PermanentError`:
- `Error`
- `Unwrap`

Configs (`WithDefaults` · `Validate`):
- `producer.ProducerConfig`
- `producer.ProduceOptions` (`Validate` only)
- `consumer.ConsumerConfig`
- `common.MessageOptions` (+ `Fill` · `Clamp`)

---

## Operator (administer & maintain) — 5 types · 5 errors · 6 funcs · 6 structs + configs

Types:
- `maintain.Duty`
- `maintain.DutyConstructor`
- `admin.VersionHealth`
- `admin.GroupLag`
- `admin.DestroyOptions`

Errors:
- `admin.ErrDestroyDisabled`
- `topic.ErrTopicNameTaken`
- `topic.ErrTopicNotEmpty`
- `topic.ErrTopicConfigMismatch`
- `maintain.ErrDutyLost`

Funcs:
- `admin.NewMessageAdmin`
- `maintain.NewFleetMaintainer`
- `maintain.NewMaintainer`
- `maintain.NewJanitor`
- `maintain.NewWaterlineRoller`
- `maintain.NewScheduler`

Struct methods:

`admin.MessageAdmin`:
- `RegisterTopic`
- `GetTopic`
- `ListTopics`
- `AlterTopic`
- `RenameTopic`
- `DestroyTopic`
- `MigrateTopic`
- `MigrateTopics`
- `FamilyHealth`
- `RegisterSystem`
- `MigrateSystem`

`maintain.FleetMaintainer`:
- `Register`
- `Run`

`maintain.Maintainer`:
- `Run`

`maintain.Janitor` · `maintain.WaterlineRoller` · `maintain.Scheduler` (the `Duty` impls):
- `Register`
- `Run`

Configs (`WithDefaults` · `Validate`):
- `admin.MessageAdminConfig`
- `topic.Config`
- `topic.AlterConfig` (`Validate` only)
- `maintain.FleetMaintainerConfig`
- `maintain.MaintainerConfig`
