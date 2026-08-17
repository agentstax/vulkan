# TODO

Sliding window of in-flight work only. Future work lives in ROADMAP.md;
shipped work in HISTORY.md; decision rationale in DECISIONS.md ->
docs/decisions/.

## Layered-pattern refactor — chunk queue

Expanded 2026-08-17 from ROADMAP's "Refactor remaining packages into the
worker/topic layered pattern" after a pkg-wide audit against CONVENTIONS.md.
One chunk = one review; work top to bottom within a section, reorder freely.

- Template for kind subpackages: pkg/alert/partitioncount and
  pkg/alert/compactionreadcost (audited identical modulo kind name).
- Audited clean: pkg/system, pkg/producer/batcher, pkg/consumer/binding,
  pkg/consumer/message, messageconsumer's datastore, the alert kinds.
- Receiver/name-length nits went to ROADMAP's naming pass; the empty
  SystemConfig stays with ROADMAP's public-surface trim ([0516]).

### Quick fixes

- [x] **Consumer option setters mutate a validated config.** Resolved
      2026-08-17: all six With* methods had zero callers repo-wide
      (examples, bench, CLI included) — pkg/consumer/options.go deleted as
      dead code. The exported Config pointer field itself stays with the
      unexport chunk.
- [x] **Producer/batcher config duplicated.** Resolved 2026-08-17: the four
      flat Batch* fields on ProducerConfig became one nested
      `Batch batcher.BatcherConfig` field — fields, docs, defaults, and
      validation now live only in the batcher; ProducerConfig's
      WithDefaults/Validate delegate (Batch.Logger inherits Logger when
      unset, errors wrap as "Batch: ..."). Public change:
      cfg.BatchMaxSize -> cfg.Batch.MaxSize. producer-batch-lab green.
- [x] **Metrics datastore retry field name.** Resolved 2026-08-17: field
      renamed `Retry` -> `DatastoreRetry` (and the constructor local to
      `datastoreRetry`), matching every sibling datastore; six Wrap call
      sites updated. No references existed outside the package.
- [x] **Id casing.** Resolved 2026-08-17, wider than surveyed: the sweep
      found the same casing in the messageconsumer / consumer / base
      datastores, pkg/metrics/routing.go, pkg/migrate, and ~40 labs.
      Every TopicID/topicID/groupID/messageID renamed to Id casing
      repo-wide ("fix every occurrence, not just the new one"); zero
      identifiers ending in ID remain outside UUID. All four modules
      build; pure rename, no SQL or log keys touched.
- [x] **Janitor model.go missing.** Resolved 2026-08-17: sweptRow moved
      out of sweepBatch's body into a new
      pkg/worker/janitor/datastore/model.go, kept unexported (no reader
      outside the package; the convention governs location, and
      RowToStructByName only needs exported fields). Waterline scans into
      scalars — no row structs, so no file created there.
- [x] **db: tags + import-alias drift.** Resolved 2026-08-17: db: tags
      KEPT — user rolled back the deletion; tags are part of the Data
      struct template regardless of scan style, not a per-query contract.
      internal/topic standardized on the iTopic alias everywhere: 13 bare
      imports converted (metrics, producer, compaction, messageconsumer,
      deliveryconsumer, waterline, janitor), matching the 16 files and all
      template packages that already aliased.
- [ ] **db: tags on every *Data struct — decide + sweep.** User leans
      toward requiring db: tags on all datastore row models (the inverse
      of the dead-tag finding). If adopted: write into CONVENTIONS.md and
      sweep the untagged models (worker, cron, consumer group/binding,
      base keylease, janitor sweptRow — audit as part of the sweep).
