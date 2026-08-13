# default alerts — executor build plan

Closes the last big Phase 14a item. Settled design record = LEARNING_PLAN 14a
"Default alerts" bullet + memory `vulkan-default-alerts`; this plan adds the
2026-08-12 decisions (terminology, executor shape, hosting) and the chunk
order. Delete this file at close-out; the 14a bullet resettles as-built.

## Context (state as of 2026-08-12)

- Cron system COMPLETE and closed out (all 5 generic-systems tasks done,
  36/36 labs green on a fresh DB). The alert executor was the last thing
  waiting on it.
- Built already: pkg/alert vocabulary (Alert, typed identity, keys), one
  file per check (partition_count.go, compaction_read_cost.go) + jobs.go
  registration list, `__system.alerts` compacted topic, per-check cron jobs
  seeded at RegisterSystem via ensureSystemCronJob (GET-THEN-CREATE — an
  operator's AlterCronJob'd threshold/schedule must survive re-register; NOT
  bare idempotent register, which would error ErrCronJobConfigMismatch).
  Job names `alert.partition_count` / `alert.compaction_read_cost`,
  @hourly, concurrency defer, data `{"threshold": 0}` (0 = derive live).
- NOT built: the executor consumers (this plan), register-time findings
  (spine layer 2), CLI read surface, alert lab.
- The three-layer spine (unchanged): (1) WARN logs via host's injected
  logger — level-triggered evaluation, edge-triggered notification,
  Postgres MESSAGE/DETAIL/HINT; (2) one-shot log-only check pass at
  producer/consumer Register; (3) `__system.alerts` topic = alerts-as-
  messages, the integration + read surface. Compaction key
  `<name>/<owner-kind>/<owner-id>` — the topic IS the state store and
  dedup memory; the head per (check, owner) is current state.

## The parked branch — mine it, then delete it

