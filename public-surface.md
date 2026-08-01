# Vulkan public surface

Every exported symbol a user is expected to touch, organized by audience:
**shared foundation** (both audiences) → **standard user** (produce & consume) →
**operator** (administer & maintain). Trimming questions settled 2026-08-01 —
this is the target surface; demotions below are pending build work. Names are
package-qualified. Within each layer: **plain funcs → types → errors → structs & methods**.

<details>
<summary>Excluded on purpose (exported today, internal / to demote)</summary>

All `*Datastore` types + methods · `claimBuffer` · `rangeState` · `ConcurrentBoundedRingBuffer` ·
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

## Shared foundation — both audiences

The connection, logging, and lifecycle plumbing every Vulkan process starts from.

| Function | Returns | Notes |
|---|---|---|
| `datastore.NewPostgresDatastore(ctx, cfg)` | `*PostgresDatastore` | |
| `logger.NewDefaultLogger(w, level…)` | `*slog.Logger` | |
| `context.LifecycleContext(log)` | `(ctx, cancel)` | |

**Types**
- `logger.Logger` (interface) — host injects
- `topic.SchemaVersion` (named type) — every constructor and admin method takes it
- `topic.Topic` (plain data struct) — returned by admin lookups; exported field on producer/consumer

**Errors**
- `errors.ErrLifecycleContextNotCancellable`
- `topic.ErrTopicNotFound` — producer `Register` and every admin lookup

| Struct | Methods |
|---|---|
| `datastore.PostgresDatastore` | `Close` |
| `datastore.PostgresConnectionConfig` | `WithDefaults` · `Validate` |
| `retry.Policy` | `WithDefaults` · `Validate` — the `Retry` field on both audiences' configs |

---

## Standard user — produce & consume

An application developer moving messages through topics an operator already registered.

| Function | Returns | Notes |
|---|---|---|
| `producer.NewProducer[M](topic, ver, ds, cfg)` | `*Producer[M]` | |
| `producer.InTransaction(ctx, ds, fn)` | `error` | |
| `consumer.NewConsumer[M](group, topic, ver, ds, cfg)` | `*Consumer[M]` | queue/pool params dropped — built internally |
| `consumer.NewMessageConsumer[M](…same as Consumer…)` | `*MessageConsumer[M]` | |
| `consumer.NewDeliveryConsumer[M](group, topic, ver, ds, cfg)` | `*DeliveryConsumer[M]` | |
| `consumer.NewExceptionConsumer[M](…same as Delivery…)` | `*ExceptionConsumer[M]` | |
| `consumer.MetaFromContext(ctx)` | `(MessageMeta, bool)` | |
| `retry.NewRetryableError(err)` | `*RetryableError` | handler error classification |
| `retry.NewPermanentError(err)` | `*PermanentError` | |

**Types**
- `producer.Tx` (interface) — `Exec` · `Query` · `QueryRow` · `CopyFrom` · `Raw`
- `producer.ProducerFunc` · `producer.TransactionFunc` · `consumer.ConsumerFunc` (func types)
- `consumer.MessageMeta` (plain data struct)

**Errors** — the producer/consumer lifecycle discipline
- `errors.ErrNotRegistered` · `ErrAlreadyRegistered` · `ErrShutdownRequested`

| Struct | Methods |
|---|---|
| `producer.Producer[M]` | `Register` · `Produce` · `ProduceFunc` · `ProduceInTx` |
| `producer.ProducerConfig` | `WithDefaults` · `Validate` |
| `producer.ProduceOptions` | `Validate` |
| `common.MessageOptions` | `WithDefaults` · `Fill` · `Clamp` · `Validate` — nil-safe, always held as `*MessageOptions` (+ `common.ConcurrencyPolicy`: `Allow` · `Forbid` · `Defer`) |
| `consumer.Consumer[M]` | `Register` · `Consume` · builders: `WithBatchLimit` · `WithWorkTimeout` · `WithQueueMargin` · `WithAckMargin` · `WithClaimPollRate` · `WithMessageRetry` |
| `consumer.MessageConsumer[M]` | `Register` · `Consume` · `Drain` |
| `consumer.DeliveryConsumer[M]` | `Register` · `Consume` · `DeliveryClaim` |
| `consumer.ExceptionConsumer[M]` | `Register` · `Consume` · `ExceptionClaim` |
| `consumer.ConsumerConfig` | `WithDefaults` · `Validate` |
| `retry.RetryableError` | `Error` · `Unwrap` |
| `retry.PermanentError` | `Error` · `Unwrap` |

---

## Operator — administer & maintain

Whoever owns the deployment: registers and migrates topics, runs fleet upkeep,
watches health. (Consumers embed their own `Janitor`/`WaterlineRoller` — the
standalone `maintain` constructors are for dedicated maintenance deployments.)

| Function | Returns | Notes |
|---|---|---|
| `admin.NewMessageAdmin(ds, cfg)` | `*MessageAdmin` | |
| `maintain.NewFleetMaintainer(ds, cfg)` | `*FleetMaintainer` | |
| `maintain.NewMaintainer(...duties)` | `*Maintainer` | |
| `maintain.NewJanitor(topic, ver, ds, cfg)` | `*Janitor` | |
| `maintain.NewWaterlineRoller(group, topic, ver, ds, cfg)` | `*WaterlineRoller` | |

**Types**
- `maintain.Duty` (interface) — `Register` · `Run`
- `admin.VersionHealth` · `admin.GroupLag` · `admin.DestroyOptions` (plain data structs)
- `maintain.FleetDuty` (plain data struct)

**Errors**
- `admin.ErrDestroyDisabled`
- `topic.ErrTopicNameTaken` · `ErrTopicNotEmpty` · `ErrTopicConfigMismatch`
- `maintain.ErrDutyLost`

| Struct | Methods |
|---|---|
| `admin.MessageAdmin` | `RegisterTopic` · `GetTopic` · `ListTopics` · `AlterTopic` · `RenameTopic` · `DestroyTopic` · `MigrateTopic` · `MigrateTopics` · `FamilyHealth` · `RegisterSystem` · `MigrateSystem` |
| `admin.MessageAdminConfig` | `WithDefaults` · `Validate` |
| `topic.Config` | `WithDefaults` · `Validate` |
| `topic.AlterConfig` | `Validate` |
| `maintain.FleetMaintainer` | `Register` · `Run` |
| `maintain.Maintainer` | `Register` · `Run` |
| `maintain.Janitor` | `Register` · `Run` |
| `maintain.WaterlineRoller` | `Register` · `Run` |
| `maintain.FleetMaintainerConfig` | `WithDefaults` · `Validate` |
| `maintain.MaintainerConfig` | `WithDefaults` · `Validate` |
