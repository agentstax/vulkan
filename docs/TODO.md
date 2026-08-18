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
- [x] **db: tags on every *Data struct.** Resolved 2026-08-17: rule
      settled from the already-tagged files' implicit pattern — every
      scan-destination row struct tags each field with its query's column
      or alias; write shapes (AppendData, RegisterCronJobData,
      OutcomeData), derived outcomes (AppendedData, both KeyLeaseData),
      and composites (ClaimedRangeData) stay untagged. 16 structs tagged
      across topic, system, metrics, cron, worker, cronscheduler,
      consumer; rule written into CONVENTIONS.md (Datastores). SQL
      untouched — unaliased expression outputs tag by their column
      concept.
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
- [x] **Metricscollector point table.** CLOSED 2026-08-17 without change:
      the gaugePoint named type was applied then rolled back by the user —
      the inline anonymous-struct table stays as is; not needed.

### One-decision sweeps

- [x] **Sentinel ownership + homes.** Resolved 2026-08-17. Rule (now in
      CONVENTIONS.md, Package layout): sentinels are declared in the
      owning domain's vocabulary, cross-stack ones in pkg/errors;
      whichever layer detects the condition raises it — so the
      admin-vs-datastore "asymmetry" was two valid cases of one rule, no
      sweep needed. ErrLeaseLost moved base → pkg/errors (its stated
      purpose; leaf import), freeing the messageconsumer/exceptionconsumer
      datastores and reclaimlab from importing runtime-heavy base;
      pkg/errors' two sentinels stay put — that question closes as "no".
      base/errors.go deleted; stale retry comment fixed. reclaim-lab
      green.
- [x] **Slice element returns.** topic/controller/datastore/topic.go:77,206
      and cron return []*Data; metrics/compaction return []Data; convention
      says values. Sweep to []Data.
    - Resolved 2026-08-17. Datastore list returns swept to value slices:
      ListTopics/RenameTopic → []TopicData, ListCronJobs → []CronJobData,
      CronJobStatus → []GroupStatusData, CronJobRequests →
      []JobRequestStatusData, plus cron's private matchingGroups/jobMessages
      and the groupStatus/groupJobRequestStatuses helpers (value params +
      value returns). Adapters keep *Data params (shared with single-row
      Gets); controller loops pass &listed[i]. Vocabulary returns
      ([]*topic.Topic etc.) unchanged — read-models stay
      pointer-classified. Full-repo []*T sweep also caught consumer
      controller openWaiters → []BindingDeclarationData (was a pointer
      filter over a value slice). Left alone: batcher queue /
      claimbuffer rangeState (shared mutable state, correctly pointers)
      and the producer append chain (ProduceItem → Append → AppendData
      pointer slices — public produce API shape, deferred to the v1 API
      review). cron-lab + reserved-topic-lab + binding-lab green.
- [x] **Janitor reads another domain's fact.** cursorFloor
      (janitor/datastore/partition.go:45-56) reads cursor JOIN
      consumer_group outside any transaction, re-deriving the committed
      fact the waterline owns. Decide the sanctioned source.
    - Resolved 2026-08-17, user picked read-inside-each-txn. cursorFloor
      isn't a re-derivation (it aggregates the stored cursor.committed the
      waterline maintains) — the smell was the outside-tx read reused
      across many later transactions. cursorFloor now takes
      datastore.Querier (getTopic pattern) and runs on the deleting
      transaction: sweepBatch reads it at tx start, dropPartition reads
      it inside its tx and returns (dropped bool, err) — false when the
      floor still protects the partition. The standalone pre-reads in
      sweepExpiredPartitions/dropExpiredPartitions are gone;
      allowDropPastCommitted skips the read entirely. drop-floor-lab +
      sweep-lab + compaction-head-retention-lab green.
- [x] **Metrics topic-id cache.** MetricsDatastore caches metricsTopicId
      (datastore.go:19,45) via resolveMetricsTopicId reading the topic
      table (event.go:61-75) — a fact topic/controller owns, and the only
      mutable cache state on any datastore.
    - Resolved 2026-08-17, user picked resolve-per-call. Cache field +
      atomic deleted; resolveMetricsTopicId is a plain per-call read
      pinned to schema_version = 1 (was ORDER BY schema_version DESC — a
      divergent second derivation). Reading the topic table stays in the
      metrics datastore: its charter is deriving snapshots from rows
      other domains maintain. The cache was also a live hazard — a
      destroy + re-register left it pointing at a dropped message_log
      table. Threading the id down callers was rejected (leaks
      metricsTopicId into snapshot public signatures).
      abandoned-routine-snapshot-lab green.
