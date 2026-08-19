# Roadmap

Future work, in order of intent. Not a promise — items reorder freely.

- **Now** — committed next work. Picking an item up expands it into TODO.md's
  working window.
- **Next** — agreed, scope still evolving.
- **Later** — intended, roughly sequenced.
- **Parking lot** — no commitment; picked up only if a real workload demands
  it. Ideas move here rather than being deleted.

An item starts as a one-liner and accumulates design notes as sub-bullets.
When a design settles, it gets a decision record in `docs/decisions/` and the
item slims to a pointer. When work ships, its summary moves to HISTORY.md and
the item is removed.

## Now

**The 14b cleanup / public API design pass** — naming, shape, comments, and
internal cleanup; no new behavior. Locks the surface before v1.

Ordered: internal restructuring first, public-surface decisions late so they
stay revisable, text polish (naming/errors/logging/comments) last.

- **Worker-tier surface review** — everything the worker tier exports
  postdates Phase 13's painstaking pass, so it hasn't had one: pkg/worker
  and its kind subpackages (janitor, waterline, manager, cronscheduler),
  pkg/worker/controller (WorkerController and the configs),
  pkg/systemmanager, the consumer split
  (Consumer/MessageConsumer/ExceptionConsumer/DeliveryConsumer, one shared
  ConsumerConfig), the renamed Producer/ProducerConfig, and the
  `vulkan manager` commands. Same rigor as Phase 13. The review's reading
  doubles as the internal readability-debt sweep (long funcs, shaping
  wads) — file both kinds of findings.
  One deliberate trap to re-examine: NewMessageConsumer KEPT its signature
  while changing meaning (bundle -> bare work loop) — decide whether the
  bare piece constructors should be harder to reach by accident.
- **`Message` generic vs a `struct{}`-based shape** for producer/consumer —
  decide and document. Weigh Go 1.27's new generics/type-inference features
  before finalizing.
- **Compaction API shape** (for the v1 review; the standalone-head-read move
  into pkg/compaction/controller shipped 2026-08-13):
  - A dedicated compacted-topic handle — Compact(Producer|Consumer) idea;
    NATS JetStream KV precedent: one typed handle doing Get + CAS-produce
    with CompactionKey required. Would sit on top of the compaction
    controller unchanged.
  - consumerFunc could hand users a common.MessageRow[Message] instead of
    payload-arg + context MessageMeta — the typed row moved to pkg/common
    2026-08-13, so both sides could share it; the consumer's raw internal
    row (payload + options columns) stays its own struct either way.
- **Config & options refinement:**
  - Required variables become plain params; only truly optional ones stay in
    Config structs, renamed OptionalConfig to make the split obvious at call
    sites. Could make compaction_key + compaction_rank required params of a
    single 'compaction' option to avoid validation.
  - Re-evaluate consumer Register params — Consumer.Register grew to 5
    (ctx, group, topic, version, bindings) and NewConsumerInstance to 7 with
    the binding lifecycle work (2026-08-13); structure them (e.g. a named
    registration struct or rebalanced required/optional split) instead of
    letting the lists keep growing.
  - Group/order config options and table fields by likeness.
  - Pass through config/options/vars/params and delete dead fields.
- **Public surface trim** (decisions settled 2026-08-01, recorded in
  _public-surface.md; build pending — deliberately late so the decisions get
  re-confirmed after living with the surface through the passes above):
  - `concurrency` pkg hidden entirely — consumers build queue + pool
    internally from ConsumerConfig, constructors drop the two params (also
    removes the consumer.Buffered leak).
  - All three sub-consumer constructors stay public; full maintain surface
    stays public.
  - `migrate` pkg + both migrations.Registry vars move to internal/ —
    admin.MigrateTopic(s)/MigrateSystem are the only user migration entry;
    CLI keeps access via the import-path prefix rule. MigrateTopic +
    MigrateTopics both stay — distinct ops.
  - Broader internal/ moves were deferred 2026-08-19 (LIFECYCLE demotion
    shipped instead) — re-decide here, alongside the removed
    datastore-interfaces question's "re-add if desired" revisit.
  - Also demoted: common.NewDefaultRetryPolicy/IsRetryable/RetryableFunc
    (retry merged into pkg/common 2026-08-17, [0528] — demotion is now an
    unexport inside common; config Retry fields stay nil, WithDefaults
    fills them). The ConsumerType + CURSOR/LIFECYCLE + ConsumerConfig.Type
    demotion shipped 2026-08-19 (see the file-structure cleanup item).
  - Trim redundant pairs generally: e.g. DestroyTopic + DestroyTopicVersion
    can only confuse — consider one DestroyTopic with a version option.
  - Decide whether the field-less system config stub (RegisterSystem cfg /
    AlterSystem / `vulkan system alter`) stays in the v1 public surface or
    gets deleted until a real system-wide knob exists ([0516]).