`default-alerts`, one commit `0aa6fd9` (2026-07-28, "WIP: default alerts —
parked for duty-system refactor", +891 lines). It built the evaluator as a
per-topic maintenance DUTY — two architecture generations stale:
pkg/maintain is deleted, the Check interface / Checks() factory /
CheckConfig died in the 2026-08-01 restructure, system.AlertPollRate is
gone (per-job schedules), and every producer call uses the pre-chunk-10
API. Port NOTHING wholesale.

The ONE piece that exists nowhere on main and must be ported:
`pkg/maintain/alert.go`'s pure `classify` + its table test
(pkg/maintain/alert_test.go), plus the publish/logAlert shape around it
(`git show 0aa6fd9:pkg/maintain/alert.go`). Its settled semantics:

    classify(fresh, head, repeat, now) -> (toPublish, edge)
      activeHead = head exists && head.Status is the active status
      if fresh != nil:                     # condition holds right now
        !activeHead        -> publish fresh, EDGE      # new incident
        severity changed   -> publish fresh, quiet
        head age >= repeat -> publish fresh, quiet     # repeat that also
                              # REFRESHES THE HEAD so retention can't sweep
                              # a live alert (the subtle detail — keep it)
        else               -> nothing
      else if activeHead   -> publish copy of head with Status resolved,
                              EDGE          # copy keeps identity+evidence+key
      else                 -> nothing
      # evidence (Data/Metadata) NEVER enters the decision. Pure func.

Host-log shape: WARN on an active edge (message + detail/hint/alert/
owner/severity keys), info one-liner on a resolve edge; repeats and
severity changes publish silently. After the port, delete the branch.

## Chunks (one at a time, user reviews/commits between)

### Chunk 1 — terminology sweep: 'firing'/'fire' out of the alert domain [DONE 2026-08-12]

User-settled 2026-08-12 (reverses the earlier "alert firing stays —
Prometheus vocabulary" carve-out): Status values become **'active' /
'resolved'** — plainest words, no product jargon, no collision ('raised'
would collide with the delivery domain).
- `alert.StatusFiring` -> `StatusActive` (const value 'firing' ->
  'active'); StatusResolved unchanged.
- Prose verbs everywhere in pkg/alert + LEARNING_PLAN/NOTES where they
  describe the mechanism: a check whose condition holds "publishes an
  active alert"; the repeat-interval republish is a "repeat", never a
  "re-fire"; `AlertRepeatInterval` KEEPS its name.
- Correct the records that memorialized the old carve-out: LEARNING_PLAN
  14a cron bullet + NOTES.md Phase 14a (cron) section + memories
  `vulkan-cron-scheduler` / `vulkan-default-alerts` all contain
  "alert-domain 'firing'/'resolved' stays (Prometheus vocabulary)" — flip
  those lines when sweeping.
- No live consumers of the stored status exist yet (nothing writes alerts
  in prod), so the value rename has no migration surface; the alert topic
  is empty until the executor ships.

### Chunk 2 — port classify into pkg/alert [DONE 2026-08-12; branch deletion pending commit]

- `classify` + table test into pkg/alert (it is alert vocabulary/logic, not
  consumer machinery), renamed to the chunk-1 vocabulary (activeHead etc.).
- Adapt the head parameter to the current producer read: the branch used
  `*producer.MessageRow[alert.Alert]`; use what `GetCompactionHead`
  returns today. (TODO.md carries "rethink making GetCompactionHead live
  on producer" — do NOT resolve that here; use it where it lives.)
- Delete the `default-alerts` branch once merged (it is fully mined).

### Chunk 3 — the executor [DONE 2026-08-12]

As built (final shape, user-iterated 2026-08-12): EACH CHECK IS ITS OWN
PROCESS IN ITS OWN SUBPACKAGE — `pkg/alert/partitioncount` and
`pkg/alert/compactionreadcost`, one concept per file. Each check owns:
`Name`/`JobName`/schedule consts + `NewJob()` (job.go — the cron spec
RegisterSystem seeds; NEVER a bare-noun `Job()` factory), a `Handler`
STRUCT (handler.go) `{datastore, publisher}` with
`NewHandler(ds, publisher, cfg *HandlerConfig)` building its own check
datastore, `Handle(ctx, request)` (decode threshold → run) and `run` as a method that
publishes per topic as it goes (no results slice), an error-returning alert
constructor `new<Check>Alert` that validates its premise (alert.go — the
crossing DECISION lives in run, constructing an unwarranted alert is an
error, no `a, _ :=` swallows), `HandlerConfig{Logger,Retry}`
(handler_config.go), and ITS OWN `datastore/` SUBPACKAGE on the layered
shape (`<Check>Datastore` Wrap-only publics, `TopicData` in model.go,
`<Check>DatastoreConfig`). The shared AlertDatastore is DELETED
(duplication beats abstraction). Parent pkg/alert: `Publisher` struct
(publisher.go) `{alerts, repeat, logger}` — `Publish(ctx, name, owner,
alert)` = head → classify → produce → log-on-statusChanged; alert nil =
check found nothing, still flows so actives resolve. CheckResult WAS BUILT
AND DELETED (pure pair-ferry — user: pass owner/alert directly). The `alert.Executor` pair-struct WAS BUILT AND DELETED
(user: "too many layers of back and forth indirection") — the chunk-4 seam
is each package's `JobName` + `NewHandler(...).Handle` directly; the only
check→parent runtime crossing is publisher.Publish. The `Jobs()`/
`Executors()` aggregators are DELETED — assemblers enumerate the subpackages
explicitly (admin RegisterSystem seeds partitioncount.NewJob() +
compactionreadcost.NewJob(); chunk 4 wires each NewHandler the way
systemmanager lists janitor/cronscheduler/waterline). run covers EVERY
topic — a nil alert flows through Publish so actives resolve (an uncompacted
topic publishes nil for the same reason). No consumer imports
anywhere in pkg/alert/*. classify's table test was removed by the user —
labs are the verification surface.

One consumer group PER check; the binding table is the dispatch. (A public
`Run(ctx, ds, name, data)` switch + one shared `alert.*` consumer group was
built 2026-08-01 and KILLED 2026-08-02 — never re-suggest a central
dispatcher switching on job name.)

    # chunk-4 assembly, one consumer per check (as built in chunk 3):
    publisher := alert.NewPublisher(alertsInstance, repeat, log)   # once
    for each check package (partitioncount, compactionreadcost):
        handler := <check>.NewHandler(ds, publisher, cfg)
        consumer := consumer.NewConsumer[cron.JobRequest](ds, cfg)
        instance := consumer.Register(ctx,
            group = <check>.JobName,            # "alert.partition_count"
            topic = cron.TopicName, v1)         # binding: the job name, exact
        instance.Consume(ctx, handler.Handle)

    # inside each package (built): Handle = decode threshold -> run;
    # run enumerates its topics, decides, publisher.Publish per topic
    # (head -> classify -> produce -> log on statusChanged); publish
    # errors joined so one topic never skips the others.

Load-bearing facts:
- The job's `concurrency: defer` means key_lease already guarantees one run
  per group at a time; the job's Timeout bounds the handler. NOTHING new to
  build for overlap.
- Handler errors ride the normal retry/exception machinery. Evaluation is
  level-triggered, so a lost/failed run self-heals at the next schedule —
  no state to repair.
- `AlertRepeatInterval` reads from the system row (KEPT on system in the
  2026-08-01 restructure: repeat cadence is evaluator behavior, not
  scheduling). Read it at Register/startup, like the branch snapshot did.
- Current APIs to use (post-chunk-10 shapes): NewConsumer(ds, cfg) +
  Register(ctx, group, topic, version) -> instance; NewProducer(ds, cfg) +
  Register(ctx, topic, version) -> instance; Produce returns
  *ProduceResult[M].
- SETTLED at build: handlers live in the check subpackages, consumer
  assembly is chunk 4's (systemmanager); pkg/alert/* has no consumer
  imports. Each check's datastore/ subpackage is on the layered shape.

### Chunk 4 — hosting in systemmanager (user-settled 2026-08-12)

The daemon hosts the executors. Rationale (recorded so it isn't relitigated):
- The handler is LIBRARY code — no user function anywhere in it. A
  documented "also run this consumer" step = alerts silently dead wherever
  the doc is missed; the system watching itself must not be opt-in
  assembly.
- It is literally system-owned work, and without the daemon nothing
  produces job requests anyway (cronscheduler lives there) — an executor
  without the scheduler consumes an empty topic, so coupling costs nothing.
- Consumer.Register seeds worker rows for each alert group, so executors
  show in ListWorkers, suspend via target_instances = 0, and N daemon
  replicas arbitrate with existing claim machinery. Per-check off
  switches: suspend the cron job (stop producing) or the consumer rows
  (stop evaluating).
- Accepted trade: embedders running RegisterSystem without the daemon get
  no alert evaluation — already true of their janitors/cron production;
  document it. If embedding ever demands it, expose the executor set as a
  constructor the way systemmanager itself is exposed.

Build: NewSystemManager assembles the per-check consumers beside
janitor/cronscheduler/waterline definitions. EACH executor runs as its own
separate process — its own consumer/worker rows, individually visible and
suspendable; never merged into one shared goroutine or a single combined
runner (user directive 2026-08-12, same rule as the per-file handlers).
Live-verify: daemon up -> due alert job produces ->
executor evaluates -> `__system.alerts` head moves -> WARN on edge, silence
on repeat inside AlertRepeatInterval, info on resolve after fixing the
condition.

DONE 2026-08-13. As built (REBUILT same day onto worker Definitions after
user review — the first build ran the consumers as errgroup goroutines
inside SystemManager.Run, which made systemmanager non-generic; never
re-suggest hosting them outside the worker machinery):
- Bind made idempotent first (declares re-run every RegisterSystem):
  `binding` gains `UNIQUE (consumer_group_id, pattern)` + `ON CONFLICT DO
  NOTHING` in the insert; the separate binding_group index deleted — the
  unique index serves the group lookup.
- Each alert package is a full worker kind on the janitor/waterline
  template: definition.go (`New<X>Definition(ds, cfg *DefinitionConfig)`,
  `Name() = JobName`, Provision = RegisterInstance claim), declare.go
  (Declare(ctx, systemOwner): resolve cron topic -> RegisterGroup(JobName)
  -> Bind(exact job name) -> InsertWorker(JobName, groupOwner)),
  execution.go (InstanceRunner heartbeat around `consume`: GetSystem
  AlertRepeatInterval -> producer Register on `__system.alerts` ->
  NewPublisher -> NewHandler -> jobRequestConsumer.Register ->
  instance.Consume — publisher rebuilt per claimed life, so an altered
  repeat applies on the next claim), metadata.go (empty, no knobs yet),
  definition_config.go ({Logger, Retry, InstanceTTL 30s}).
- admin.NewMessageAdmin builds both definitions as `alertDeclarers`
  (declarer-only, never run); RegisterSystem declares them AFTER the
  topics + cron jobs (they resolve the job_requests topic).
- systemmanager reverted to generic: the two definitions just join
  janitor/cronscheduler/waterline in NewManagerDefinition; Run is
  runner-only again (pkg/systemmanager/alert.go deleted).
- Live-verified on a fresh DB: RegisterSystem declares both group+worker
  rows -> daemon's system manager SPAWNS both consumers ("manager spawned
  worker worker=alert.partition_count") -> full active->resolve cycle via
  cron alter/run threshold 1->0 (3 WARNs, heads active->resolved) ->
  suspend: target_instances=0 does NOT kill the running instance (claim
  gate only, same as every worker); restarted daemon skips it, and setting
  target back to 1 makes the LIVE manager spawn it within seconds.
- Repeat-interval silence not exercised live (4h default); classify's repeat
  arm is the only untraveled path — alertlab (chunk 7) covers it.

### Chunk 5 — register-time findings (spine layer 2)

One-shot, log-only check pass at producer/consumer Register. Shape at
build; keep it log-only (no topic writes from a register path).

DONE 2026-08-13. REBUILT same day on user correction — the first build
duplicated the queries/thresholds/texts into producer and consumer ("each
side's datastore" convention misapplied); the alert domain OWNS its
measurement and decision, so it is defined once and injected. Never
re-suggest per-side copies of domain logic that has an owning package. As
built:
- pkg/alert is now PURE VOCABULARY (no producer import): publisher.go +
  classify.go moved to new pkg/alert/publisher (publisher.Publisher,
  producer.Producer-style naming). classify/Publish first param renamed
  `alert` -> `found` (would shadow the package qualifier; `found` is the
  handler's established word).
- Each alert got the house controller/datastore layers:
  pkg/alert/<alert>/controller (+ datastore/ moved under it). The
  controller owns the WHOLE condition as one verb:
  `Evaluate(ctx, owner, threshold) (*alert.Alert, error)` — nil when none
  applies, threshold 0 = derive live (JobData semantics). The name const
  (AlertPartitionCount / AlertCompactionReadCost), warnDivisor /
  warnPartitions, and new<X>Alert (texts) all live in the controller —
  it can't import its parent (parent imports consumer), so JobName =
  "alert." + controller.Alert<X>.
- Handler shrank to decode -> ListTopics -> loop controller.Evaluate ->
  publisher.Publish; its threshold/ceiling logic and datastore field are
  gone.
- SECOND ROUND same day (user: handler/publisher mixed into the
  definition/execution space, and both are new terminology): Handler +
  HandlerConfig DELETED — the work is a method on the execution, janitor
  shape (`evaluateTopics(ctx, request)`: decode -> ListTopics -> loop
  Evaluate -> Record; definition builds+holds the condition controller).
  pkg/alert/publisher -> pkg/alert/controller: AlertController, verb
  `Record(ctx, name, owner, found)` (Record* family; "publish" was not a
  codebase verb), built per claimed life in consume (repeat re-read).
  "handler" and "publisher" retired from the domain vocabulary — the
  nouns are alert / job / controller (Evaluate = the condition,
  AlertController.Record = the write door) / the worker template's
  definition/declare/execution. Vocabulary prose in pkg/alert swept
  ("one handler finding" -> "what one run found").
- Producer/consumer INJECT the controllers: NewProducer/NewConsumer build
  both (like topicController) into `evaluators []alert.Evaluator` — the
  role interface lives in the pkg/alert VOCABULARY, worker.Provisioner
  pattern (a private per-side interface was built first and killed same
  day: not an established pattern). logAlerts = build topic owner
  -> loop Evaluate(ctx, owner, 0) -> WARN with found.Message/Detail/Hint/
  Name/Owner.Name/Severity. Log-only, never fails Register; pass errors
  log "register-time alert pass failed". Level-triggered per Register.
- Deleted: all per-side copies (producer datastore reads/consts/texts,
  ConsumerController PartitionCount/PartitionLockCeiling/Compacted +
  controller/datastore/alert.go).
- Verified: build/vet/`go test -race ./pkg/...` (77 in 63 pkgs);
  producerregister + routinglab clean; register-pass WARNs smoke-fired
  live on both sides (temp threshold 1 vs __system.job_requests, both
  alerts, then reverted); executor end-to-end re-verified through the new
  layers (daemon up -> cron alter threshold 1 -> run: 3 edge WARNs ->
  threshold 0 -> run: 3 resolve INFOs, heads resolved).

### Chunk 6 — CLI read surface

Read `__system.alerts` heads: current state per (check, owner), probably
`vulkan alerts` or under `vulkan system`. Naming/shape at build with the
user.

DONE 2026-08-13. User picked `vulkan alert` tree + all-groups bindings. As
built:

- `vulkan alert list [-q]` — one row per (alert, owner): NAME OWNER STATUS
  SEVERITY SINCE MESSAGE; quiet prints `name kind/owner`. Backed by
  `MessageAdmin.ListAlerts` (registers the alert producer, lists heads).
- Heads listing EXTENDS the producer's compaction machinery, not a parallel
  read: `ListCompactionHeads` on datastore/controller/instance, sharing the
  single head SELECT (`headSelectSql`) with `GetCompactionHead`, ordered by
  compaction_key.
- `vulkan alert bindings` — the TODO's binding audit surface, ALL groups:
  GROUP TOPIC VERSION PATTERN. `ConsumerController.ListBindings` +
  datastore join (COALESCE(display, pattern)); `MessageAdmin.ListBindings`;
  admin grew consumerController + alertProducer fields.
- Live-verified against the dev DB: 3 resolved partition_count heads from
  the chunk-5 executor run render correctly; both alert consumer bindings
  list.
- Follow-up same day: standalone head reads moved out of the producer into
  the new pkg/compaction read domain (TODO "compaction API shape" taken
  early). ListAlerts = GetTopic + CompactionController.ListCompactionHeads
  (no producer Register); AlertController reads heads via an injected
  CompactionController[alert.Alert]; producer keeps only
  GetCompactionHeadInTx + the head upsert. Re-verified live: alert list,
  forced job run 10/10 succeeded under the daemon, multitargetlab +
  compactionheadwritelab.

### Chunk 7 — alertlab + close-out

- Lab: register system -> suspended-vs-active check jobs, run-now a check
  job, active edge (WARN + head), repeat republish refreshes head, resolve
  edge after clearing the condition, severity-change silent publish,
  bindingless/other groups untouched, per-topic error isolation.
- Close-out: lab-mirror grep sweep, full fresh-DB suite, resettle the
  LEARNING_PLAN 14a "Default alerts" bullet as-built, NOTES.md Phase 14a
  (alerts) section, delete this file, update memories
  (vulkan-default-alerts, vulkan-cron-scheduler terminology line), delete
  the default-alerts branch if not already done.
- After this: 14a has one unchecked bullet left (group destroy verb + CLI),
  then the phase gate — every item fixed or written decision, NOTES.md,
  `git tag phase-14a`.

## Settled decisions — do not re-suggest the losers

- NO central dispatcher / shared executor group switching on job name (user
  killed it 2026-08-02). One consumer per alert; the binding table is the
  dispatch.
- The `alert` DUTY KIND is dead (2026-08-01), with alert_poll_rate_ns and
  per-check config surface — scheduling is per-check cron jobs.
- Alert identity = Name + *common.Owner + Status + Severity (v1 ships only
  "warn"). REPLACED 2026-08-12: the Entity* fields (EntityType/EntityId/
  EntityName + SystemEntityName) were 'entity' jargon duplicating
  common.Owner — Owner constructors carry the validation, Owner.Kind()/ids
  feed the keys, Owner.Name is the human handle. Typed Value/Ceiling
  numeric fields REJECTED; evidence lives in Data (the check's
  measurements) / Metadata (about the report); neither ever
  routes/keys/dedups, and evidence never enters classify.
- Routing key `alert.<name>.<owner-kind>.<severity>.<owner-name>` (owner name
  last: the only dot-carrying field, so the prefix keeps a fixed depth);
  compaction key `<name>/<owner-kind>/<owner-id>`; retention interplay
  intentional (the repeat republish outruns it).
- Suspended check job = alert state freezes until unsuspend (honest cron
  semantics; next run resolves) — accepted 2026-08-01.
- Old schema limits DISSOLVED by the Owner switch: system alerts carry the
  real systemId, and consumer-group owners are addressable (no alert emits
  one yet).
- Thresholds computed live where possible (max_locks_per_transaction ->
  real partition ceiling); job data threshold 0 = derive live.
