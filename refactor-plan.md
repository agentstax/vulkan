okay we need to do some refactoring of our maintenance / duty system to make things more generic.

# entity table — SETTLED DESIGN (2026-07-28)

Entities are a first-class INTERNAL resource: a stable global id + the FK root for
lifecycle cascade. Users never see them.

Rule: CONTROL-PLANE rows enroll (topic, consumer_group); DATA-PLANE rows never do
(messages / deliveries / exceptions — enrolling them would add DELETE overhead and
vacuum churn; revisit only with explicit partition drops).

CREATE TABLE IF NOT EXISTS entity (
  id   BIGSERIAL PRIMARY KEY,
  type TEXT NOT NULL   -- 'topic' | 'consumer_group' | ... (classify+switch in code)
);

- NO name column — admin.Rename exists; a second copy of name drifts. Display resolution
  = switch on type + join to the owning table (internal-only, house classify+switch).
- entity is the LIFECYCLE ROOT: every enrolled table carries
    entity_id BIGINT NOT NULL UNIQUE REFERENCES entity(id) ON DELETE CASCADE
  admin.Destroy deletes the entity row and everything downstream goes transactionally
  (resource row, its cron_jobs, its alerts, ...).
- CASCADE is implicit deletion and is acceptable ONLY because pkg/admin is the sole
  lifecycle write path and Destroy is double-guarded. Do not add a second delete path.
- Each package's Register owns its entity insert (same txn) — enrollment is not a
  separate step anyone can forget.
- Enroll NOW: topic, consumer_group (cron_jobs need them as owners). Nothing else until
  a feature actually references it. cron_job itself is NOT enrolled (nothing targets
  cron_jobs yet; enroll later if e.g. alerts ever reference them).
- Alerts are the obvious second customer (typed identity in Data -> owner/target entity
  refs) but that is their refactor, not this one.

---

# cron_job / job scheduler — SETTLED DESIGN (2026-07-28)

CREATE TABLE IF NOT EXISTS cron_job (
  id                  BIGSERIAL PRIMARY KEY,
  owner_entity_id     BIGINT REFERENCES entity(id) ON DELETE CASCADE, -- whose lifecycle I'm bound to
  name                TEXT NOT NULL UNIQUE,         -- user identifier
  handler             TEXT NOT NULL,                -- valid 'runner' (like image for k8s cronjobs)
  schedule            TEXT NOT NULL,                -- compliant '* * * * *' cron schedule
  concurrency         TEXT NOT NULL DEFAULT 'allow' -- 'allow' | 'forbid' | 'defer' (later)
  timeout             INTERVAL NOT NULL,            -- job spec (k8s activeDeadlineSeconds
                                                    -- analogue); scheduler stamps it into
                                                    -- MessageOptions.WorkTimeout
  suspended           BOOLEAN NOT NULL DEFAULT false,
  data                JSONB NOT NULL DEFAULT '{}', -- OPAQUE handler payload incl. the actual
                                                   -- target; decoded only by handlers, never
                                                   -- resource refs the system must act on
  metadata            JSONB NOT NULL DEFAULT '{}'  -- labels / annotations / etc

  next_scheduled_time TIMESTAMPTZ NOT NULL,
);

CREATE INDEX ON cron_job (next_scheduled_time) WHERE NOT suspended;  -- the poll predicate

-- REJECTED: cron_job's own entity enrollment (nothing references cron_jobs yet);
-- target refs inside data JSONB as the GC mechanism (opaque blobs are
-- un-garbage-collectable — admin.Destroy can't find dependents it can't decode)

Registration-time validation (SETTLED — no handler registry needed):
- schedule parses; minimum firing gap >= work_timeout. Self-contained: work_timeout is the
  job's own spec, stamped into MessageOptions.WorkTimeout at produce — a producer concern,
  not a consumer concern.
- This check is a SANITY check, not a correctness guarantee — overlap correctness is
  enforced at dispatch by key_lease. It only prevents configs where every slot gets
  skipped. Consumer-side Grace/AckMargin are epsilon and ignored; a consumer MessageMax
  clamping the request down only makes the check more conservative. Both errors land safe.

Suspend semantics:
- scheduler predicate: ... AND NOT suspended
- unsuspend admin op RECOMPUTES next_scheduled_time from now() — resume on schedule, no
  surprise immediate fire from the stale past-due timestamp (k8s spec.suspend semantics)

## job scheduler
- 1 min poll loop (anything needing sub-minute frequency stays a long-lived worker)
- Single transaction per tick:
  - SELECT ... WHERE next_scheduled_time <= now() FOR UPDATE SKIP LOCKED
  - ProduceInTx to job_request topic (produce + advance commit atomically)
  - UPDATE next_scheduled_time computed FROM now() -- missed runs are dropped, fire once
- Scheduler produces EVERY due slot unconditionally — concurrency policy is enforced at
  consume time (see exclusive consumption below). No last_message_id / resolved-check on
  cron_job (rejected: dual enforcement layers).

## job_request topic
- compacted; compaction key = cron_job identity (bounds topic at ~1 row per job)
- routing_key: cronjob.<handler>.<name>  (handler first -> bindings are cronjob.<handler>.*)
- idempotency key: deterministic v7-layout UUID — scheduled_time in the 48 timestamp bits,
  hash(cron_job.id) in the rest (preserves index locality). Manual "run now" uses a RANDOM
  key so it doesn't dedupe against the slot.
