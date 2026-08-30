---
status: accepted
date: 2026-08-30
phase: pre-v1
---

# 0621 — a schedule is a producer on a cron expression; "cron job" is renamed "schedule" everywhere

Amends [0468], [0471], [0520], [0531]; the noun applies on top of [0611]/[0613].

## Context

`MessageAdmin.RegisterCronJob` stores an `any` payload and the fleet
scheduler produces a `cron.JobRequest` envelope onto
`__system.job_requests` with the job's name as routing key. A user
consuming their own job must know the system topic constant, that
name == binding, and unmarshal `JobRequest.Payload` by hand; nothing
types the two ends. The register/consume shape is also unlike the
other two handles (`NewProducer` / `NewConsumer` -> `Register[T]` ->
verb). Two false starts were rejected in design: a handler on the job
config (couples the consumer to the schedule), and a payload func run
by a per-job loop in the user's process (nothing fires unless that
process runs -- the same coupling from the other side). The spec is
website/src/content/docs/guides/schedules.mdx.

## Decision

- A schedule is a producer on a cron expression. The system produces
  the stored message onto the user's own topic; consumers are ordinary
  `consumer.Register[T]` groups on that topic and know nothing about
  the schedule.
- Third API handle, the producer/consumer mirror:
  `scheduler.NewScheduler(ds, cfg)` -> `Register[Message
  topic.Versioned](ctx, name, expression, topicName, payload *Message,
  cfg)` returns `*scheduler.SchedulerInstance[Message]` ->
  `Schedule(ctx)` runs the system manager (nothing per-schedule; it
  exists so a register-and-exit program is a manager somewhere).
- `Register` stores the marshaled payload and `Message`'s schema
  version on the row; the fleet worker produces that JSON at that
  version -- the append path takes the row's version instead of
  computing it from a type. `topic_id` is the target topic, no longer
  an owner column. Message key = the schedule name, compaction on,
  concurrency/timeout from config, idempotency key from (scheduled
  time, schedule id) -- the produce mechanics of [0463]/[0471] are
  unchanged.
- `scheduled_at` reaches the handler through `consumergroup.MessageMeta`,
  never the payload.
- The resource is named "schedule" on every surface: domain package
  `pkg/schedule` (read-model `schedule.Schedule`, the parsed cron string
  `schedule.Expression`), tables `schedule_config` / `schedule_config_log`
  / `schedule_cursor` with column `expression`, admin `RegisterSchedule`
  / `RunSchedule` / `SuspendSchedule` / `UnsuspendSchedule` /
  `ScheduleStatus` / `ScheduleMessages` / `DestroySchedule`, CLI `vulkan
  schedule ...`, log attributes `schedule` / `schedule_id`,
  `ErrScheduleNotFound`. The worker becomes `pkg/schedule/producer`.
- `cron.JobRequest` and `cron.TopicName` leave the user surface; the
  built-in alerts keep `__system.job_requests` as their own target topic
  and register through the same handle.

## Consequences

- One noun on code, admin, CLI, tables, and logs -- the code/CLI split
  ("Schedule" in Go, "cron" on the command line) was the tell that
  forced the rename. Temporal and EventBridge Scheduler name the same
  resource "Schedule"; K8s keeps "CronJob" because what it fires is a
  batch job, not a message.
- A schedule's name shares the target topic's message-key space with
  the user's own keys; documented, no config knob until someone hits it.
- Rejected: package `cronjob` (handle collides with the read-model),
  package `schedule` for the API handle (collides with the expression
  type), plural `NewSchedulers`, topic-first `Register` argument order,
  and both coupled shapes named in Context.