- [x] **newestInstalled duplicated across the layer boundary.**
      consumer/controller/binding_declaration.go:80-123 re-implements the
      datastore's newestInstalledDeclaration
      (.../datastore/binding_declaration.go:249) over the same rows. One
      derivation.
    - Resolved 2026-08-17. Datastore's copy exported as
      NewestInstalledDeclaration (the datastore owns the rows and uses it
      inside declareBindings' transaction); controller's newestInstalled
      deleted, ListDeclarations calls the exported one. binding-lab
      green.
- [x] **Cron datastore drift.** Hand-called json.Marshal in replace.go:113
      (rule: any goes straight to pgx); unsuspendCronJob re-derives
      next-scheduled-time inline (cronjob.go:122-142) beside the named
      helper (:184); dbNow/nextScheduledTime placed after a pair that never
      calls them; RegisterCronJobData is not table-exact (model.go:30-37);
      LastScheduledTime is *time.Time here vs pgtype.Timestamptz in metrics
      for the same column — pick one nullable-column shape.
    - Resolved 2026-08-17. replace.go: marshalJson + jsonEqual deleted —
      the marshal fed a Go-side mirror of jsonb's `=`; the UPDATE now
      takes declared.Data/Metadata straight to pgx with SQL-side COALESCE
      (matching the INSERT) and configChanges compares found against the
      RETURNING row, both jsonb-normalized. Behavior note: a no-op
      re-registration now performs an idempotent UPDATE instead of
      returning early; both log messages preserved. unsuspendCronJob uses
      the nextScheduledTime helper (its own error text folds into a
      "stays suspended" wrap); dbNow/nextScheduledTime moved to follow
      the Unsuspend pair that calls them. RegisterCronJobData closed as
      no-change: it's a declaration write shape, and write shapes are
      untagged/not table-exact per the settled db-tag rule. Metrics
      CronJobSnapshotData.LastScheduledTime pgtype.Timestamptz →
      *time.Time (the codebase's majority nullable-timestamp shape).
      cron-lab + metrics-collector-lab green.
- [x] **Metrics drift.** Bare "abandoned"/"cleared" strings
      (controller/abandonedroutine.go:33,37) duplicate consumer/metrics
      EventAbandoned/EventCleared; IsCompacted (metrics) vs Compacted
      (compactionreadcost) name the identical query — one verb; the inline
      shaping loop + bare eventKey struct (abandonedroutine.go:15,42-62)
      breaks the file's own adapter pattern.
    - Resolved 2026-08-17. EventType + EventAbandoned/EventCleared moved
      consumer/metrics → pkg/metrics routing.go, beside AbandonedRoutineKey
      whose comment already states the shared-wire-contract principle
      (consumer/metrics imports pkg/metrics already; the reverse would be
      a new cross-stack arrow). GoRoutineEvent/EventTimestamps now carry
      metrics.EventType end to end. compactionreadcost Compacted →
      IsCompacted (matches IsEmpty/IsCompacted verb; duplication of the
      query itself is the sanctioned each-side-writes-its-own shape).
      Shaping loop + eventKey moved to adapter.go as
      toAbandonedRoutineSnapshot. abandoned-events-lab +
      abandoned-routine-snapshot-lab + alert-lab green.
- [x] **Consumer declaration enums + re-validation.** The datastore public
      DeclareBindings returns vocabulary binding.DeclarationOutcome while
      its model.go:13-18 defines BindingDeclarationStatus for the same
      column — two enums, one fact; wildcardToRegex re-validates the empty
      pattern the controller already rejects
      (.../datastore/binding_declaration.go:229-232 vs controller :27-29);
      exceptionconsumer's recordAndReleaseKey re-checks a nil the callers
      already branched on (outcome.go:238-240).
    - Resolved 2026-08-17. Enum half closed as no-change: they are two
      facts, not one — DeclarationOutcome is the attempt-level result of
      the sanctioned classify function and includes joined, which never
      lands in a row; BindingDeclarationStatus is the column's value set
      traveling with the datastore model. wildcardToRegex's empty-pattern
      error deleted (controller already rejects "": datastores trust
      inputs) — it now returns just the string. recordAndReleaseKey's
      nil guard deleted: all three callers branch on keyClaim == nil
      first. binding-lab + exception-lab + defer-lab green.

### Package restructures

- [x] **pkg/migrate comb-through** (surveyed 2026-08-16; shape approved
      2026-08-17, landed in two chunks; decision record [0526]):
    - Three-layer layout done 2026-08-17 (user-requested; record [0527]):
      pkg/migrate is vocabulary only (Migration + Validate, ErrNotRegistered
      rehomed from the datastore, Min/Max version consts); Controller +
      configs + gates + reads moved to pkg/migrate/controller, datastore to
      pkg/migrate/controller/datastore; Migration.ToStep became the
      controller-side toStep adapter; the ErrNotRegistered re-export var
      deleted. Importers re-pathed (topic/worker controllers, admin,
      systemmanager, CLI, invariantlab, schemagatelab) — errors.Is /
      registry users keep importing pkg/migrate. Verified: build+vet all
      modules, migrate+registry tests, invariant-lab, schema-gate-lab,
      `vulkan migrate status` against dev DB.
    - Follow-up done 2026-08-17 (user-requested consistency pass): the
      whole pool-read surface is by id — Controller.SystemVersion(ctx,
      systemId) + TopicVersion(ctx, topicId); the owner-taking
      Version(ctx, owner) controller method and datastore pair deleted
      (owner form survives only as the conn-taking free func for the
      locked run flow, which is owner-generic end to end). No
      GroupVersion — nothing writes group rows to migration_log yet. CLI
      status/gatherTargets read by id; status's topic loop no longer
      builds owners. Verified: build+vet both modules, migrate/common
      tests, schema-gate-lab, invariant-lab, `vulkan migrate status`
      against dev DB.
    - Chunk B done 2026-08-17: gate split into
      AssertSystemSchemaSupported(ctx, systemId) /
      AssertTopicSchemaSupported(ctx, systemId, topicId) on
      migrate.Controller; new datastore TopicVersion(ctx, topicId) reads
      by id (migration_log stores one owner column per row, so the topic
      row is system_id IS NULL AND topic_id = $1); the shared
      42P01/no-rows → ErrNotRegistered mapping deduplicated into
      registrationError. Both NewTopicOwner("") fabrication sites gone →
      NewTopicOwner/NewConsumerGroupOwner reject empty names,
      Owner.Name's "diagnostics only" caveat deleted. Worker gate
      branches on owner.Kind(); topic gate passes ids through.
      Checkpoint: 41/41 labs on a drop+recreate fresh DB, CLI migrate
      status + up no-op path, migrate/common tests green.
    - Chunk A done 2026-08-17: Version/SystemOwner/IsLocked/
      AssertSchemaSupported are methods on migrate.Controller — the
      Runner→Controller rename is user-settled: Controller is the house
      word for a domain's door, and Runner stays reserved for run-loops
      (manager.Runner, InstanceRunner). NewController(ds,
      *ControllerConfig) + MigrateDatastoreConfig with
      WithDefaults/Validate; datastore field unexported; Retry field →
      DatastoreRetry; RecordSuccess → recordSuccess (TryRecordFailure
      stays public — the controller calls it); datastore gained Wrap-only
      pool-read pairs, conn-under-lock free funcs Version/SystemOwner
      stay for the locked flow. Callers rebuilt: topic/worker controllers
      + systemmanager hold a migrateController; admin passes
      ControllerConfig; CLI builds a Controller in
      migrate_status/migrate_scopes (gatherTargets takes
      *migrate.Controller); invariantlab + schemagatelab updated. migrate
      tests + invariant-lab + schema-gate-lab + `vulkan migrate status`
      against dev DB green.
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
- [x] **alert/controller conformance.** Done 2026-08-17: controller_config.go
      added (ControllerConfig{Logger} with WithDefaults/Validate);
      NewAlertController(ctx, alerts, heads, repeat, cfg) — the nil-default
      logger moved into WithDefaults; the retention clamp stays in the
      constructor body (user-settled: it crosses the required repeat param
      with alerts.Topic.RetentionTTL and warns, so neither WithDefaults nor
      Validate can see both sides; ctx stays solely for the clamp's
      WarnContext). RecordOutcome + consts moved to pkg/alert beside
      Status/Severity (ripples: both kind executions, alertlab). classify /
      statusChanged say common.MessageRow directly. DefinitionConfig renamed
      to the kind-named PartitionCountConfig / CompactionReadCostConfig in
      partitioncount_config.go / compactionreadcost_config.go (janitor
      template: JanitorConfig in janitor_config.go); Config stays a
      *<Kind>Config pointer field like every sibling (a value-copy attempt
      was rolled back — the codebase-wide pattern is the pointer, though
      CONVENTIONS' "long-lived instance stores a value copy" line reads
      otherwise; flagged, not resolved). Alert-name consts stay in the kind
      controllers
      (user-settled: moving them to the kind root cycles — the controller's
      own alert builder is the deepest user, and the kind root already
      imports its controller; don't re-suggest). Verified: build+vet, alert
      lab green.
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
- [x] **pkg/context + pkg/logger → pkg/common.** Done 2026-08-17, expanded
      to the full flat merge (user-settled; record [0528]): logger, retry,
      errors, context all merged into a flat pkg/common — renames
      retry.Policy → common.RetryPolicy, DatastoreRetry →
      common.RetryDatastore (+NewRetryDatastore), logger.With →
      common.LoggerWith; files named for their declarations
      (retry_policy.go, retry_datastore.go, lifecycle.go, ...);
      messageoptions.go → message_options.go with ConcurrencyPolicy split
      to concurrency_policy.go; concurrency EXCLUDED (destined internal/
      per the public-surface trim — don't fold it into common). 140 caller
      files re-pathed across all three modules (labs importing
      examples/phase_1/common alias the pkg as vulkancommon); the
      vulkanerrors/vulkanctx aliases are gone; CONVENTIONS.md wording
      updated (common.Logger, sentinels shared across stacks live in
      pkg/common). Verified: build+vet+tests all modules, fresh-DB full
      lab suite at checkpoint.