- Message { data, metadata } — stamped with the slot time it represents
- handler consumes with existing retry machinery; handlers must respect ctx cancellation
  (WorkTimeout); overlap guarantee is only as strong as ctx-respect (abandoned goroutines
  can't be killed — same caveat k8s escapes only by SIGKILLing pods)

## exclusive consumption (concurrency policy — the generic primitive)

Per-MESSAGE, set at produce time via MessageOptions (see message options section):

  MessageOptions.Exclusive: None | Skip | Defer   (persisted with the message)

- Skip  = k8s Forbid: key busy -> slot is dropped, never runs
- Defer = reconciler/workqueue semantic: key busy -> park in exception window, run latest when free
- rule: all messages sharing a compaction key MUST carry the same policy (upheld naturally:
  one scheduler produces all messages for a cron_job)
- exclusive produce onto a non-compacted topic errors at produce time

Dispatch predicate (base.go, before consumerFunc) — ORDER IS LOAD-BEARING, supersede
check MUST run before the lease attempt or a hot key floods the exception window:

  if CompactionHead(msg.key) > msg.rank:  resolveSuperseded(msg)      # stale slot
  else if !acquireKeyLease(msg):
      Skip:  resolveSuperseded(msg)                                   # slot dropped
      Defer: resolveDeferred(msg)                                     # exception window
  else:
      runHandler(msg); recordOutcome + releaseKeyLease                # ONE transaction

Invariant: a message runs iff it is the compaction head at the moment it wins the key lease.

key_lease table (tiny, hot; NOT partitioned, NOT append-only — locks are irreducibly
mutable state; append-only acquire/release event log rejected, permanent rows rejected):

  CREATE TABLE key_lease (
    topic_id       BIGINT NOT NULL,
    consumer_group TEXT NOT NULL,
    compaction_key TEXT NOT NULL,
    lease_token    UUID NOT NULL,        -- PER-ATTEMPT fence (reuse claim lease token identity)
    expires_at     TIMESTAMPTZ NOT NULL, -- resolved WorkTimeout + Grace + AckMargin
    PRIMARY KEY (topic_id, consumer_group, compaction_key)
  );

  -- acquire (steal-on-expiry built in; rowcount 0 = busy):
  INSERT ... ON CONFLICT (pk) DO UPDATE SET lease_token=..., expires_at=...
    WHERE key_lease.expires_at < now();
  -- release (same txn as RecordSuccess/RecordFailure):
  DELETE FROM key_lease WHERE ... AND lease_token = $mytoken;

- fence by lease_token, NOT message_id: same message re-attempted after a steal would let
  the stale holder free the new attempt's lease -> concurrent runs under Forbid (real bug)
- per-group IgnoreExclusive bool on consumer Config (zero value = honor, which is the safe
  default for executors) — observer/audit groups set true so an executor's policy can't
  silently drop their messages. (Renamed from HonorExclusive: sparse-struct zero value must
  be the safe case.)
- Defer bookkeeping: 'deferred' exception kind, distinct from failure — own short/fixed
  retry cadence, does NOT consume the message's resolved Retry.MaxRetries attempts. No
  defer-TTL needed: a crash-looping holder burns its own attempts and frees the key; newer
  slots supersede parked ones (torture lab must prove the wedged-key case)
- superseded terminals ALWAYS recorded in delivery log (debugging Forbid = "why didn't
  12:05 run?"); cron status derives last-run/last-skipped from the log, no new columns

new vocabulary (one concept one name): terminal kind 'superseded', exception kind 'deferred'

---

# message options & config layering — SETTLED DESIGN (2026-07-28)

Resolution model, one sentence: consumer bounds > message > consumer defaults > system
defaults. Messages REQUEST, consumers PROTECT THEMSELVES, defaults FILL SILENCE.

## retry.Policy consolidation (prerequisite sweep)
- ONE construct for message redelivery: *retry.Policy {MaxRetries, BaseDelay, MaxDelay,
  Exponent}. The count lives INSIDE the policy — delete the root consumer.Config.MaxAttempts
  field; Retry.MaxRetries is now actually read (today it's dead weight in the Backoff role).
- The infra-retry family (consumer.Config.Retry, producer.Config.Retry, datastore Retry,
  metrics Retry — transient-error loops for the process's own Postgres calls via retry.Wrap)
  NEVER enters the message-options system. Consumer-only mechanics, untouched.
- consumer.Config.Backoff / datastore MessageRetry are the delivery-curve role — they fold
  into Config.Message.Retry below.

## the shape

  type MessageOptions struct {        // sparse; zero value = "consumer decides"
      Exclusive   ExclusivePolicy     // None | Skip | Defer
      WorkTimeout time.Duration
      Retry       *retry.Policy      // nil = consumer default; field-wise merge
  }

  producer side:
    ProduceOptions{ IdempotencyKey, RoutingKey, Message: MessageOptions{...} }
    producer.Config.Message MessageOptions   // per-topic defaults, merged under every produce

  consumer side:
    type Config struct {
        // root = consumer-only mechanics: QueueMargin, AckMargin, WorkTimeoutGrace,
        // ShutdownTimeout, Retry (infra), ... — knobs a message could never own
        Message         MessageOptions  // defaults  — fill what the message left unset
        MessageMin      MessageOptions  // floors    — consumer wins upward
        MessageMax      MessageOptions  // ceilings  — consumer wins downward
        IgnoreExclusive bool            // veto — not a bound, deliberately not in the trio
    }

  resolution (field-wise):
    resolved = clamp( fill(msg, cfg.Message), cfg.MessageMin, cfg.MessageMax )

## why this shape
- Override fields are the SAME struct as the message options -> names can never drift;
  a new MessageOptions field is automatically defaultable + boundable, compiler-enforced.
  Clamp DIRECTION is structural (which trio field you set), not named (no MaxWorkTimeout /
  MinBackoff names).
- Direction follows the consumer's risk: WorkTimeout bounds via MessageMax (can't demand
  more runtime than the group sized leases for); Retry.BaseDelay bounds via MessageMin
  (threat = 1ms curve hot-looping the group); Retry.MaxRetries via MessageMax.
- Exclusive is an enum, not ordered: Validate() REJECTS it in MessageMin/MessageMax; the
  veto is the flat IgnoreExclusive bool (a veto is not a bound — don't force the shape).
- Zero values: MessageMin/MessageMax zero = unconstrained; Message zero = system defaults
  via WithDefaults(); empty Config behaves like today.
- Rejected: consumer-supplied resolver hook (policy-as-code: can't introspect, can't show
  in status, can't validate at Register). Rejected: separate Overrides struct with
  Max*/Min*-named fields (names drift from the fields they override).

example:
  consumer.Config{
      Message:    MessageOptions{Retry: &retry.Policy{MaxRetries: 3, BaseDelay: time.Second}},
      MessageMin: MessageOptions{Retry: &retry.Policy{BaseDelay: time.Second}},
      MessageMax: MessageOptions{WorkTimeout: 8*time.Minute, Retry: &retry.Policy{MaxRetries: 5}},
  }
  message:  WorkTimeout=10m, Retry={MaxRetries: 8, BaseDelay: 10ms}
  resolved: WorkTimeout=8m (clamped), MaxRetries=5 (capped), BaseDelay=1s (floored),
            MaxDelay/Exponent from Message default

## discipline rule
An option joins MessageOptions only when a real producer needs per-message variance, and
joins the bounds only when a real consumer needs to distrust messages. The root/Message
boundary IS the consumer-only/shared classification, made structural.

## migration cost (budget for it)
Root Config.WorkTimeout, Config.Backoff, Config.MaxAttempts move into Config.Message.* —
touches every existing consumer construction site. Rename sweep, not pure addition.
Grep labs for hand-copied config/queries while sweeping (labs go silently stale).

---

# worker system — CHUNK PLAN (2026-08-02)

Generic worker system in a NEW pkg/worker. Do NOT retrofit pkg/maintain — duplicate
its code into pkg/worker where it fits; maintain stays untouched until cleanup.
Worker = first-class user resource; janitor/waterline/cron_scheduler are just the built-in
workers we ship.

Model (settled in discussion):
- worker row = what should run: name, owner, metadata (optional), target_instances
  (2026-08-02: replaced min/max_instances — the claim gate reads ONE number, target;
  0 = suspended. min/max are rails on whoever MUTATES target and return with the
  alter surface / autoscaler, which don't exist yet)
- worker_instance row = who's alive: id, worker_id, token, expires_at (heartbeat-renewed)
- exclusivity moves claim-per-tick -> claim-per-instance: Register claims an instance
  slot, heartbeat holds it, tick pacing is worker-internal (no per-tick claim race).
  Slot claim MUST be one atomic statement (live-count + insert in one snapshot —
  same race class as AdvanceWaterline).
- factory pattern, codebase-wide invariant:
  New* = pure factory, never touches the DB; Register = the only build step, callable
  many times, RETURNS the product (an instance) instead of mutating the factory;
  callers operate on what Register returned. Register outcomes: instance claimed /
  declined (slots full — not an error, manager retries next reconcile) / error.
- WorkerManager holds worker Provisioners; spawn = Provision per discovered worker
  row + Run the returned Execution.

Package layout (settled 2026-08-02) — vocabulary at the bottom, every arrow points down:
- pkg/worker                       vocabulary only: Worker, WorkerInstance, worker names,
                                   Definition/Declarer/Provisioner/Execution,
                                   ErrInstanceLost. No DB, no config.
- pkg/worker/controller            WorkerController + config + adapters; the only door to
                                   worker persistence.
- pkg/worker/controller/datastore  WorkerData + SQL (not internal/ for now).
- pkg/worker/manager               WorkerManager + runner pool; NewWorkerManager takes the
                                   core postgres datastore and constructs its own controller
                                   (same shape as the seeders' datastores).
- pkg/worker/{janitor,waterline,cronscheduler} (chunks 5-7) import worker + controller.

## chunks
1. tables — worker + worker_instance DDL in the pkg/system baseline (edit in place).
   Decide here: owner shape (explicit system_id/topic_id/consumer_group_id columns
   mirroring maintenance vs owner_entity_id per the entity design above); where the
   observable failure streak lives (today maintenance.attempts feeds DutySnapshots —
   it must not silently vanish).
2. NewWorker — pkg/worker: Worker/WorkerMetadata models + constructors, datastore
   (list workers, get metadata, instance claim/renew/release, expired-instance reap).
3. seeding — register paths write worker rows (topic register -> janitor, group
   register -> waterline, RegisterSystem -> cron scheduler), ALONGSIDE the existing
   maintenance seeds until cleanup.
4. NewWorkerManager — reconcile loop adapted from fleet.go/dutypool.go; accepts
   worker factories; spawns/destroys instances; reaps expired worker_instances.
5. Janitor worker — sweep logic duplicated from maintain. EnsureNextPartition does
   NOT come along (create-ahead is provisioning, not sweeping) — decide its new home
   here (producer self-heal already covers misses).
6. Waterline worker.
7. CronScheduler worker — tick/produce logic duplicated from maintain/scheduler.go.
8. consumer: replace maintainer — consumer registers/runs janitor + waterline workers
   instead of maintain duties.
9. consumer factory-style register — SETTLED DESIGN (2026-08-03), supersedes the one-line
   plan below. Ownership INVERTED: the manager is lifted out of consumers entirely; the
   public Consumer is the higher-order construct that runs manager.Runner, and the
   consumption loops are worker rows the manager spawns like any other.
   - per-type worker rows per group: message_consumer / exception_consumer /
     delivery_consumer, seeded at target_instances = -1 (NoInstanceTarget). Per-type rows
     make a pure retry consumer visible/alertable and give a per-type toggle: 0 suspends
     new claims. DEFERRED: pool destroying already-running instances on 0 — for now the
     toggle takes effect on next claim, not next reconcile.
   - shape C: NewConsumer validates cfg and builds nothing stateful; Register(ctx) is the
     build step — resolve topic/group, seed rows, construct THIS instance's consumer
     factories + upkeep factories + manager factory + runner, return the instance;
     instance.Consume(ctx, fn) sets the fn slot (http.Server pattern — before the runner
     starts, no race) then blocks in runner.Run. Register-many-times = independent lives;
     wound-down-stays-down dies (re-Register instead of re-New). fn slots are
     per-instance because factories are built per-Register.
   - pool error rule: ErrInstanceLost -> respawn (the healing); any other error from a
     spawned instance's Run -> propagate, tear down the manager, surface out of Consume.
     Upkeep workers' Run only exits on loss/cancel, so only consumer-type instances can
     produce a fatal error in practice.
   - manager's first reconcile pass runs immediately — a fresh consumer must not idle out
     a jittered first tick before its loops spawn.
   - InstanceTickRunner splits: claim-hold half (heartbeat + release, no pacing) reusable
     by continuous loops like consumers; tick-pacing half composes it.
   - MessageConsumer/ExceptionConsumer/DeliveryConsumer become pure worker factories with
     a standalone self-claim door (rows are -1, self-claim never declines). NO manager,
     NO upkeep in any sub-consumer — a pure ExceptionConsumer is a bare retry loop that
     runs beside normal consumers.
   - queue + poolLimiter leave the constructor signature for cfg knobs — each spawned
     instance must own its queue; a caller-shared one across manager-spawned lives is
     broken. Closes TODO.md's "refactor out queue/poolLimiter" item. ~23 lab call sites.
10. producer factory-style register — BUILT (2026-08-07). Producer is a factory;
    Register returns a *ProducerInstance, callable many times.
    REVISED 2026-08-08 (user-directed, two follow-ups):
    (a) identity params moved constructor→Register: NewProducer(ds, cfg) +
    Register(ctx, topicName, version); NewConsumer(ds, cfg) + Register(ctx,
    consumerGroup, topicName, version) — one factory registers instances on any
    number of topics/groups, validation of those params lives in Register.
    (b) ProducerInstance lifecycleCtx DROPPED (reverses the keep decision below):
    Register is a stateless build step whose ctx bounds only that call's I/O;
    shutdown is per produce call via the ctx passed to it — the batcher already
    refuses a cancelled ctx before enqueue and runs BatchShutdownGrace off it.
    Accepted trade-off: a caller producing on context.Background() is no longer
    refused at app shutdown (same contract as any database call). Deleted with it:
    lifecycleErr, producer Done()==nil check, ProducerConfig.DisableGracefulShutdown
    + MetricEventConfig.DisableGracefulShutdown, pkg/producer/errors.go, and the
    ErrShutdownRequested sentinel (zero callers remained). Consumer side unchanged —
    Consume's ctx was already the sole lifetime, its Done()==nil check and
    ConsumerConfig.DisableGracefulShutdown stay.
    Original chunk 10 decisions: MetricEventProducer.Register deleted outright —
    Run(ctx) registers its own producer instance then drains, so it is re-callable
    and NewMetricEventProducer does no I/O at all; consumer Register builds a fresh
    MetricEventProducer per instance and ConsumerInstance.Consume runs it beside the
    manager in an errgroup; mergeLifecycle/stopReason/instance lifecycleCtx deleted,
    Done()==nil rejection moved to Consume, "Callable many times" doc restored on
    consumer Register; ErrNotRegistered + ErrAlreadyRegistered sentinels deleted
    (no remaining callers); producerregister lab rewritten to the new contract.
    Original plan text, for the record:
   - consumermetrics.MetricEventProducer converts too, and its Run(ctx) IS the drain
     loop. Today Register(ctx) both resolves the metrics topic AND spawns `go drain(ctx)`,
     so a consumer's Register ctx silently owns a goroutine — Register from a factory
     method is the wrong door for that. After: consumer Register builds the instance
     (call-scoped I/O), Consume runs it on Consume's ctx, and Run is re-callable across
     repeat Consumes (producer.Register today is once-per-instance and refuses a second
     call, which is what made a per-Consume rebuild the only alternative).
   - Producer's own lifecycleCtx is a DECISION, not a default drop. It binds no
     background work — the batcher's goroutine runs on context.Background by design
     (batcher.go:86) — so it is a pure admission gate, and unlike Consume there is no
     long-running call whose ctx could replace it. A request ctx is still alive while the
     app drains, so dropping the stored lifetime costs producers the ability to refuse
     new work during shutdown. Either ProducerInstance keeps the field (asymmetric with
     the consumer on purpose) or that property goes.
   - MUST FIX HERE, live defect as of the consumer Definition/Execution refactor:
     every consumer's MetricEventProducer is now built in its constructor and started
     by Register (`c.abandonedEvents.Register(ctx)`), so a SECOND Register on the same
     Consumer value fails with
     ErrAlreadyRegistered out of producer.Register (producer.go:169, and once its ctx
     cancels the producer is wound-down-stays-down). Consumer.Register's doc promised
     "Callable many times -- each call returns an independent instance"; that line was
     REMOVED rather than left lying, so restore it when this lands. No caller hits it
     today -- every lab constructs a fresh consumer per registration -- so it is latent,
     not broken. Converting MetricEventProducer (bullet 1 above) makes Run re-callable and
     resolves it; if that lands differently, amend the Register docs to once-per-value
     instead, matching Producer.Register's own contract.
   - consumerBase.lifecycleCtx + runCtx + stopReason die WITH this chunk, not before:
     AbandonedEvents.Register is the last thing binding to the consumer's Register ctx,
     so until it converts the Register ctx is a lifetime whether or not the field admits
     it. Then Consume's ctx is the sole lifetime, the Done()==nil rejection moves from
     register to Consume, and the stopped logs lose their "reason" field (only one side
     can ask). The ErrNotRegistered branch was already unreachable under the
     Definition/Execution surface and is deleted.
11. metrics — split in two (2026-08-08):
    11a. worker snapshots — BUILT (2026-08-08). WorkerSnapshot in
    pkg/metrics/datastore (model.go + worker.go): carries *common.Owner
    (system-owned rows show owner name "system"), worker name, and a
    WorkerStatus verdict — suspended (target 0) / claimed (>=1 live instance) /
    unclaimed — via classifyWorker; the counts are TargetInstances,
    LiveInstances, Attempts (max streak across live), OldestInstanceAge
    (now() - min created_at, the time-since-claimed ask), UnclaimedFor
    (now() - max expires_at while nothing live; 0 once dead rows are reaped —
    best-effort on purpose, rows linger exactly when nothing is healthy enough
    to clean them). worker_instance gained created_at TIMESTAMPTZ DEFAULT
    now() (baseline DDL edit; all write paths use explicit column lists).
    monitor/worker.go: RegisterWorkerGauges mirrors the duty trio —
    vulkan.worker.state.{unclaimed_workers,oldest_unclaimed_age,failing_workers};
    no caller yet, same standing as RegisterDutyGauges (wiring lands with the
    chunk 12 daemon / CLI). Old DutySnapshots/duty.go untouched — dies chunk 13.
    Verified on a fresh DB: a live consumer shows its exact claim chain as
    claimed with correct owners; topic/system manager rows read unclaimed (the
    chunk 12 gap, now visible in metrics).
    11b. cron-job snapshots — BUILT (2026-08-08). REVISED same day: cron jobs
    now REQUIRE an owner like every other polymorphic row — cron_job CHECK
    tightened num_nonnulls <= 1 → = 1 (baseline DDL edit), datastore
    RegisterCronJob(ctx, owner, name, ...) inserts the owner columns and
    assertConfigMatches rejects an owner mismatch, NewCronJob requires exactly
    one owner id, admin.RegisterCronJob keeps its public signature and stamps
    the SYSTEM owner (admin-registered jobs ride the system's lifecycle;
    owner-targeted public registration is a cron chunk 3/4 question).
    CronJobSnapshot carries *common.Owner via the shared toOwner adapter
    (renamed from toWorkerOwner); fields are Schedule, Suspended, NextScheduledTime,
    LastScheduledTime (zero = never fired), DueFor (now() -
    next_scheduled_time), Overdue = !Suspended && DueFor > overdueThreshold
    (flat 10m const — user chose flat over interval-scaled; a suspended row's
    stale next_scheduled_time is never overdue because unsuspend recomputes
    it). monitor/cronjob.go: RegisterCronJobGauges =
    vulkan.cron.state.{overdue_jobs,oldest_due_age,suspended_jobs}; unwired
    like the worker/duty trios. Verified cold on the dev DB: past-due row
    reads overdue, suspended 3h-stale row does not, and with a consumer
    running the scheduler fired the due row and the snapshot showed
    last_scheduled_time advance.
    11c. metrics package layered (2026-08-08) — BUILT. pkg/metrics reshaped to
    the house vocabulary/controller/datastore pattern: snapshot read-models
    (ConsumerGroupSnapshot+GroupLag, WorkerSnapshot+WorkerStatus,
    CronJobSnapshot, DutySnapshot, AbandonedRoutineSnapshot, TopicSnapshot)
    moved up to pkg/metrics. REVISED same day (the GroupSnapshot.Queue naming
    question): GroupSnapshot DELETED — it split one subject by data source and
    duplicated ConsumerGroupSnapshot's role. ConsumerGroupSnapshot is now THE
    per-group composite, sectioned by the store each number reads: Cursor
    CursorSnapshot (head/claimed/committed/backlog/inflight), Exceptions
    ExceptionSnapshot (ready/inflight/deferred/dead + OldestUnresolvedAge —
    renamed from OldestUnackedAge, ack is banned vocab, column alias
    oldest_unresolved_at), OpenLeases, AbandonedRoutines
    AbandonedRoutineSnapshot (renamed from EventSnapshot, matches
    AbandonedRoutineKey). The controller's ConsumerGroupSnapshot verb fills
    every section (event pairing included — accepted cost per gauge cycle);
    AbandonedRoutineSnapshot stays a standalone verb for reads with no real
    topic/group rows (eventsnapshotlab). Gauge family renamed
    vulkan.consumer.queue_state.* → vulkan.consumer.cursor.{head,claimed,
    committed,backlog,inflight}, vulkan.consumer.exceptions.{ready,inflight,
    deferred,dead,oldest_unresolved_age}, vulkan.consumer.open_leases.
    TopicSnapshot.Groups []ConsumerGroupSnapshot; pkg/metrics/monitor DELETED — MetricsController
    took its snapshot verbs + TopicSnapshot composite, and the otel gauges
    moved to their own pkg/metrics/metrics package (user: gauges don't belong
    beside controller logic): Metrics type = NewMetrics(controller, cfg) with
    Meter on MetricsConfig (noop default), structs renamed <type>Gauges →
    <type>Metric with Register<Type>Metric methods; the controller carries no
    meter. SQL moved down to pkg/metrics/controller/datastore returning
    query-exact *Data structs in model.go. Verdict/derivation logic (toOwner, classifyWorker,
    overdueThreshold/overdueFactor, backlog/inflight math, abandoned/cleared
    event pairing) lives in controller adapters; input validation
    (topicId/group) controller-side. Callers moved: admin (field monitor →
    metricsController, TopicMetrics returns *metrics.TopicSnapshot), CLI
    maintain status, eventsnapshotlab/dutybackofflab/metricslab. Verified:
    both modules build, eventsnapshotlab + metricslab + `vulkan maintain
    status` pass live (dutybackofflab fails identically on clean main —
    pre-existing, it's a chunk 13 rewrite).
12. CLI daemon — BUILT (2026-08-09). `vulkan maintain run` rebased onto the
    worker machinery through a new top-level door, pkg/systemmanager. Name
    researched against the field (K8s kube-controller-manager, Postgres
    autovacuum launcher, River QueueMaintainer, OTP supervisor): the umbrella
    process is conventionally named for what it does to its children, and
    this codebase's noun for that is already `manager` -- SystemManager over
    Maintainer, because the daemon IS the manager run at system scope.
    NewSystemManager(ds, cfg{Logger, Retry}) assembles janitor +
    cronscheduler + waterline definitions into a manager.ManagerDefinition;
    Run resolves the system owner via migrate.SystemOwner
    (migrate.ErrNotRegistered on a fresh DB keeps the CLI's teaching error)
    and hands off to manager.NewRunner -- the daemon claims THE SYSTEM
    MANAGER ROW; claims, heartbeats, N-way arbitration all come from
    existing machinery. Waterline is in the provisioner set for parity with
    the old fleet's WaterlineRoller: retention keeps moving while a group's
    consumers are offline. The "everything" scope settled as one clause in
    listWorkers -- `OR ($2 = 0 AND $3 = 0 AND t.system_id = $1)` -- only a
    system owner has both topic and group ids unset, and every worker row
    resolves to its system through the topic join, so system scope reaches
    down to the whole deployment while topic/group scoping stays
    byte-identical. Topic manager rows deleted: admin's topic declarers drop
    managerDefinition (nothing would ever claim one; the per-topic kill
    switch is suspending the janitor row itself); the system manager row
    earns its place as the daemon's claim anchor and deployment-wide
    suspend switch. Ridden along: dead ConsumerConfig.Meter and
    FleetMaintainerConfig.Meter deleted (defaulted to noop, read by
    nothing). Gauge wiring deliberately NOT done -- Register*Metric still
    has zero callers; the exposure story (Meter-in-config for embedders,
    CLI-hosted prometheus /metrics for the daemon) is researched and
    deferred to TODO.md. Verified live: the daemon claimed the system
    manager row and spawned all 5 topics' janitors + cron_scheduler in one
    reconcile pass, drained clean on SIGINT; metricslab passes, so group
    chains are unaffected.
13. cleanup — BUILT (2026-08-11). pkg/maintain DELETED (11 files) along with
    the maintenance table DDL + its unique indexes (baseline edit in system
    tables.go), migrate.AssertSystemSchemaSupported (both callers died with
    the fleet), and ALL duty metrics: pkg/metrics/duty.go, controller
    duty.go + toDutySnapshot + overdueFactor, datastore duty.go +
    DutySnapshotData, metrics/metrics duty.go with the
    vulkan.maintain.duty_state.* gauges. CLI: maintain_status.go deleted,
    `maintain` keeps only `run`. EnsureNextPartition deleted WITHOUT a
    rehome — settled with the user: creation is the write path's job
    (Kafka's writer rolls segments; the janitor is cleanup only).
    Correctness never depended on it: partition 0 exists from RegisterTopic
    and the produce path's reactive ensureCoveringPartition heals every
    later boundary — and had been the only live creator since chunk 5
    anyway, since the worker janitor never carried create-ahead.
    consumer.Register's cold-start call and its maintain field deleted; the
    producer's "outran janitor create-ahead" warn reworded to "no partition
    covers the next message id -- creating it". Proactive create-ahead is
    now a producer TODO (TODO.md): sentinel-id trigger at ~80% of the
    partition (ids are unique fleet-wide, so exactly one producer fires per
    partition with zero coordination), intra-process atomic gate,
    best-effort by design — the boundary heal is the only layer allowed to
    matter for correctness — and pg_try_advisory_xact_lock caps the heal
    path's thundering herd. Labs: 12 swapped maintain.NewMaintenanceDatastore
    for the janitor/waterline datastores' same-named verbs; partitionlab +
    topiclab reshaped onto heal-driven partition creation (each boundary
    burns one id on the rolled-back insert); compactionwidthlab reads the
    seeded rows' real ids back instead of hard-coding them (wide case
    reframed: 40 rows fit one partition, both EXPLAINs collapse to a single
    scan); dropfloorlab pre-creates fixture partitions with lab-local DDL to
    keep its dense-id choreography; dutybackofflab REWRITTEN onto the worker
    tick runner (rename message_log away → worker_instance.attempts climbs
    with SweepRetry-capped gaps, WorkerSnapshots surfaces the streak, reset
    on heal; needs RetentionTTL > 0 so the sweep reads message_log at all —
    this also retires the lab's pre-existing failure); maintenancelab
    REWRITTEN onto worker claims (janitor/waterline target-1 rows hold
    exactly one live instance across 3 consumers, with the NoInstanceTarget
    message_consumer as the one-loop-per-process contrast; failover and
    full-release phases; waterline reaches head). TODO.md pruned: the
    listDuties, GetGroupId, JanitorSweepBatchSize, and
    EnsureNextPartition-in-janitor notes. Verified on a drop+recreate fresh
    DB: both modules build, vet clean, go test -race green, all 18 touched
    labs pass, and `vulkan maintain run` claims the system manager and
    reconciles leftover topics' janitors plus an orphaned group's waterline.
    Post-close renames (2026-08-11): eventsnapshotlab →
    abandonedroutinesnapshotlab, maintenancelab → workerclaimlab (just
    recipes renamed to match), and `vulkan maintain run` → `vulkan manager
    run` (role-noun subcommand, airflow-scheduler/vault-server pattern;
    maintain*.go → manager*.go).

---

## open
- implementation-shaped (deferred until work is picked up): consumer-side compaction-head
  read (inline datastore method per house style, no shared pkg), entity enrollment inserts
  in topic/consumer_group Register paths, MessageOptions persistence shape on the message row (typed
  columns vs metadata — internals TBD), torture-lab coverage (two consumers fighting one
  key through crash / lease-steal / supersession chains / wedged-key starvation)

- left over from chunk 8, none blocking:
  - (CLOSED by chunk 12: the daemon claims the system manager row and a system-scoped
    ListWorkers reaches the whole deployment; topic manager rows are deleted.)
  - EnsureNextPartition is still homeless — chunk 5 deferred it and the worker janitor's
    sweep does not do it. Producer self-heal is the only cover today.
  - (CLOSED by chunk 9: CURSOR no longer builds the factory set twice -- Consumer.Register
    builds one base and one factory set.)

---

# pkg/consumer layered refactor — BUILT (2026-08-04)

Split pkg/consumer into a door, one package per worker row, and the two persistence
layers, as already done for topic (2026-08-02) and system (2026-08-03).

Gate cleared: chunks 8 and 9 are built. This supersedes the 2026-08-03 layout, which
was written before the Definition/Execution surface landed and still named symbols
chunk 9 deleted (Seed*, consumerWorkerFactory, ConsumeClaimed, RunCtx, LifecycleErr,
SetLifecycleCtx).

pkg/consumer was 4362 lines / 17 files, excluding metrics/ and _test. Built
2026-08-04: module builds, go vet clean, go test -race green, 35 of 38 labs pass
(the 3 failures predate this work -- see `## stale labs`).

## the constraint that fixed the shape

`consumer.NewConsumer` is NON-NEGOTIABLE -- the library's most common user entry
point keeps its name and package. So pkg/consumer is a DOOR (like pkg/admin is
topic's), not a vocabulary layer, and the row packages sit under it.

That means nothing a user types may live below pkg/consumer, because the door imports
the rows and the rows may not import back. Every user-facing symbol clears that bar
except one:

  MessageMeta / MetaFromContext -- the rows stamp it into ctx under an unexported
  key and the user reads it back inside their consumerFunc. It cannot be duplicated
  per row (the keys would not match and MetaFromContext would return false), so it
  needs exactly one home BELOW the rows and still reachable by the user.

Everything else stays on `consumer.` because the rows take narrow configs of their
own instead of ConsumerConfig -- see below.

## layout

pkg/consumer                          THE DOOR -- every symbol a user types
  consumer.go        Consumer[M] + NewConsumer + Register
                     + ConsumerInstance[M] + Consume
  config.go          ConsumerConfig + ConsumerType (CURSOR / LIFECYCLE) + ConsumerFunc
  definitions.go     newGroupDefinitions / newTopicDefinitions -- resolves
                     ConsumerConfig once, then builds each row's narrow config
  options.go         With* builders
  errors.go          lifecycleContextHelp
  context.go         SleepWithContext
  metrics/           unchanged

pkg/consumer/message                  leaf -- imports only context, time, pkg/common
  meta.go            MessageMeta + MetaFromContext + WithMeta

pkg/consumer/messageconsumer          worker row: message_consumer
  messageconsumer.go MessageConsumerDefinition + MessageConsumerExecution
                     + messageRunner + Declare + Provision
  config.go          MessageConsumerConfig
  claimbuffer.go     claimBuffer + Buffered
  rangestate.go      rangeState + rangeSnapshot + outcome kinds
  worker.go          const WorkerMessageConsumer + consumerWorkerMetadata

pkg/consumer/exceptionconsumer        worker row: exception_consumer
pkg/consumer/deliveryconsumer         worker row: delivery_consumer      PARKED

pkg/consumer/controller               the only door to consumer persistence
  controller.go          ConsumerController + NewConsumerController
  controller_config.go   ControllerConfig{Logger, Retry}
  group.go               Group + GetGroup / RegisterGroup
  binding.go             Bind / ClearBindings
  cursor.go              MessageRow, LeaseRow, CursorRange, ClaimedRange, MessageOutcome
                         + ClaimMessagesWithCursor / Commit / PartialCommit
                         / ForceReclaimRange
  exception.go           ClaimedException + Kill / Claim / Renew / RecordException x4
  keylease.go            KeyLeaseClaim + KeyLeaseVerdict + Claim / Release
  delivery.go            DeliveryRow + FanOut / Claim / Record x3        LIFECYCLE
  errors.go              ErrLeaseLost
  adapter.go             *Data -> vocabulary

pkg/consumer/controller/datastore      table-exact SQL, Wrap-only
  datastore.go           ConsumerDatastore + New
  config.go              ConsumerDatastoreConfig
  group.go               GroupData + SQL
  binding.go             + wildcardToRegex
  cursor.go              MessageData / LeaseData / CursorData / ClaimedRangeData + SQL
  exception.go           ExceptionData + SQL
  keylease.go            KeyLeaseData + SQL
  delivery.go            DeliveryData + SQL

Arrows: consumer -> rows -> message / controller -> controller/datastore. Nothing
points up. 20 public verbs move across; datastore.go (1687 lines) becomes six focused
files over two layers.

## why the rows get their own configs

Measured ConsumerConfig (21 fields) usage: messageconsumer 12, exceptionconsumer 7,
deliveryconsumer 4, the shared plumbing 2. The rows are not each using the whole
config, so each declares its own -- exactly janitor.JanitorConfig /
waterline.WaterlineConfig / cronscheduler.CronSchedulerConfig.

WithDefaults stays at the door because its derivations are interdependent:
QueueSize <- BatchLimit, MessageMax <- Message, ShutdownTimeout <- MessageMax.Timeout
+ TimeoutGrace + AckMargin. The door resolves once, then hands each row its slice.
This also fixes a live smell: all three New*Definition constructors currently re-run
cfg.WithDefaults() + cfg.Validate() on the same *ConsumerConfig the door already
resolved.

ConsumerFunc stays at the door too: a row constructor taking the unnamed
`func(ctx context.Context, message *Message) error` accepts consumer.ConsumerFunc[M]
with no conversion.

## no base package

The shared plumbing duplicates into each row rather than becoming
pkg/consumer/base: callSafely (44 lines), claimKeyedRun + dispatchVerdict (29), the
topic/group resolution newConsumerBase does (61), declareConsumerWorker (11), and
resolveMessageOptions (4). About 150 lines, three times, net +250.

That is the conventions.md call -- duplication beats abstraction, no new shared
packages -- and it leaves each row structurally identical to pkg/worker/janitor: its
own Provision doing its own GetTopicById, its own config, its own worker-name const.
deliveryconsumer takes the smaller copy: it uses callSafely but never claimKeyedRun.

## why MessageMeta lands in pkg/consumer/message

Three homes were possible and only one reads right at a user's call site.

- pkg/common, beside MessageOptions. REJECTED: min/max/override bounds and
  MessageMeta are consumer-only. Checked -- the producer's only options field is
  ProducerConfig.Message, merged with Fill; it never Clamps or resolves concurrency.
  pkg/common is for what BOTH sides need (Owner, MessageOptions), so this would be a
  junk-drawer move.
- pkg/consumer/controller, beside MessageRow / ClaimedException, which
  toMessageMeta converts from. On-pattern for the type, but it puts
  consumercontroller.MetaFromContext inside a user's callback -- the persistence door
  addressed from application code.
- pkg/consumer/message. CHOSEN. Consumer-scoped by its path, leaf-clean (context,
  time, pkg/common only), and reads as message.MetaFromContext(ctx). The
  toMessageMeta adapters stay in each row, so nothing in the consumer tree points
  into it except the two rows that stamp meta -- deliveryconsumer never does.

Known wart: WithMeta must be exported for the rows to call it, so a user can stamp a
fake meta into their own ctx. Harms only that caller; not worth contorting around.

MessageBounds (a named Default/Min/Max/ConcurrencyOverride type) was considered to
stop the four options fields repeating across three row configs. DROPPED:
resolveMessageOptions is 4 lines wrapping common.MessageOptions's own Fill / Clamp /
ResolveConcurrency, so the same duplication call that killed base kills this too.

## cleanups this carries

- ConsumerDatastore[Message any] is a PHANTOM type parameter -- zero uses in any
  field, signature, or body. Payload is json.RawMessage end to end; unmarshalling
  happens in the rows. Drop it: de-genericizes both persistence layers and removes
  the type argument from 16 lab call sites.
- Wrap-only violations: ReclaimWithCursor and FreshClaimMessagesWithCursor are
  EXPORTED, own their own transactions, have no Wrap, and are called from inside the
  private claimMessagesWithCursor -- exported names doing private work. They become
  the private halves. ClaimMessages / quarantine / readMessages / record /
  recordAndReleaseKey are tx-scoped or helpers: stay unwrapped but private
  (cronscheduler precedent -- no Wrap inside a txn).
- Validation moves to the controller: newConsumerBase's version < 1 check, the
  datastore's inline low >= high sanity check.
- CONSOLIDATE the four range outcome types into ONE kinded type. MessageException /
  MessageTerminal / MessageSuperseded / MessageDeferred are structurally identical
  ({MessageId int64, Err string}), constructed in exactly one place (rangestate.go's
  contiguousResolved + resolvedOutcomes), and consumed only by Commit / PartialCommit.
  One type carrying the kind collapses both four-armed switches into a single walk and
  drops Commit from 10 params to 7, PartialCommit from 11 to 8. Ripples:
  cursorPartialCommit and 5 lab call sites that build MessageException directly
  (exceptionlab, deliveryloglab x4).
- MessageOutcome lives in controller, NOT controller/datastore: it carries no db:
  tags, unlike MessageRow / LeaseRow / CursorRange / ClaimedException / DeliveryRow,
  which all do. It is rangeState's output vocabulary that the write path consumes, and
  it sits in datastore.go today only by history.
- The three worker-name consts split out of worker.go onto the row that owns each,
  matching janitor's `const WorkerJanitor = "janitor"` sitting in the package that
  implements it. consumerWorkerMetadata copies with them.
- EnsureNextPartition needs a home in this pass. consumer.go builds a
  maintain.MaintenanceDatastore purely for it, and chunk 13 deletes pkg/maintain --
  either it becomes a controller verb here or the janitor's sweep picks it up.
  Tracked in `## open` as homeless since chunk 5.

## settled decisions

- Row type names keep the house stutter: messageconsumer.MessageConsumerDefinition /
  MessageConsumerExecution, matching janitor.JanitorDefinition. Dropping it to
  messageconsumer.Definition reads better but breaks the pattern the four worker
  packages just standardized on -- revisit repo-wide in the v1 API review, not here.
- Read-models live in controller, not a vocabulary package. With pkg/consumer settled
  as a door, there is no vocabulary layer for them to sit in, and controller +
  controller/datastore is a complete two-layer stack on its own.
- adapter scope: convert only what the runtime manipulates (ClaimedRange,
  ClaimedException, MessageRow, LeaseRow, DeliveryRow).
- controller, controller/datastore, message, and the three rows are ORDINARY
  packages, not internal/ -- surface trimming is a later pass.

## sequencing

Decide LIFECYCLE before starting. Deleting deliveryconsumer rather than parking it
drops ~560 lines plus DeliveryRow, delivery.go in both persistence layers, and
FanOutBatchLimit -- one of the three rows disappears and its narrow config with it.
config.go already calls it "a strictly more expensive CURSOR" that re-earns its place
only with the non-FIFO queue work. Deferred, not rejected on merit; the layout makes
the deletion a whole-directory drop later.

## what shipped differently from the plan above

- Read-model names dropped `*Row` in the controller layer: MessageRow -> Message,
  LeaseRow -> RangeLease, DeliveryRow -> Delivery. The plan carried the old names
  forward, but `*Row` in a layer whose whole job is to abstract the database is the
  naming the user already rejected, and conventions.md says fix every occurrence.
  `*Data` in controller/datastore keeps the row-shaped names.
- Lease tokens are uuid.UUID in the controller vocabulary and pgtype.UUID only in
  controller/datastore, converted by toTokenData -- the worker adapter's precedent.
  pgx no longer leaks above the datastore.
- ErrLeaseLost is declared in controller/datastore (the layer that detects it) and
  bound in controller/errors.go as `var ErrLeaseLost = datastore.ErrLeaseLost`, so
  errors.Is matches on either side without callers importing the datastore.
- The per-row config builders are PUBLIC: cfg.MessageConsumerConfig(),
  cfg.ExceptionConsumerConfig(), cfg.DeliveryConsumerConfig(). Four labs
  (abandonedevents, defer, metrics, shutdowntruncation) drive a single worker row
  directly and need to derive its config slice from the group's.
- The rows never build a Group read-model. newConsumerBase used to construct one
  from Owner fields; the rows read Owner.ConsumerGroupId / Owner.Name instead.
- The `low >= high` guard stayed in controller/datastore. The plan listed it as
  validation to move up, but low and high come from the cursor statement, not from
  a caller -- it catches a cursor row that went backwards, not bad input.
- ReclaimWithCursor, FreshClaimMessagesWithCursor and ClaimMessages went private as
  planned, and each dropped a `limit` parameter no body ever read.
- Bind and ClearBindings gained the DatastoreRetry.Wrap they were missing.
- ~140 lines of commented-out dead code at the tail of the old datastore.go were
  deleted rather than carried across.

## stale labs (all predate this refactor)

Three labs assert against pkg/maintain's duty table, which chunks 8 and 9 replaced
with worker rows. They are chunk 13's rewrite list:

- maintenancelab -- "janitor executions ~ one per interval: got 0"
- dutybackofflab
- consumergrouplab -- "0 waterline duties at registration". Verified against
  HEAD: registerGroup has only ever inserted consumer_group + cursor, so the
  duty assertion was already dead before this work touched it.

## rejected alternatives

- EXTRACTING rangeState (and/or claimBuffer) into its own generic package. Rejected on
  evidence: three things reach into rangeState's internals, not one. claimBuffer
  mutates raw atomics directly (state.dispatched.Add at claimbuffer.go:97,
  state.stale.Store at :147) and messageRunner.closeRange reads state.stale /
  state.lease and calls neverDispatched + contiguousResolved. A boundary anywhere in
  that triangle exports 7+ members whose only callers are inside the same mechanism,
  and publishes the very atomics whose correctness rests on unexported write ordering
  (result.resolve writes kind/err THEN done; TryGetSnapshot gates on a one-shot CAS).
  The unit is "in-flight range bookkeeping", not "rangeState" -- both files stay
  private in pkg/consumer/messageconsumer. Only the outcome vocabulary moves, and it
  moves DOWN to controller.
- cursor / lifecycle MODE packages (the original 2026-08-03 layout). Rejected:
  message_consumer and exception_consumer share no code, so the grouping was taxonomy,
  not cohesion. exceptionconsumer.go has zero references to claimBuffer / rangeState /
  Buffered.
- FLAT runtime -- rows stay as files in pkg/consumer, only persistence splits out.
  Zero new packages and no MessageMeta problem at all. Rejected: the rows are genuinely
  independent worker rows and pkg/worker already proves one package per row; keeping
  them flat also leaves the parked LIFECYCLE path tangled with the two live ones.
- pkg/consumer/base holding consumerBase + ConsumerConfig + ConsumerFunc + MessageMeta.
  Rejected: it puts &base.ConsumerConfig{} and base.MetaFromContext(ctx) -- symbols
  users type at 23 and 2 call sites -- behind a package named for plumbing.
- MOVING the door out so pkg/consumer could be the vocabulary layer per template
  (pkg/consumergroup.NewConsumer). Matches topic/worker exactly and would put the
  read-models back in pkg/consumer. Rejected outright by the user:
  consumer.NewConsumer is the most common entry point and stays.
- deleting LIFECYCLE outright instead of parking it. See `## sequencing`.

TODO - need to integrate this as a phase into learning plan after completion