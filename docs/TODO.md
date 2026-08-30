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
2. DONE 2026-08-30 (uncommitted) -- **Target topic + stored version.** `schedule_config.topic_id` becomes
   the target (the owner CHECK narrows to system_id / consumer_group_id);
   add `schema_version`; the produce path takes the row's version --
   the one internal delta, an append that does not compute the version
   from a type. `scheduled_at` onto the message options document and
   `consumergroup.MessageMeta.ScheduledAt`. Status/messages reads move
   to the target topic's delivery_log by message key.
3. DONE 2026-08-30 (uncommitted) -- **The handle.** `pkg/scheduler` (API package): `NewScheduler`,
   `Register[Message topic.Versioned](ctx, name, expression string,
   topicName, payload, cfg)` -> `*SchedulerInstance[Message]`
   (`Registered` row + `Payload`; the row field is not named `Schedule`
   because that is the verb), `Schedule(ctx)` = a per-instance system
   manager's Run. `RegisterSchedule` on admin stays (alerts + CLI path);
   playground 06 and a schedulelab handle section drive the handle.
4. **Alerts through the handle + docs sweep.** (JobRequest retirement,
   alerts consuming `alert.JobPayload`, and the playground 06 rewrite
   landed with step 2.) partitioncount / compactionreadcost register
   through the handle; quickstart / architecture / table-design /
   rabbitmq-sqs pages swept; the schedules page's PROPOSED aside comes
   off.
5. Closeout: full fresh-DB suite, `just verify`, PROPOSED aside off,
   HISTORY entry, memory.

Settled during step 2 (2026-08-30): every schedule is the system's --
the nullable owner pair + `num_nonnulls` CHECK are gone (a group-owned
schedule would re-couple what [0621] decoupled; add the column
additively if a real writer appears); `RegisterSchedule` warns VK0058
when the target's DeliveryLogMode keeps no success rows;
`ScheduleConfig.Metadata` stays as operator annotation.

Done 2026-08-30: every `[Message any]` outside pkg/producer is
`[Message topic.Versioned]`; `common.MessageRow` keeps `any` because
pkg/common (infrastructure) cannot import the pkg/topic vocabulary root.
