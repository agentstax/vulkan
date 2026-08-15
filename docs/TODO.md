# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

**Config becomes code-owned** ([0518] [0520]) — chunk plan. Replaces the
layering plan of [0515]/[0517]; chunks 1-3 of that plan are committed and
their override machinery comes back out here.

1. **Topic write path** — DONE. AlterTopic, AlterTopicConfig,
   UpdateTopic (controller + datastore), AlterTopicData, toAlterTopicData /
   toAlterValue, `topic config set|unset` deleted; registerTopic's found path
   now writes the declared mutable config through `replaceTopicConfig`, rejecting
   only a changed partition_size and logging old -> new when stored config is
   replaced; `topic config get` kept, showing DEFAULT alongside VALUE;
   `vulkan topic register` deleted outright ([0521]) along with its mismatch
   diff table, and cmd/vulkan/README.md's register section replaced;
   registeridempotencylab and idempotencykeyslab now assert the newest
   declaration wins and only PartitionSize is rejected; reservedtopiclab's
   alter step dropped. Chunk 3 owes reservedtopiclab a declared-config
   assertion once ensureSystemTopic collapses into a register.
2. **Group/worker write path** — DONE. AlterGroup, AlterWorker(s),
   AlterGroupConfig, AlterWorkerConfig, MetadataValue, mergeMetadata,
   declaresKey, applyOverrides, WorkerMetadataData, `group config set|unset`
   and pkg/common/update.go all deleted; worker metadata is now the plain
   typed value per key, so every kind's metadata struct holds `time.Duration`
   / `int` / `common.MessageOptions` directly and its Validate reads the
   field. insertWorker is a latest-wins upsert -- it writes metadata onto an
   existing row instead of merging, and errors if the row went missing
   between the insert and the update. target_instances stays create-only:
   0 is how a worker is suspended, so a redeclaration writing it would resume
   a suspended row -- the same rule chunk 3 owes cron's Suspended, and worth
   settling for both there. `group config get`
   prints KEY / WORKER / VALUE. Two labs hand-built the layered shape for
   Provision (dutybackofflab, cronlab) and now pass the flat one.
3. **Cron + alert config** ([0520] [0521]) — delete cron_alter.go,
   cron_register.go (taking fieldDiff with it), AlterCronJob,
   AlterCronJobConfig, ErrCronJobConfigMismatch; RegisterCronJob goes
   latest-wins and logs a schedule re-seed; a JobConfig per built-in alert
   (Schedule, Threshold, current consts as WithDefaults values) composed into
   admin.RegisterSystemConfig; ensureSystemCronJob + ensureSystemTopic
   collapse into ordinary register calls; register must leave Suspended alone.
4. **Labs + close-out** — topiclab PROOF 2 is already failing on unmodified
   code and blocks the gate: it asserts topicA's remaining partitions are
   exactly [1] after a drop, but create-ahead ([0512] [0513]) pre-creates
   partition 2 from a goroutine, so the result races between [1] and [1 2].
   Decide whether the lab waits for create-ahead or expects both partitions.
   Then: alertlab's threshold assertion inverts (a
   declaration applies, rather than an alter surviving); labs standing in for
   a producer or consumer move off RegisterTopic to GetTopic; HISTORY /
   ROADMAP close-out; fresh-DB suite.