- [x] **pgtype.UUID in worker public signatures.** Resolved 2026-08-17:
      the four datastore instance publics (+ their privates) now take
      uuid.UUID; toTokenData moved from the controller adapter into the
      datastore, converting at the query-arg boundary.
      WorkerInstanceData.Token stays pgtype.UUID (table-exact scan target,
      same precedent as metrics' pgtype.Timestamptz). worker-claim-lab
      green.
- [x] **Worker vocabulary dead fields.** Resolved 2026-08-17:
      Worker.TargetInstances and WorkerInstance.ExpiresAt deleted (writers
      only, zero readers repo-wide; the metrics snapshot pipeline carries
      its own TargetInstances end to end). WorkerData.SystemId KEPT: the
      no-reader rule governs the vocabulary layer, while row structs are
      bound by table-exact — consistent with the db-tags template call.
- [x] **Misnamed config file.** Resolved 2026-08-17: runner_config.go
      renamed instance_tick_runner_config.go (git mv), matching the
      InstanceTickRunnerConfig it declares.
- [x] **Manager pool cleanups.** Resolved 2026-08-17: newExecutionPool now
      (*executionPool, error) with nil-checks (Run propagates);
      newSpawnedExecution and newWorkerChange constructors replace the bare
      literals — newWorkerChange returns a value (flat slice element) and
      rejects the workerRemoved/nil-worker mispairing, so diff and
      reconcile now return error (refresh propagates, surfacing as a
      failed tick); stop guards the map read with comma-ok + warn;
      receiver `s` on spawnedExecution. worker-claim-lab green.
- [x] **systemmanager Run re-entry.** Resolved 2026-08-17: not a data race
      — the manager row is unbound (NoInstanceTarget), so a double Run
      means two full reconcile loops in one process, not corruption.
      Guarded anyway with the consumer's permit pattern: atomic.Bool
      CompareAndSwap in Run, second concurrent call refused with an error;
      doc comment states one-Run-at-a-time. (The raw ds.Pool read still
      resolves with the migrate chunk.)
- [x] **Permit raised to pkg/concurrency.** Resolved 2026-08-17
      (user-directed follow-on): consumePermit promoted to
      concurrency.Permit — Acquire() (release, ok), callers word their own
      refusal errors. Three sites converted: ConsumerInstance.Consume
      (ErrAlreadyConsuming wording unchanged), SystemManager.Run (same
      shape), BaseExecution.Run (one-shot: release discarded, replacing
      the started atomic.Bool). pkg/consumer/permit.go deleted;
      permit_test.go added; the snapshot/trigger CAS sites
      (rangestate, create_ahead_gate) deliberately untouched — different
      pattern. routing-lab green.
- [ ] **Metricscollector point table.** The 14-row inline anonymous-struct
      table (pkg/worker/metricscollector/execution.go:279-298) gets a named
      type.

### One-decision sweeps

- [ ] **Sentinel ownership + homes.** topic/system sentinels are raised
      only from pkg/admin (admin/system.go:168,183); cron's are raised from
      its own datastore. Pick the rule, write it into CONVENTIONS.md,
      sweep. Same review: rehome base.ErrLeaseLost — the messageconsumer
      and exceptionconsumer datastores import all of pkg/consumer/base for
      it (exceptionconsumer .../datastore/outcome.go:9), the clearest arrow
      violation in the consumer tree — and decide whether pkg/errors' two
      Consume-lifecycle sentinels move to pkg/consumer.
- [ ] **Slice element returns.** topic/controller/datastore/topic.go:77,206
      and cron return []*Data; metrics/compaction return []Data; convention
      says values. Sweep to []Data.
- [ ] **Janitor reads another domain's fact.** cursorFloor
      (janitor/datastore/partition.go:45-56) reads cursor JOIN
      consumer_group outside any transaction, re-deriving the committed
      fact the waterline owns. Decide the sanctioned source.
- [ ] **Metrics topic-id cache.** MetricsDatastore caches metricsTopicId
      (datastore.go:19,45) via resolveMetricsTopicId reading the topic
      table (event.go:61-75) — a fact topic/controller owns, and the only
      mutable cache state on any datastore.
- [ ] **newestInstalled duplicated across the layer boundary.**
      consumer/controller/binding_declaration.go:80-123 re-implements the
      datastore's newestInstalledDeclaration
      (.../datastore/binding_declaration.go:249) over the same rows. One
      derivation.
- [ ] **Cron datastore drift.** Hand-called json.Marshal in replace.go:113
      (rule: any goes straight to pgx); unsuspendCronJob re-derives
      next-scheduled-time inline (cronjob.go:122-142) beside the named
      helper (:184); dbNow/nextScheduledTime placed after a pair that never
      calls them; RegisterCronJobData is not table-exact (model.go:30-37);
      LastScheduledTime is *time.Time here vs pgtype.Timestamptz in metrics
      for the same column — pick one nullable-column shape.
- [ ] **Metrics drift.** Bare "abandoned"/"cleared" strings
      (controller/abandonedroutine.go:33,37) duplicate consumer/metrics
      EventAbandoned/EventCleared; IsCompacted (metrics) vs Compacted
      (compactionreadcost) name the identical query — one verb; the inline
      shaping loop + bare eventKey struct (abandonedroutine.go:15,42-62)
      breaks the file's own adapter pattern.
- [ ] **Consumer declaration enums + re-validation.** The datastore public
      DeclareBindings returns vocabulary binding.DeclarationOutcome while
      its model.go:13-18 defines BindingDeclarationStatus for the same
      column — two enums, one fact; wildcardToRegex re-validates the empty
      pattern the controller already rejects
      (.../datastore/binding_declaration.go:229-232 vs controller :27-29);
      exceptionconsumer's recordAndReleaseKey re-checks a nil the callers
      already branched on (outcome.go:238-240).

### Package restructures

- [ ] **pkg/migrate comb-through** (surveyed 2026-08-16, all 670 lines +
      callers; proposed shape below, not yet reviewed):
      - Version / SystemOwner / IsLocked / AssertSchemaSupported are free
        funcs taking a raw Querier — the banned public-signature shape; the
        topic and worker controllers feed them by reaching through two
        layers of exported fields (c.datastore.Datastore.Pool). Proposal:
        make them Runner methods; those controllers build a Runner from
        their own *PostgresDatastore at construction and hold it.
      - Runner + MigrateDatastore have exported fields, bare
        (ds, retryPolicy, log) params with nil-tolerated logger, no Config
        structs. Proposal: RunnerConfig + MigrateDatastoreConfig with
        WithDefaults/Validate, unexported fields, Wrap-only same-named
        pairs on the datastore publics; internals-only verbs
        (RecordSuccess, TryRecordFailure) unexport.
      - Owner.Name "diagnostics only" exists ONLY because the schema gate
        fabricates NewTopicOwner(systemId, topicId, "") at support.go and
        topic/controller/schema.go — the read never uses the name.
        Proposal: split the gate by what callers know —
        AssertSystemSchemaSupported (ctx, systemId) /
        AssertTopicSchemaSupported (ctx, systemId, topicId), datastore
        reads by id columns — then NewTopicOwner / NewConsumerGroupOwner
        reject an empty name.
      - SystemOwner stays in migrate (as a Runner method): admin imports
        migrate so rehoming is a cycle, and migrate must resolve the system
        before schemas are trustworthy (it owns the 42P01 handling).
      - Checkpoint: invariant-lab + schema-gate-lab, CLI migrate
        status/up/down paths, fresh-DB suite.
- [ ] **alert/controller conformance.** The one controller shaped like no
      sibling: add controller_config.go; NewAlertController(ctx, alerts,
      heads, repeat, log) (controller.go:28) → (deps, cfg) with the
      nil-default logger (:38-40) and retention clamp (:44-50) moved into
      WithDefaults/Validate; RecordOutcome + consts (record.go:14-20) move
      to the pkg/alert vocabulary beside Status/Severity; classify names
      its param *producer.MessageRow through a two-alias chain
      (classify.go:12) — say common.MessageRow. Kind nits ride along: the
      definitions store Config as a pointer on the long-lived struct
      (partitioncount/definition.go:23,109; same in compactionreadcost) —
      value copy; the alert-name consts live in the kind controllers
      (controller.go:11) and get imported back up by job.go — consider the
      kind root.
- [ ] **compaction vocabulary layer.** The only two-layer domain: no
      pkg/compaction package exists; the read-model is common.MessageRow.
      Decide deliberate exception (write it down) vs add the layer.
      Ride-along: errors.New validation messages drop the offending value
      (controller/head.go:14,31, message.go:15) where every sibling uses
      fmt.Errorf with the got-value.
- [ ] **Vocabulary importing controllers.** cron/jobrequest.go:11,22,
      metrics/topic.go:8,14, and alert/topic.go:8,14 declare
      TopicConfig() *topiccontroller.TopicConfig; alert/job.go:7,15 stores
      *croncontroller.CronJobConfig in a vocabulary struct; alert/
      adapter.go:8 exports ToJobData from the vocabulary. One design
      question: where do a domain's system-topic and cron-job declarations
      live so arrows stay downward (topic and system manage without).
- [ ] **Consumer read-model homes.** The row-stack vocabularies hold runner
      machinery, so read-models sit in controllers: Message / ClaimedRange
      / RangeLease / MessageOutcome / OutcomeKind in
      messageconsumer/controller/cursor.go:16-71 (a file named for none of
      them); ClaimedException (exceptionconsumer/controller/
      exception.go:19); Delivery plus its untyped Status string
      (deliveryconsumer/controller/delivery.go:20-28, datastore
      model.go:20 — every other status column has a typed enum); Group
      (consumer/controller/group.go:13) while sibling Declaration lives in
      pkg/consumer/binding; KeyLeaseVerdict / KeyLeaseClaim
      (base/controller/keylease.go:13-28). One shape decision, then
      per-stack moves.
- [ ] **consumer/metrics restructure.** Not vocabulary — it owns a
      goroutine and a producer.Producer. metrics_config.go →
      metric_event_config.go with WithDefaults/Validate added (the only
      config in the tree missing both); NewMetricEventProducer nil-checks
      ds and defaults/validates cfg (producer.go:23-41); delete the
      nil-safe receiver (producer.go:71-74), the reader-less Noop field
      (metrics_config.go:9), the unused ctx params (producer.go:63,67);
      decide the sideways pkg/consumer/metrics → pkg/producer arrow (base
      transitively depends on all of producer through it).
- [ ] **consumer/base cleanup.** NewBaseConsumer does a DB read with ctx
      first (base/consumer.go:33); loose timeoutGrace/ackMargin params →
      slim config (deliveryconsumer/provision.go:34 passes a bare 0 with a
      comment — the tell); NewBaseDefinition's 6 positional params with
      trailing retryPolicy+log → (dep, cfg) (definition.go:33); exported
      KeyLeases field lets rows release directly
      (messageconsumer/consumer_runner.go:275) while claiming through the
      base's own verb (:236) — make claim/release symmetric on
      BaseConsumer; drop the inference-noise type param on NewBaseExecution
      (execution.go:23). Ride-along: sanction-or-reshape ClaimKeyLease's
      documented non-Wrap shape (token minted before the Wrap so it
      survives retries, base/controller/datastore/keylease.go:18-32).
- [ ] **Worker kind controller decision → janitor/waterline.** The worker
      kinds are pkg/worker/<kind>/datastore with no controller, so no
      layer owns validation (janitor's DropExpiredPartitions /
      SweepExpiredPartitions take 6 unvalidated params each, fed from
      execution.go:68,71; waterline publics likewise); the alert kinds
      carry controller/datastore and are the template. Decide
      grow-controllers vs sanction datastore-only in CONVENTIONS.md, then
      restructure janitor + waterline to match (cronscheduler rides the
      next chunk). Waterline ride-alongs: AdvanceWaterline's discarded
      return (datastore/waterline.go:23, execution.go:68); the deliberate
      two-round-trip non-transactional advance (waterline.go:50,63) —
      reconfirm and state it, or make it one transaction.
- [ ] **Produce-transaction seam.** One design, two packages; overlaps
      ROADMAP's Querier-interface item — settle the two together.
      Producer datastore: pgx.Tx in the publics AppendMessageInTx
      (append.go:88) and GetCompactionHeadInTx (compaction.go:15); the Tx
      interface's Raw() pgx.Tx re-exports the driver to the door
      (transaction.go:22, producer_instance.go:153); NewTx returns a
      concrete pointer through an interface-typed return
      (transaction.go:29); AppendMessageBatch does heal + gate + trigger
      in the public with a nested Wrap (batch.go:16-36) and returns three
      values; controller InTransaction opens the transaction itself
      (controller/transaction.go:24-36). Cronscheduler: exported
      tx-taking datastore methods with producer.Tx in their signatures
      (datastore/cronjob.go:49,76,88); the transaction is opened in the
      worker layer via producer.InTransaction (execution.go:102); those
      publics are bare pass-throughs, not Wraps; unchecked
      common.ConcurrencyPolicy cast (execution.go:134) because model.go
      types the column string.
- [ ] **Unexport controller/datastore fields.** Exported Datastore /
      DatastoreRetry / Logger on all 18 datastores plus exported Logger /
      Config on controllers, instances, and executions repo-wide
      (alert/controller is the lone unexported-logger case; Consumer /
      ConsumerInstance align with producer's unexported shape here). One
      repo-wide sweep, deliberately LAST: the migrate chunk removes the
      c.datastore.Datastore.Pool schema-gate reach-through
      (topic/controller/schema.go:22, worker/controller/schema.go:17,
      systemmanager.go:113) and the seam chunk removes cronscheduler's
      i.datastore.Datastore grab.
- [ ] **pkg/context + pkg/logger → pkg/common.** Probably move until the
      final public surface is settled (carried from the original roadmap
      sub-bullet).
