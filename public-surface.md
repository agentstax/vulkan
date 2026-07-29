# Vulkan public surface

Every exported symbol a user is expected to touch. `?` marks a trimming question to
settle. Names are package-qualified. Order: **plain funcs → types → vars → structs & methods**.

<details>
<summary>Excluded on purpose (exported today, internal / to demote)</summary>

All `*Datastore` types + methods · `claimBuffer` · `rangeState` · `ConcurrentBoundedRingBuffer` ·
metrics types (`ConsumerMetrics`, `QueueState`, `AbandonedRoutines`, `DutyState`, `Snapshot`) ·
parked cursor/LIFECYCLE surface (`ClaimedRange`, `LeaseRow`, `MessageException`, `MessageTerminal`,
`MessageRow`, `DeliveryRow`, `ClaimedException`, `CursorRange`, `MessageConsumer.CursorPartialCommit`,
`MessageConsumer.CloseOpenRanges`) · low-level retry (`NewRetry`, `NewDatastoreRetry`, `Retry`,
`DatastoreRetry`, `IsTransientPgError`).
</details>

---

## Plain funcs

| Function | Returns | ? |
|---|---|---|
| `datastore.NewPostgresDatastore(ctx, cfg)` | `*PostgresDatastore` | |
| `admin.NewMessageAdmin(ds, cfg)` | `*MessageAdmin` | |
| `producer.NewProducer[M](topic, ver, ds, cfg)` | `*Producer[M]` | |
| `producer.InTransaction(ctx, ds, fn)` | `error` | |
| `consumer.NewConsumer[M](group, topic, ver, queue, pool, ds, cfg)` | `*Consumer[M]` | |
| `consumer.NewMessageConsumer[M](…same as Consumer…)` | `*MessageConsumer[M]` | ? |
| `consumer.NewDeliveryConsumer[M](group, topic, ver, ds, cfg)` | `*DeliveryConsumer[M]` | ? |
| `consumer.NewExceptionConsumer[M](…same as Delivery…)` | `*ExceptionConsumer[M]` | ? |
| `consumer.MetaFromContext(ctx)` | `(MessageMeta, bool)` | |
| `concurrency.NewPressureQueue[W](limit)` | `*PressureQueue[W]` | ? hide behind Consumer? |
| `concurrency.NewWorkerPoolLimiter(limit)` | `*WorkerPoolLimiter` | ? |
| `maintain.NewFleetMaintainer(ds, cfg)` | `*FleetMaintainer` | |
| `maintain.NewMaintainer(...duties)` | `*Maintainer` | ? Fleet the only entry? |
| `maintain.NewJanitor(topic, ver, ds, cfg)` | `*Janitor` | ? |
| `maintain.NewWaterlineRoller(group, topic, ver, ds, cfg)` | `*WaterlineRoller` | ? |
| `logger.NewDefaultLogger(w, level…)` | `*slog.Logger` | |
| `retry.NewDefaultRetryPolicy()` | `*Policy` | |
| `retry.NewRetryableError(err)` | `*RetryableError` | |
| `retry.NewPermanentError(err)` | `*PermanentError` | |
| `retry.IsRetryable(err)` | `bool` | |
| `context.LifecycleContext(log)` | `(ctx, cancel)` | |
| `migrate.Validate(registry)` | `error` | ? whole pkg — custom migrations only? |
| `migrate.NewStep(...)` · `migrate.NewRunner(...)` | | ? |
| `migrate.Version/IsLocked/AssertSchemaSupported/AssertSystemSchemaSupported` | | ? |

---

## Types

**Interfaces**
- `producer.Tx` — `Exec` · `Query` · `QueryRow` · `CopyFrom` · `Raw`
- `concurrency.Queue[W]`
- `concurrency.PoolLimiter`
- `maintain.Duty` — `Register` · `Run`
- `logger.Logger` — host injects

**Func types**
- `producer.ProducerFunc` · `producer.TransactionFunc`
- `consumer.ConsumerFunc`
- `retry.RetryableFunc`

**Named types**
- `topic.SchemaVersion`
- `consumer.ConsumerType`
- `migrate.StepType`

**Plain data structs** (no receivers)
- `topic.Topic`
- `admin.VersionHealth` · `admin.GroupLag` · `admin.DestroyOptions`
- `consumer.MessageMeta`
- `maintain.FleetDuty`
- `migrate.Entity` · `migrate.Step`

---

## Vars

**Sentinel errors** (`errors.Is` targets)
- `admin.ErrDestroyDisabled`
- `topic.ErrTopicNotFound` · `ErrTopicNameTaken` · `ErrTopicNotEmpty` · `ErrTopicConfigMismatch`
- `maintain.ErrDutyLost`
- `errors.ErrNotRegistered` · `ErrAlreadyRegistered` · `ErrShutdownRequested` · `ErrLifecycleContextNotCancellable`
- `migrate.ErrNotRegistered`

**Registries**
- `topic.Registry` &nbsp;`?` user-facing, or internal?
- `system.Registry`

---

## Structs & methods

| Struct | Methods |
|---|---|
| `datastore.PostgresDatastore` | `Close` |
| `datastore.PostgresConnectionConfig` | `WithDefaults` · `Validate` |
| `admin.MessageAdmin` | `RegisterTopic` · `GetTopic` · `ListTopics` · `AlterTopic` · `RenameTopic` · `DestroyTopic` · `MigrateTopic`&nbsp;`?` · `MigrateTopics` · `FamilyHealth` · `RegisterSystem` · `MigrateSystem` |
| `admin.MessageAdminConfig` | `WithDefaults` · `Validate` |
| `topic.Config` | `WithDefaults` · `Validate` |
| `topic.AlterConfig` | `Validate` |
| `producer.Producer[M]` | `Register` · `Produce` · `ProduceFunc` · `ProduceInTx` |
| `producer.ProducerConfig` | `WithDefaults` · `Validate` |
| `producer.ProduceOptions` | `Validate` |
| `consumer.Consumer[M]` | `Register` · `Consume` · builders: `WithBatchLimit` · `WithWorkTimeout` · `WithQueueMargin` · `WithAckMargin` · `WithClaimPollRate` · `WithBackoff` |
| `consumer.MessageConsumer[M]` | `Register` · `Consume` · `Drain` |
| `consumer.DeliveryConsumer[M]` | `Register` · `Consume` · `DeliveryClaim` |
| `consumer.ExceptionConsumer[M]` | `Register` · `Consume` · `ExceptionClaim` |
| `consumer.ConsumerConfig` | `WithDefaults` · `Validate` |
| `concurrency.PressureQueue[W]` | `EnQueue` · `DeQueue` · `WaitForRoom` · `Cap` |
| `concurrency.WorkerPoolLimiter` | `WaitForPermit` · `ReleasePermit` |
| `maintain.FleetMaintainer` | `Register` · `Run` |
| `maintain.Maintainer` | `Register` · `Run` |
| `maintain.Janitor` | `Register` · `Run` |
| `maintain.WaterlineRoller` | `Register` · `Run` |
| `maintain.FleetMaintainerConfig` | `WithDefaults` · `Validate` |
| `maintain.MaintainerConfig` | `WithDefaults` · `Validate` |
| `retry.Policy` | `WithDefaults` · `Validate` |
| `retry.RetryableError` | `Error` · `Unwrap` |
| `retry.PermanentError` | `Error` · `Unwrap` |
| `migrate.Migration` | `ToStep` |
| `migrate.Runner` | `RunAll` · `RunOnce` |