- **Naming pass:**
  - Rename consumer waterline terms to something like cursor.committed —
    waterline is useful for understanding but should not dictate code naming.
  - Controllers have redundant verbage: topicController.GetTopic should just
    be Get.
  - Consider the Definition / Declarer / Provisioner / Instance split:
    `*Definition` Name(); `*Declarer` Declare(*Definition) error;
    `*Provisioner` Provision(*Definition) (*Instance, error); `*Instance`
    Run() error. Right now Definition and Provisioner are mixed, which
    doesn't hold together logically.
    - Settled preference (2026-08-16): the Run-side structs are named
      `*Instance`, not `*Execution` — Instance is the noun the mind
      reaches for, and it matches the worker_instance row the struct
      holds. `Execution` may survive only as the interface name the
      Instance structs implement.
  - Mechanical nits from the 2026-08-17 layered-pattern audit: receiver
    letters not matching the type initial (`i` on the *Execution types,
    `c` on *BaseDefinition, `f` on the definitions' provision methods);
    truncated/single-letter domain names (`sched`, `op`, `n`, `g`, `min`
    shadowing the builtin); withMetadata config methods living outside
    their *_config.go files; exported methods on unexported types
    (claimBuffer, rangeState, batchResponse, createAheadGate.Delete);
    cronscheduler's nine-column single-line SELECT.
- **Named-return-params house style** — decide and apply consistently across
  the reviewed surface (same sweep as the naming pass).
- **Error message consistency and obsession.** Standardize every error
  message to be understandable and actionable — enrich context, eventually
  add doc links. Consider a standard shape: Handler (where), Description
  (why/what), Action (how to resolve), Link (future docs).
  - Named/defined errors for users to errors.Is on — decide the taxonomy
    (coordinates with the circuit breaker's error_class enum).
- **Logging consistency and obsession.** Informative, actionable,
  filterable/queryable, consistently structured.
- **DefaultProducer / DefaultConsumer** for easier quickstarts, with comments
  and maybe a log statement recommending not to use in prod.
- **go.mod cleanup after factoring examples into a separate module** —
  blocked until examples are a separate module; either do that split now or
  decide the go.mod weight isn't worth it pre-v1 and drop this.
- **Comment conventions for public surfaces** — a standard: description,
  defaults, errors, doc links. Plus standardized SQL formatting.
- **Comment sweeps:**
  - fanOut (pkg/consumer/deliveryconsumer/controller/datastore/fanout.go) —
    both the Go comments and the ones inside snapshotSql/scanSql. SQL
    comments ship to Postgres, so every comment edit needs a live lab re-run
    (routing-lab is cheapest). (Verified 2026-08-13: pgx sends comments
    verbatim, but default QueryExecModeCacheStatement sends query text only
    at prepare time — once per connection per unique query — so the cost is
    observability noise, not network bytes.)
  - pkg/metrics; pkg/admin (health/metrics specifically);
    pkg/consumer/metrics (comments specifically).
  - The config-struct comment boilerplate ("pass your own *slog.Logger (own
    Handler)...", the Retry field comment, "Validate runs after
    WithDefaults...") is copied verbatim across ~40 config files — improving
    it must be ONE codebase-wide sweep so the files stay identical, never a
    per-package rewording. The "(own Handler)" fragment looks like a copy
    artifact to fix in that same sweep.

## Next

## Later

Pre-v1, after 14b — measurement, evaluation, and documentation; these want a
surface that has stopped moving.

- **Topic config as append-only declaration rows.** Design settled in [0519];
  build deferred so the config surface from [0518] settles first. topic keeps
  identity only (id, system_id, schema_version, created_at); a
  topic_declaration table holds name, partition_size and the four mutable
  config fields with declared_by/declared_at, newest row by MAX(id), full
  snapshots,
  appended only on change. Two details that carry the work: uniqueness of
  (name, schema_version) goes procedural, so renameTopic gains registerTopic's
  per-name advisory lock (both keys, sorted) and ErrTopicNameTaken comes from
  a check rather than 23505; and partition_size's immutability is enforced at
  append. Worker metadata takes the same shape afterwards.
- **binding_declaration retention** — the ledger is append-only ([0511]):
  waiting retries append attempt rows, so a long-blocked group grows the
  table without bound. Add time-based cleanup of superseded attempt rows
  (append-then-prune like message_log; off the register path, never touching
  the newest installed row or a declarer's newest waiting row).
- **Benchmark-recording pipeline** (14c) — decide where lab throughput
  numbers get saved so regressions are visible over time. First real
  workload: a thorough multi-topic throughput/latency benchmark under high
  concurrency, pushed to real DB limits (connection pool, lock table, I/O)
  rather than the library's own bottleneck. Single-topic skip-vs-claim was
  already measured in bench/idempotency/RESULTS.md; multi-topic contention
  is still open.
- **Idle-fleet worker-load benchmark** (14c; measure BEFORE building any
  fix). An idle deployment pays per worker row per poll: winner's claim
  UPDATE + no-op work each tick, and — the growing term — every replica's
  LOSING claim attempt (R replicas x W rows x 1/poll_rate no-op UPDATEs).
  Bench an idle fleet at 100 / 1k / 10k worker rows x 1-3 replicas:
  Postgres CPU, QPS, where the curve hurts. Result picks a rung on the
  settled fix ladder (cheapest first, don't skip rungs): (1) per-row
  poll_rate already exists in worker metadata — coarsen quiet topics' rows,
  document; (2) idle backoff inside the instance tick runner only — no
  progress backs off toward a cap (~10x poll_rate), any progress snaps
  back; cost is a committed-staleness spike on wake, janitor side covered
  by the producer's partition self-heal; (3) LISTEN/NOTIFY-woken workers —
  real complexity, only if (2) measurably fails. Prior: rung 1 carries to
  ~1k rows, rung 2 well past 10k, rung 3 never earns it.
- **Cross-version compatibility matrix** (14c) — producer/consumer built
  against release N-1 on a database migrated by N (what a rolling deploy
  produces; the empirical definition of which schema changes are BREAKING vs
  plain additive). The other direction (binary N, database N-1) should fail
  fast at Register's schema gate — that clean error is itself an assertion.
  Mechanics: a small test module whose go.mod pins the prior released
  version, run against a database migrated by HEAD.
- **TEST.md expand and refine** (14c) — the shutdown/interruption scenarios
  recorded there are Setup/Action/Assert prose from a scratch harness;
  implement as a real pkg/producer/pkg/consumer test suite once the API
  stops moving.
- **Compaction-key deadlock evaluation** — test compaction key with default
  produce and determine whether deadlock contention from reverse-ordered
  transactions is a real problem: at what (extreme or not) example does it
  truly hurt users, or does the system self-heal through retries. ProduceFunc
  is the escape hatch either way; this is about knowing.
- **System-table churn evaluation** — reconsider whether compaction_head
  should be per-topic compaction_head_(topic_id) (high update churn from
  many topics), and evaluate all system tables: cursor / lease / binding /
  topic / compaction_head.
- **Migration docs:** should use LOCK TIMEOUT for any ALTER migration
  commands (likely just needs documenting).
- **Append-only tables think-through** — making all tables append-only would
  be Cassandra-like, give audit/debuggability for all operations, and enable
  partition-based drops everywhere; trade-off is complexity and hot-path
  read throughput.
- **Documentation** (Phase 15, deliberately last):
  - Doc site: worked example of the transactional-outbox side-effect footgun
    (calling sendEmailConfirmation() before a Produce/multi-target closure is
    known to commit fires the email even if a later step rolls back) — pair
    with the outbox framing already on the site.
  - Doc comments on the public API surfaces 13/14b finalized.
  - Document the "consumerFunc hard timeout, goroutine abandoned" error
    (CallSafely in pkg/consumer/base/consumer.go): what it means, how to
    prevent it — handle ctx.Done() inside consumerFunc or raise
    TimeoutGrace; rare, but the abandoned goroutine is a real side effect.
  - Quick Start documentation — might require CLI work.
  - DDL table design diagram.
- **Circuit breaker implementation** (Phase 16, post-v1; two-tier design
  settled in Phase 13 — per-instance trip unit, quorum globalization,
  refund-on-close reconciliation; only questions explicitly left for
  implementation time reopen here):
  - error_class enum on delivery rows, recorded at exception time from the
    user's classification; values coordinate with 14b's named-errors
    taxonomy. The one schema touch, lands first.
  - Per-instance breaker: local streak tracking (N non-empty all-systemic
    ticks + M cumulative, exception retries counting), open state gating
    claims/retries/buffered work; state read from an atomic refreshed by its
    own async ticker, never a hot-path query.
  - Shared breaker row + globalization: (topic_id, group) row, guarded
    CLOSED->OPEN with a generation counter; settle quorum K here (small
    absolute vs presence-backed fraction — if fraction wins, presence
    heartbeat rows become a prerequisite; see parking lot).
  - Probe paths: local self-probe on cooldown; global prober elected via
    session-level advisory lock (self-releasing on crash); half-open exists
    only as OPEN + holding the lock; global close does not force local
    closes.
  - Reconciliation on close: probe winner refunds attempts AND reclaims
    systemic-classed rows (dead -> ready); the hand-back question for
    claimed-but-unattempted work resolves here (PartialCommit + refunded
    reclaim vs a new explicit release).
  - Config + observability: sparse opt-in MessageConsumerConfig fields (trip
    threshold N/M, cooldown, probe size, quorum); per-instance and group
    state metered + queryable; "instance open, group closed" surfaced as the
    bad-node signal; DLQ alerting able to report "breaker open, N dead rows
    pending reconciliation".
  - Breaker lab: dead-dependency (all trip -> global OPEN -> recovery ->
    zero wrongful DLQ), bad-node (one instance trips alone, group drains,
    its rows succeed elsewhere), flap (cooldown backoff + refund cycles
    converge, no poison-quarantine creep).
  - Real systems: Envoy outlier detection (tier 1), Resilience4j/Polly
    (classic in-process), Finagle failure accrual; the two-tier composition
    is per-host ejection + cluster-wide panic thresholds.

## Parking lot

Post-v1, unordered. Pick up only if a real workload demands it. Known
dependencies: pgx-vs-database/sql should weigh LISTEN/NOTIFY's outcome if
both are in play; presence heartbeat rows are the circuit breaker's
prerequisite if quorum-as-a-fraction wins.

- **FIFO partitions** (Phase 12) — ordering on demand, paid only where opted
  in. `partition_key` on message rows (nullable = no ordering; a second key
  beside compaction_key on purpose: compaction_key is a read-time "what's
  current" filter, partition_key a claim-time "don't run two at once" gate).
  The bare claim-from-log path is unordered under concurrent workers, so
  FIFO is an opt-in on the lifecycle path: keyed claim skips rows whose key
  already has an in-flight delivery in the group (null key = full
  concurrency); a single cursor reader in id order is the trivial K=1 case.
  Keyed lanes at the dispatch point: same partition_key -> same lane, each
  lane sequential — claimBuffer.WaitForNext (pkg/consumer/claimbuffer.go) is
  the single dequeue point every dispatched message passes through, so a
  lane-routing policy slots in without touching prefetch/Add/resolve. Order
  through a retry is the subtlety: only the lowest unresolved offset of a
  key is eligible, so a backed-off head blocks its later offsets (and a dead
  head stops blocking). Also the principled fix for the Phase 3.5 claim
  hotspot — sharding the claim by key spreads workers across index ranges.
  Hot key serializes to single-worker throughput by design. Real systems:
  Kafka partitions, Pulsar Key_Shared, SQS FIFO MessageGroupId.
- **User-initiated defer & policy-driven dispatch** (12b) — consumers control
  neither WHEN a message comes back nor WHICH pending message runs next.
  - The feature: consumerFunc says "can't process NOW, retry at T" without it
    counting as a failure (downstream rate limit, keyed dependency outage —
    the circuit breaker hands the dead-tenant case here — out-of-order
    business state, off-peak scheduling). Open shapes: sentinel error the
    library recognizes (least churn, composes with named-errors taxonomy) vs
    richer return; substrate = exception window's can_run_after (exists
    today) vs the ordered-index buffer; does a defer consume an attempt, does
    it write a delivery_log row. A defer-only need does not justify building
    the orderer.
  - The mechanism sketch (async ordered-index claim table, for whenever the
    lifecycle path revives): deliveries stays the durable unordered backlog;
    an orderer async top-ups a SMALL ordered ready-buffer per user policy
    (priority, delay, load shedding); claims pop the buffer head. Ordering is
    WINDOW-APPROXIMATE, not global (Sidekiq/Celery precedent — document it);
    deep-backlog strict priority is restored with low-cardinality priority
    TIERS (one id-ordered orderer cursor per tier, merged by weight); the
    buffer stays only slightly ahead of claims (bounded depth; it's DERIVED
    state — resume story is truncate-and-re-score); the orderer is another
    fenced scanner with a mark, same machinery as fanOut. Open: separate
    buffer table vs nullable position column (position updates lose HOT);
    where policy inputs live (delivery-row columns vs the headers JSONB that
    header routing would add).
  - Could redo both concurrency-deferral and exception claiming on this
    table: retries land in the unordered backlog, materialize near the front
    soon after; a concurrent defer waits to enter the ordered index until
    the compaction key frees.
  - Overload policies to mine from the Uber resilient-DB talk
    (https://www.youtube.com/watch?v=g7FmEc5GLWs&t=387s): FIFO->LIFO as a
    load-shedding gauge (lag growing -> skip older work until caught up);
    priority tiers 0-5 doing double duty for shedding; producer-side
    backpressure at enqueue is the unexplored half.
  - A revived lifecycle path must also wire the exclusive-consumption key
    gate (consumerBase.claimKeyedRun) — DeliveryConsumer predates it and
    would run keyed Defer messages ungated.
  - Real systems: SQS DelaySeconds/ChangeMessageVisibility; Pulsar
    reconsumeLater + delayed-delivery tracker (exactly the derived
    ready-buffer shape); Sidekiq weighted queues / Celery priorities.
- **Shard the hot lane** (6.5d) — K lanes per group, each owning a frozen
  contiguous block of the log, draining independently; only if a single
  group's frontier is provably contended. Frozen + contiguous means no
  overlap and no seam: lane s owns (H*s/K, H*(s+1)/K], claims cap at the
  lane's block_hi. The exception term in Advance must be lane-scoped or one
  lane's stuck exception freezes every lane. The group's contiguous
  watermark is the committed of the FIRST lane not yet at its block_hi, not
  min(committed) (which sticks once lane 0 finishes). Striping by
  offset % K is wrong — a dense single-integer cursor can't represent it.
- **Header/content routing** (7b) — routing_key matching is
  positional/hierarchical; some routing is about an unordered attribute set
  ("region=eu AND tier=gold"). Add `headers jsonb not null default '{}'`;
  binding grows a discriminator (kind column) once there are two matcher
  shapes; header matcher is `headers @> '{...}'` containment with a GIN
  index; the same JSONB is the candidate substrate for 12b's
  delays/tiers/shedding if those want to be header-driven. Foot-gun: an
  empty {} match is @>-true for EVERY event — reject at bind time. Also
  parked here: a NATS-style selector for pattern bindings — today `*`
  matches any run including dots and can't pin an exact depth; NATS splits
  `*` (exactly one token) from `>` (trailing tokens). Real systems: RabbitMQ
  header exchanges (x-match all/any).
- **LISTEN/NOTIFY latency** (8d) — producers wake idle workers instead of
  waiting for the poll tick. NOTIFY is fire-and-forget (lost if no listener
  or during reconnect), so the fallback poll stays underneath — it also
  covers delayed (run_at) messages. Same pattern River/Oban use. Revisit
  only if poll-interval latency is a measured problem.
- **Lease heartbeat/renewal** (9b) — for jobs whose legitimate runtime
  exceeds WorkTimeout but still want fast crash reclaim: an opt-in
  heartbeat()/touch() handle passed to consumerFunc; the lease extends only
  when touched (`UPDATE ... SET lease_until = now()+ext WHERE id=$1 AND
  lease_token=$2`); RowsAffected==0 on renew means already reclaimed ->
  cancel the work context (the row is another worker's now — never retry the
  renew). Settled gotchas: renewal is PROGRESS-based (Temporal
  activity-heartbeat style), never an unconditional background ticker — only
  the user can tell slow-but-progressing from hung; interval ~ window/3
  survives ~2 missed beats; the extension must cover the ack, not just
  processing; keep a hard max-duration ceiling so a hung-but-touching loop
  still caps out; in-process the library can only stop renewing, never kill
  the goroutine. Prerequisites (Phase 13 boundary settle, debug readout)
  are satisfied — pick up on merit when a real long-running workload shows.
- **pgx vs database/sql** (11b) — decide whether dropping pgx for
  database/sql is worth losing native types, COPY, pgx.Batch pipelining, and
  LISTEN/NOTIFY; pgx.Tx threads through every producerFunc closure and
  pgtype.UUID sits in public structs, so the swap means re-deriving all of
  it for portability nobody asked for. Inventory the dependencies, weigh 8d
  first if it's in play, write the decision even if it stays "keep pgx".
  River and Oban both commit to pgx for the same reasons.
- **Dynamic partition bounds** (11.5b; shape settled 2026-07-24 — unlocks
  the immutable PartitionSize). Today every partition-math call site assumes
  one constant width for the topic's life. The fix: Postgres already stores
  every partition's true bounds — the math is just a cache of the catalog.
  KEEP sequential `message_log_<id>_<n>` naming; reads walk pg_inherits +
  pg_get_expr(relpartbound) and use the (relname, lower, upper) triples;
  creation mints n = max suffix + 1 and from = max upper bound off ONE
  catalog read (CREATE TABLE IF NOT EXISTS keeps the concurrent-create race
  benign — racers on the same snapshot compute identical name+bounds);
  contiguity + non-overlap survive any size history because new partitions
  only append at the top; cache the partition map in memory, re-read only
  when head crosses the cached max upper bound. Resulting semantics:
  PartitionSize = width of FUTURE partitions only (Kafka segment.bytes) —
  a freely-alterable topic-row UPDATE via AlterConfig's sparse-patch
  machinery.
- **Consumer lifecycle extension point** (13b) — decide whether the
  startup -> poll -> shutdown sequence becomes an overridable public
  Lifecycle struct or stays internal. Deferring was itself the v1 decision:
  internal for now; publishing hook points freezes the poll loop's internal
  ordering into API. Pick up only on a real embedding need (external poll
  trigger, custom scheduler, hand-driven test harness). sarama's
  ConsumerGroupHandler vs River's internal loop are the two defensible
  answers.
- **RLS & chaos-testing surfaces** (13c) — both additive post-v1. RLS: most
  likely a topic.Config toggle provisioning Postgres RLS policies + a
  least-privilege role so a compromised consumer credential can't reach
  outside its topic; decide the config field, which tables carry policies,
  role-to-topic mapping, RegisterTopic-rides vs separate admin verb.
  Chaos/fixture: internal seed/inject helpers first (seed ready/inflight/
  dead, inject failures), then decide public testing package vs
  internal-only (River's rivertest precedent).
- **Presence: heartbeat rows for live producer/consumer instances** (13d;
  design shaped in discussion, not built; prerequisite for the circuit
  breaker's quorum-as-a-fraction). Nothing records what's connected to a
  topic — operators can't answer "what exists right now, idle or active",
  and Destroy finds out the hard way (a live producer's missing-partition
  self-heal resurrects partitions mid-drain).
  - One presence row per instance, three timestamps, two mechanisms:
    registered_at written once at Register (what EXISTS); last_heartbeat
    bumped by a lifetime heartbeat goroutine (what's ALIVE — crashed process
    leaves a stale row for a TTL sweep); last_produced_at/last_consumed_at
    (what's ACTIVE) bump an in-memory atomic on the hot path, flushed on the
    heartbeat tick — zero hot-path writes. Activity-only heartbeats rejected
    (collapse "nothing registered" and "registered but idle"); piggybacking
    on the Consume loop rejected (breaks symmetry, misses janitor-only
    instances).
  - Register(ctx) inserts the row, validates the topic's PARENT tables via
    to_regclass (parents only — partitions come and go by design), starts
    the heartbeat. The shipped three-state gate keeps presence honest:
    producing implies alive becomes an invariant.
  - First consumer — a Destroy gate: refuse while any producer is ALIVE (not
    merely active; idle-but-alive can wake mid-drain), refusal naming
    instances and last-seen times; force override; deleteTopic's bounded
    drop loop stays the hard backstop (check-then-drain has an unavoidable
    TOCTOU window). RabbitMQ queue.delete(if-unused) precedent.
  - Second consumer — the breaker's globalization quorum as a fraction of
    ALIVE instances. Also the natural substrate for alerts that name
    instances ("destroy blocked: producer X seen 2s ago").
  - Third consumer — automatic consumer-group expiration
    (topic.Config.GroupExpiration): janitor-style reap of a group idle past
    the threshold, deleting the same rows as the manual destroy verb.
    Mechanism settled 2026-07-29: idleness computed DYNAMICALLY —
    now() - GREATEST(newest heartbeat, group registered_at) — never a
    recorded became-empty transition (missable on crash; derived form is
    idempotent). Idleness keys on MEMBERSHIP (heartbeats), never activity
    timestamps — Kafka (KAFKA-4682) and Pulsar (#17573) both shipped GC
    that deleted state under live-but-quiet groups and fixed it by anchoring
    to membership. Prerequisites: newest heartbeat must SURVIVE instance
    departure (retain last row per group, or roll last_seen onto the
    consumer_group registry row); never-consumed groups floor at
    registration time. Default open question: retention-anchored
    max(RetentionTTL, 7d) vs industry-standard OFF (Pulsar/NATS opt-in;
    Kafka the lone always-on at 7d) — re-settle at build; needs an explicit
    never sentinel (0-as-unset vs 0-as-never collide). Retention-forever
    topics never expire groups. Expiry is recoverable by design: a returning
    group re-seeds and REPLAYS what retention holds — duplicate work, not
    data loss. Stakes: with allow_drop_past_committed=false (default) an
    abandoned group's cursor pins partition drops. Hard rules: expiry must
    telegraph (idle/expiring visible in worker snapshots/state gauges
    BEFORE the reaper), and unresolved delivery rows refuse-or-alert, never
    silent drop.
  - Real systems: RabbitMQ if-unused; Kafka group membership
    (session.timeout.ms as the TTL sweep); Temporal worker pollers view.
- **Post-v1 research backlog** (14d):
  - Contribute a MIN_ACTIVE_ROWVERSION-style primitive upstream to Postgres
    (watch-and-propose only): SQL Server's MIN_ACTIVE_ROWVERSION() is a
    cheap first-class read of the low-water mark across in-flight
    transactions — exactly what the snapshot fence
    (pending_head/pending_xmax cursor columns) answers by hand. A core
    primitive would let claim fences poll a system value instead of carrying
    tracking columns.
  - Read for hot-path ideas once the API stops moving:
    https://packagemain.tech/p/golang-optimizations-for-highvolume — mine
    for the actual hot paths (claim, produce, janitor loops), don't apply
    speculatively.
  - worker_log failure-evidence table (design settled under the old
    maintenance-tier names; rides the worker backoff's fenced failure
    UPDATE): one SHARED append-only table, failed worker runs only —
    `worker_log (id BIGSERIAL PK, worker, topic_id, consumer_group, error
    TEXT, attempts INT, created_at)`; NO success/recovery rows (absence IS
    success). The write rides the backoff UPDATE's fence as one
    data-modifying CTE, so an instance that lost its claim mid-run can't
    write noise. Retention: the janitor sweeps rows past ~7d in its
    existing pass. Surfacing: worker snapshots join the latest log row per
    failing worker.
- **Claim-fence transaction-xmax logic -> its own async ticker** (really
  want) — abstract the fence read into an async ticker with claimers
  reading a shared in-memory value; the query is cheap so the poll rate can
  be much faster, and the complex logic gets one home.
- **Exception claiming revamp onto topic/cursor machinery** (really want) —
  exception claiming is queue-based today and carries a lot of custom logic.
  Converting it needs the async ordered-index claim table (see 12b): a
  failed retry is produced to an unordered topic, materialized as a new
  attempt near the front of the ordered index, picked up soon after because
  materialization runs only slightly ahead of claiming.
- **Delivery rows delete on completion** instead of persisting as 'done' —
  the delivery table's irreducible job is a dispatch index over pending
  messages, not a completion record; deleting on success makes storage
  O(pending window) instead of O(history). Composes with success-by-absence
  audit: "was message X processed by group Y" = id <= the group's frontier
  AND no dead row AND no open exception, with delivery_log supplying the
  attempt history — per-message audit with zero happy-path writes. Caveats:
  no success timestamp to report; audit horizon = retention TTL.
- **Page-bitmap completion tracking** as a someday replacement for range
  leases on the cursor path: a page row per N ids above the committed
  cursor holding a done-bitmap gives bit-granular crash recovery (reclaim
  redelivers only unset bits), updates batched one page-row UPDATE per
  commit — Pulsar's ack-hole structure. Deliberately NOT a route to custom
  dispatch order (bitmaps compress "who's done"; priority/delay/fairness
  need "who's next", one index entry per pending message regardless).
  Only worth building if range-granular crash redelivery shows up as a real
  cost — it's rare-path today.
- **json.Number sweep for jsonb-through-Go paths** — any jsonb that
  round-trips through a Go `map[string]any` (insertWorker's metadata merge is
  the known case) decodes numbers as float64, silently corrupting integers
  above 2^53 (~104 days in nanoseconds) on write-back. Fix is decoding with
  `json.Decoder.UseNumber()` (scan the jsonb as bytes, decode both maps
  ourselves); the merge itself never touches values, so digit-strings pass
  through lossless. Do it as one audited sweep of every such path, not a spot
  fix — pre-v1 the realistic values sit far below the threshold.
- **BRIN indexes** — look into using them for different tables.
- **DeadLetterTopic consumer** — consume on events to the DLQ.
- **Shadow/Mirror functionality** — watch exactly the same cursor as another
  group (message-by-message mirroring would be better if possible; probably
  not).
- **Binary/compressed message storage** (maybe as an option) — prevents easy
  field search in the DB but could mean smaller network requests and faster
  unmarshalling, protocol-buffers style. https://github.com/Apaezmx/pgproto
- **Debezium-like generalization** — how could the system watch a generic
  table and stream its data elsewhere.
- **Standardized HTTP API design** — producing is a POST with batching first
  class; consuming via SSE/websockets needs a cursor-advance
  acknowledgement mechanism, or a plain GET/QUERY as the simple
  alternative. https://github.com/durable-streams/durable-streams for a
  potential protocol.
- **WorkerManager split into WorkerScheduler + WorkerSpawner** — scheduler
  acts like the cron scheduler but submits 'spawn'/'destroy' topic requests
  (reconciler logic); spawner reads the topic and spawns or destroys
  instances.
- **Antithesis** (https://antithesis.com) for production hardening and bug
  hunting.
