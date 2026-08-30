# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Schedules -- the cron-job redesign and rename [0621]

Spec: website/src/content/docs/guides/schedules.mdx (PROPOSED aside
comes off when the last step ships). Sequenced so each step builds and
its labs pass alone; the full fresh-DB suite runs at the end.

1. DONE 2026-08-30 (uncommitted) -- **Rename the domain.** `pkg/cron` -> `pkg/schedule` (read-model
   `Schedule`, `cron.Schedule` -> `schedule.Expression`, robfig vendor
   moves with it), `pkg/cron/scheduler` -> `pkg/schedule/producer`,
   tables `cron_job_config` / `cron_job_config_log` / `cron_job_cursor`
   -> `schedule_*` with `schedule` column -> `expression` (baseline DDL
   in place, drop+recreate), admin verbs `*CronJob` -> `*Schedule`
   (`CronJobRequests` -> `ScheduleMessages`), CLI `vulkan cron` ->
   `vulkan schedule`, log keys `cron_job` / `cron_job_id` -> `schedule`
   / `schedule_id` (CONVENTIONS registry + VK0013/VK0025/VK0037 fix
   text, placeholders, docs pages, codes.json), CONVENTIONS ## Tables
   examples and the vocabulary row ("cron jobs keep their name" ->
   "schedules"), labs (`cronlab`, `just cron-lab`), sandbox SQL
   mirror. ~90 Go files reference cron today.
2. **Target topic + stored version.** `schedule_config.topic_id` becomes
   the target (the owner CHECK narrows to system_id / consumer_group_id);
   add `schema_version`; the produce path takes the row's version --
   the one internal delta, an append that does not compute the version
   from a type. `scheduled_at` onto the message options document and
   `consumergroup.MessageMeta.ScheduledAt`. Status/messages reads move
   to the target topic's delivery_log by message key.
3. **The handle.** `pkg/scheduler` (API package): `NewScheduler`,
   `Register[Message topic.Versioned](ctx, name, expression, topicName,
   payload, cfg)` -> `*SchedulerInstance[Message]`, `Schedule(ctx)` =
   system manager. `RegisterSchedule` on admin stays for the CLI (no
   type argument: payload as JSON, version explicit).
4. **Alerts + JobRequest retirement.** partitioncount /
   compactionreadcost register through the handle with
   `__system.job_requests` as target and consume `alert.JobPayload`
   directly; `cron.JobRequest`, `cron.TopicName`, `cron.NewJobRequest`
   deleted from the public surface. Playground 06 rewrite; quickstart
   / architecture / table-design / rabbitmq-sqs pages swept.
5. Closeout: full fresh-DB suite, `just verify`, PROPOSED aside off,
   HISTORY entry, memory.
