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

### Chunk 3 — the executor

One consumer group PER check; the binding table is the dispatch. (A public
`Run(ctx, ds, name, data)` switch + one shared `alert.*` consumer group was
built 2026-08-01 and KILLED 2026-08-02 — never re-suggest a central
dispatcher switching on job name.)

    for each check in alert.Jobs():
        consumer := consumer.NewConsumer[cron.JobRequest](ds, cfg)
        instance := consumer.Register(ctx,
            group = check's job name,           # "alert.partition_count"
            topic = cron.TopicName, v1)         # binding: the job name, exact
        instance.Consume(ctx, handler(check))

    handler(check)(ctx, request *cron.JobRequest) error:
        threshold := decode(request.Data)       # {"threshold":0}; 0 = derive live
        for topic := range ds.topics():         # each check enumerates its own
                                                # scope (AlertDatastore.topics())
            alert := check.decide(owner(topic), threshold)  # pure decision func
            head  := alertProducer.GetCompactionHead(ctx, CompactionKey(check, owner))
            publish := classify(alert, head, repeatInterval, now)
            if publish != nil:
                alertProducer.Produce(ctx, publish,
                    RoutingKey: publish.RoutingKey(),
                    CompactionKey: publish.CompactionKey())
                if statusChanged(publish, head): host log (WARN active / info resolved)
        return joined per-topic errors          # one topic's failure never
                                                # skips the others

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
- Open at build: where the handler/assembly code lives — pkg/alert holds
  the run/decision funcs; the consumer assembly probably belongs in
  systemmanager (chunk 4), keeping pkg/alert free of consumer imports.
  Decide with the code in front of us. (pkg/alert itself is NOT yet on the
  layered pattern — datastore.go sits directly in the package; that
  refactor is TODO.md's "refactor rest of packages", NOT this plan.)

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
janitor/cronscheduler/waterline definitions; Run consumes on the manager's
ctx (errgroup beside the runner, or as worker definitions if the shape
fits — decide at build). Live-verify: daemon up -> due alert job produces ->
executor evaluates -> `__system.alerts` head moves -> WARN on edge, silence
on repeat inside AlertRepeatInterval, info on resolve after fixing the
condition.

### Chunk 5 — register-time findings (spine layer 2)

One-shot, log-only check pass at producer/consumer Register. Shape at
build; keep it log-only (no topic writes from a register path).

### Chunk 6 — CLI read surface

Read `__system.alerts` heads: current state per (check, owner), probably
`vulkan alerts` or under `vulkan system`. Naming/shape at build with the
user.

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
- Routing key `alert.<name>.<owner-kind>.<owner-name>.<severity>`;
  compaction key `<name>/<owner-kind>/<owner-id>`; retention interplay
  intentional (the repeat republish outruns it).
- Suspended check job = alert state freezes until unsuspend (honest cron
  semantics; next run resolves) — accepted 2026-08-01.
- Known schema limits (recorded, not solved): system-scoped EntityName
  pinned by key derivation; consumer-group-scoped subjects not addressable.
- Thresholds computed live where possible (max_locks_per_transaction ->
  real partition ceiling); job data threshold 0 = derive live.
