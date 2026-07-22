consider using database/sql from stdlib to remove pgx dependency (might be a bad idea)
better abstract out the datastore from the consumer. ie consumer should not know or care about the internals of the datastore
Process's loop (CURSOR/LIFECYCLE both) still exits entirely the moment CursorClaim/LifecycleClaim returns any error. A blip that clears within pkg/retry's MaxRetries is now correctly absorbed (Wrap returns nil, Process never sees it -- fixed as part of Phase 9's fault-isolation-lab). The open question ("should Process retry/backoff at the poll-loop level, or is 'the caller of Consume restarts the process' the intended boundary") is RESOLVED: keep the per-tick-fatal design as-is -- an unbounded loop-level retry would mask a genuinely permanent error forever (crash is both a signal and a chance to fix things on restart), and it'd make Process a special case out of step with RollWaterline/DrainExceptions/Project/janitor, which all already share this identical give-up-and-exit shape via DatastoreRetry's bounded MaxRetries budget.
  What WAS a real gap: a connection killed mid-query (SQLSTATE 57P01/57P02/57P03 -- admin/crash shutdown, cannot-connect-now) was misclassified PERMANENT (zero retries) despite the outcome being genuinely ambiguous, not deterministic -- unlike a syntax error or constraint violation, "did that commit land before the connection died" deserves the SAME bounded retry budget every other transient blip already gets. Fixed: pkg/retry.IsTransientPgError now classifies those three codes retryable alongside 40P01, after auditing every DatastoreRetry.Wrap call site (consumer/producer/topic/metrics, ~21 sites) for retry-safety-under-ambiguous-commit -- every write self-consumes its own re-entry (a token/status guard a retry can't re-match) or carries an idempotency key with ON CONFLICT DO NOTHING, so a retry of an attempt that actually landed is a no-op. One pre-existing gap surfaced and fixed alongside it: consumer's dropPartition did a bare `DROP TABLE` (no IF EXISTS), unlike every other DDL call site in the codebase -- now `DROP TABLE IF EXISTS`. Bonus: this also fixes producer's classifyBatchFailure, which could wrongly evict an innocent caller from a batch on a mid-read connection kill (the existing IsRetryable-first guard, added for 40P01, now covers this class too for free). Verified live: 8/8 repeat pg_terminate_backend-mid-consume runs recovered transparently (previously ~50% died outright) + a unit-level classification check for all three codes + a dropPartition double-DROP-IF-EXISTS check.
  Follow-up: audited the REST of Postgres's SQLSTATE space for the same "ambiguous outcome, every write-path already proven safe under retry" shape and added 10 more codes to IsTransientPgError: 40001 (serialization_failure, same guaranteed-full-rollback story as 40P01 -- currently dead code since nothing uses SERIALIZABLE isolation, kept for correctness if that ever changes), 08000/08001/08003/08006/08007/40003 (connection never established or died mid-request, or Postgres explicitly says "don't know if it committed"), 57P05 (idle_session_timeout, same operator/server-lifecycle vein as 57P01-03), 53300 (too_many_connections -- never got a connection at all, so zero ambiguity), and 57014 (query_canceled, but ONLY safe because it's already filtered to exclude our OWN ctx-driven cancellation via the existing context.Canceled guard -- an externally cancelled statement aborts cleanly). Explicitly did NOT add: 08004 and 08P01 (can mean permanent misconfiguration -- bad credentials, protocol mismatch -- not just transient load), 40002 (deterministic constraint violation), the 53xxx/58xxx resource-exhaustion and I/O codes (retrying those masks a real operational emergency instead of surfacing it -- the same "crash is a signal" reasoning that kept Process's loop per-tick-fatal), the XX class (corruption -- retrying is actively dangerous), and 25P02 (already correctly handled as post-failure noise by the batch resolver's own poison-eviction logic -- adding it here would double-handle or mask the real error). Verified with a 32-case classification table (all 10 additions + all exclusions, including 25P02) plus a full rebuild/vet/faultisolationlab pass.

lease heartbeat / renewal (LONG TERM, low priority - narrow edge case)
  edge case: long-running jobs whose runtime exceeds WorkTimeout but still want fast reclaim on a real crash. today such a job is either falsely reclaimed mid-flight (double-processed) or forces a huge WorkTimeout (slow crash recovery). a heartbeat decouples the reclaim timeout from job duration - the lease tracks "worker still alive" instead of a one-shot duration guess.
  decision: PROGRESS-BASED renewal (temporal activity-heartbeat style), NOT an unconditional background ticker. the lib fundamentally can't tell "slow but progressing" from "hung" for an opaque user consumerFunc - only the user can. so this is a user concern: we can't force consumerFunc to respect ctx or make progress, and a hung goroutine can't be killed in-process (go has no goroutine kill - only process/sandbox isolation could).
  mechanism sketch: hand consumerFunc a heartbeat()/touch() handle. framework extends the lease only when touched -> `UPDATE ... SET lease_until = now()+ext WHERE id=$1 AND lease_token=$2`. opt-in: default short jobs ignore it and rely on the fixed lease window. touches stop -> lease lapses -> normal reclaim.
  gotchas to remember when building:
    - RowsAffected==0 on a renew = lease already reclaimed -> cancel workCtx so a cooperative func stops; the row is another worker's now.
    - timing invariant: heartbeat interval must be comfortably < lease window so one missed beat (gc pause, db blip, renew round-trip, worker-vs-db clock drift) doesn't falsely reclaim a healthy worker. window is bounded above by acceptable crash-recovery latency. (interval ~ window/3 survives ~2 misses.)
    - the extension must cover the ack (RecordSuccess/Failure), not just processing - don't reopen the window at the finish line.
    - still keep a hard max-duration ceiling as a backstop: a job that hangs WHILE still touching (buggy progress loop) must eventually be capped -> lapse -> reclaim -> dead via attempts.
    - in-process we can only bound queue damage (stop renewing -> reclaim -> dead-letter), not kill the hung goroutine; accept the leak until process restart.
  depends on lease_token + lease_until (done); pairs with the existing workCtx (WithoutCancel+WorkTimeout) and attempts/dead-letter machinery.

review / refine the comments in fanOut (pkg/consumer/datastore.go) -- both the Go
comments and the ones inside snapshotSql/scanSql. remember SQL comments ship to
Postgres, so every comment edit needs a live lab re-run (routing-lab is the cheapest).

delivery rows should delete on completion instead of persisting as 'done' (from the
LIFECYCLE-vs-CURSOR review that led to parking the lifecycle path): the delivery
table's irreducible job is a DISPATCH INDEX over pending messages -- "who's next by
an arbitrary key" -- not a completion record. deleting on success makes its storage
O(pending window) instead of O(history). composes with the success-by-absence audit
idea from the same review: an API answering "was message X processed by group Y" as
id <= the group's frontier AND no dead row AND no open exception = succeeded, with
delivery_log's failure rows supplying the attempt history for bumpy successes -- per-
message audit with zero happy-path writes. write cost per delivery stays either way
(insert at fanout, delete at done -- index maintenance is irreducible); only storage
improves. known caveats: no success timestamp exists to report, and the audit horizon
is the retention TTL.

intra-batch concurrency for CursorClaim: one worker's claimed range processes
sequentially today, so a single slow (not failing) message queues the rest of the
batch behind it -- worker latency is sum(all) instead of max(slowest). note the
counter-argument before building: parallelism ACROSS ranges already exists (N
workers claim disjoint ranges; claimed advances at claim time), so "run more
workers" already scales throughput -- this only matters when a single process
wants concurrency without more claim loops. FEATURE ONLY -- no impl decided.
things any impl must answer (all open): partial-commit assumes a contiguous
lastProcessed prefix, concurrent completion is non-contiguous, so mid-batch
shutdown needs a new answer; in-order processing is CURSOR's pitch, so this is
opt-in at minimum, and per-key ordering wants key lanes (see
reference/waterline's lanes) not a free-for-all pool; Queue/PoolLimiter on
MessageConsumer are vestigial (CursorClaim never touches them) and look like the
half-built home for exactly this -- the real work item is finish or delete that
architecture, not bolt on a second one.

user-initiated defer: a way for consumerFunc to say "can't process this NOW, retry
me at T" without it counting as a failure. today returning an error is the only
tool -- it burns an attempt, records a failure that isn't one, and retries on the
failure backoff curve instead of when the consumer actually wants it back. use
cases: downstream rate limit ("retry in 60s"), a known-dead dependency (the
circuit-breaker entry's outage scenario currently burns maxAttempts and
dead-letters work that would have succeeded), out-of-order business state (the
shipped event arrives before the payment row exists), deliberate off-peak
scheduling. FEATURE ONLY -- no impl decided. candidate shapes to evaluate when
picked up (all open): a sentinel error the library recognizes (least API churn,
composes with the named-errors entry) vs a richer consumerFunc return; park into
the existing exception window with can_run_after vs feed the async ordered-index
idea below; whether a defer consumes an attempt (it's not a failure, but
uncapped defers can loop forever) and whether it writes a delivery_log row (no
error happened).

async ordered-index claim table for LIFECYCLE (design sketch for whenever the
lifecycle path revives as the non-FIFO substrate; from the same review): two-stage
dispatch -- deliveries stays the durable unordered backlog, an orderer process
async top-ups a SMALL ordered ready-buffer per user policy (priority, delay, load
shedding), and claims just pop the buffer head. what it buys: claims stop sorting
over pending entirely, and because ordering is async the policy can be arbitrary
Go (tenant budgets, load-shed gauges), not just indexable SQL expressions. key
decisions already made in discussion:
  - ordering is WINDOW-APPROXIMATE, not global: the orderer scores a sliding
    window of the backlog and accepts inversions beyond it. precedent: sidekiq
    priorities are probabilistic queue-weights, celery is best-effort -- nobody
    promises global priority order. document it.
  - deep-backlog strict priority is the one thing window-ordering breaks (an
    urgent message behind 100k backlog only jumps the window) -- fix with
    low-cardinality priority TIERS on delivery rows: one id-ordered orderer
    cursor per tier, merged by weight. exact tier semantics, no global sort.
  - the buffer must stay only slightly ahead of claims (bounded depth). eager
    ordering-on-arrival freezes stale decisions across a consumer outage; and
    since the buffer is DERIVED state (rebuildable from deliveries), the resume
    story is truncate-and-re-score, which bounded depth keeps cheap.
  - the orderer is just another fenced scanner with a mark ("ordered through
    message_id X") -- same marked-scan machinery fanOut uses, state can sit on
    the same cursor row.
  - open: separate buffer table vs a nullable position column + partial index
    on deliveries (fewer tables, but position updates lose HOT).

page-bitmap completion tracking as a someday replacement for range leases on the
CURSOR path (same review): a page row per N ids above the waterline holding a done-
bitmap gives bit-granular crash recovery -- a reclaim redelivers only unset bits
instead of the whole range -- with updates batched one page-row UPDATE per commit.
this is Pulsar's ack-hole structure. deliberately NOT a route to custom dispatch
order: bitmaps compress "who's done", but priority/delay/fairness need "who's next
by a per-message key", which takes one index entry per pending message no matter the
encoding (that's the delivery table's job). only worth building if range-granular
crash redelivery ever shows up as a real cost -- it's rare-path today (crashes only;
failures partial-commit and move on).

add a proper NATS-style topic selector for routing bindings (LATER, low priority)
  today `binding.pattern` is a true wildcard: `*` matches any run of characters
  including dots, so it can span any number of hierarchy levels (`orders.*.central1`
  matches `orders.us.central1` AND `orders.us.high.central1`). simple to implement
  and reason about, but it can't pin an exact depth -- there's no way to say "match
  this one segment, not deeper nesting" (e.g. distinguish a region-level event from
  a datacenter-level sub-event at the same position). a NATS-style selector fixes
  that by splitting `*` (exactly one dot-delimited token) from `>` (one-or-more
  trailing tokens, tail-only) -- see reference/waterline/routing.go's natsToRegex
  for the translation this would follow. revisit only if bindings actually need
  that precision.

consider an index on deliveries.status if claim scans ever show up hot
  deliveries (Phase 8b) stays one shared table across every topic/group -- unlike
  message_log it doesn't need per-topic physical separation, since rows are
  ephemeral (deleted/resolved continuously, no retention-drop mechanism) and
  aren't keyed by a shared sequence. today it has only the PK (consumer_group,
  topic_id, message_id); ClaimMessagesWithLifecycle/ClaimExceptions filter further
  by status, which isn't indexed, so they scan every row under a (group, topic)
  key to find the ones that match.
  deliberately NOT adding a status index preemptively: status is the single most
  frequently written column in the table -- every recording function (Record*,
  ClaimExceptions' kill/claim, Commit's park) sets it, so it's touched on nearly
  every write. because none of today's UPDATEs touch an indexed column (PK
  columns never change), those writes likely already get Postgres's HOT
  (heap-only-tuple) fast path -- no index maintenance at all. adding a status
  index would end that for every single state transition, on every topic, to
  speed up a read that's only expensive in an already-contained case: a badly
  lagging group/topic piling up rows under one key, which MaxAttempts/backoff/
  dead-lettering already bound.
  revisit with real evidence (pg_stat_user_tables / EXPLAIN ANALYZE on a lagging
  group) rather than speculatively. if it's needed, prefer a PARTIAL index
  (WHERE status IN ('ready', 'inflight')) over a full one -- terminal done/dead
  rows drop out of upkeep instead of bloating the index forever.

  revisited during the delivery_log design discussion (Phase 11's attempt
  audit trail): the "stays sparse, already bound by MaxAttempts" assumption
  above holds for the AVERAGE case but not a correlated failure burst. Worked
  through concretely at 30k-500k msg/s: a single downstream dependency (e.g.
  AWS) going down for an hour pushes close to 100% of that window's traffic
  into deliveries at once -- ~100M+ rows for one topic, comparable to
  message_log's own designed-for volume, not the trickle this entry assumed.
  because deliveries is shared, that one topic's vacuum/bloat/buffer-cache
  pressure degrades every OTHER topic's exception-window queries too -- the
  exact blast-radius problem 8b already fixed for message_log/cursor, just
  never revisited here. conclusion: deliveries needs the same per-topic
  physical split message_log got in 8b. the same per-topic requirement
  applies to the new delivery_log audit table this discussion is designing
  -- see LEARNING_PLAN.md's Phase 11 "Attempt audit trail" bullet once it's
  updated with the finalized design.

  the status/can_run_after index idea above was reconsidered once more,
  further into the same discussion, and dropped rather than made per-topic
  opt-in as first proposed -- see the "circuit breaker for a known-dead
  downstream dependency" TODO entry's own addendum below for why a faster
  exception-claim scan is actively counterproductive during exactly the
  burst scenario that motivated it. this entry's original "revisit with
  real evidence, don't add speculatively" verdict stands.

v1 admin surface: pkg/admin owns topic lifecycle (DECIDED in discussion, not built)
  why the free funcs lose: topic.Exists/Register/Destroy run on the app's own
  connection, so least-privilege is impossible in principle -- Register does
  DDL (CREATE TABLE message_log_<id>), so every service registering on
  startup needs a role that can CREATE and therefore DROP. topic config also
  has no single owner (upsert is DO NOTHING + ErrTopicConfigMismatch:
  changing retention means lockstep redeploys of every registrant, and no
  Alter exists at all). and Destroy is one unguarded call on the same object
  every service already holds. every surviving system splits control plane
  from data plane AND backs the API split with a permission split (Kafka
  AdminClient + delete.topic.enable, Pub/Sub admin clients + IAM, RabbitMQ
  configure/write/read permissions, River's rivermigrate).
  decided shape:
    - pkg/admin is the ONLY write path for topic lifecycle: admin.New(ds,
      &admin.Config{}) with Register, Alter, Exists, Destroy, Migrate (and
      likely List, CLI-driven). the pkg/topic free funcs go away. the
      admin's ds is where privileged credentials live in prod; app services
      never construct one -- the object is the place the privilege split
      attaches to.
    - Destroy layered: admin.Config.AllowDestroy (default false ->
      ErrDestroyDisabled) AND DestroyOptions{Force} required while the topic
      still holds messages (ErrTopicNotEmpty). the existing drain refusal
      stays underneath both. Register stays frictionless -- create is
      recoverable, destroy is not.
    - no topic.Lookup: producers/consumers take the topic NAME string in
      their constructors (pure wiring, no I/O, no ctx) and lifecycle
      Register(ctx) resolves name -> *topic.Topic (ErrTopicNotFound fails
      fast at startup; a wait-for-deploy-ordering caller wraps Register).
      folds into the same Register(ctx) the Presence entry below already
      grows. *topic.Topic becomes an admin-return/informational type.
      gotcha to doc on Destroy: destroy+recreate mints a new topic id, so a
      long-running process holds a stale resolved topic.
    - cmd/vulkan CLI (repo's first binary) wraps pkg/admin thinly: vulkan
      migrate, vulkan topic register/alter/exists/destroy/list. the CLI sets
      AllowDestroy internally -- an interactive type-the-name confirmation
      replaces the config gate (--yes/--force for CI). conn via
      DATABASE_URL-style env, which is where admin credentials live in CI.
  deferred past v1: role creation / least-privilege bootstrap -- users can
  GRANT themselves, and nothing in this shape blocks a later EnsureRoles
  (default privileges cover dynamically created per-topic tables). wrinkle
  recorded for then: the janitor does runtime partition DDL, which needs
  table ownership -- role membership in the admin role or SECURITY DEFINER
  functions are the candidate answers.
  still needing their own design passes: Alter (currently an impossible
  operation) and Migrate (embedded base-schema migrations, River-style).

Presence: heartbeat rows for live producer/consumer instances
  problem: nothing records what's connected to a topic. Operators can't answer
  "what producers/consumers exist right now, and are they idle or active?"
  without inferring it from message traffic, and Destroy can't tell whether
  it's tearing down a topic something is still writing to -- it currently
  finds out the hard way (a live producer's missing-partition self-heal
  resurrects partitions mid-drain; deleteTopic's drop loop bounds its passes
  and errors, but the error can only guess "a producer is likely still
  writing", it can't name one).
  design (shape agreed in discussion, not built): one presence row per
  instance carrying three timestamps, two mechanisms:
    - registered_at, written once at construction -- what EXISTS.
    - last_heartbeat, bumped by a lifetime heartbeat goroutine -- what's
      ALIVE. A crashed process leaves a stale row for a TTL sweep, same as
      any lease.
    - last_produced_at (/ last_consumed_at) -- what's ACTIVE. Must not cost a
      write on the hot path: produce bumps an in-memory atomic timestamp and
      the heartbeat tick flushes it alongside last_heartbeat, so activity
      resolution is bounded by the heartbeat interval and the message path
      stays untouched.
  activity-only heartbeats (batcher-style, spawn on produce / exit at idle)
  were considered and rejected: they collapse "nothing registered" and
  "registered but idle" into one state, and the registered-idle producer that
  is about to wake is exactly what an operator wants visible.
  where the lifecycle lives (agreed in discussion): MessageProducer gets a
  Register(ctx) matching MessageConsumer's -- constructors stay pure, and
  Register is where an instance announces itself: insert the presence row,
  validate the topic's PARENT tables exist (message_log_<id>, delivery_<id>,
  idempotency_key_<id> via to_regclass -- parents only, partitions come and
  go by design and are self-heal/janitor property), and start the heartbeat
  goroutine, which the name already implies. NewMessageProducer(ctx, ...)
  ctx-in-constructor was the earlier idea; Register(ctx) supersedes it (no
  constructor signature change, and producer/consumer become symmetric).
  Produce/Consume then check registration before doing work -- an atomic
  flag, one load, free on the hot path -- with THREE states, not two:
  not-registered errors "call Register first"; registered works; and
  wound-down (Register's ctx cancelled -> heartbeat goroutine exits ->
  flips the flag closed) errors too. The third state is what keeps presence
  honest: without it a producer whose heartbeat died keeps producing while
  its row goes stale -- actively-writing-but-looks-dead is exactly the
  false-dead case that would wave the Destroy gate through into the
  livelock. Producing implies alive becomes an invariant, not a hope.
  known semantic change to make LOUDLY: consumer Register's ctx today is
  call-scoped one-shot setup (a short startup timeout ctx is reasonable
  against current code); this redesign makes that ctx the INSTANCE'S
  LIFETIME -- fine pre-v1, but both Registers' doc comments must say it.
  (Piggybacking the consumer heartbeat on the Consume loop instead was
  considered and rejected: breaks the producer/consumer symmetry and misses
  janitor-only instances.)
  the API-shape decision belongs in Phase 13's review BEFORE v1 freezes the
  surface, even if the feature itself ships later.
  first consumer: a Destroy gate -- refuse to destroy while any producer on
  the topic is ALIVE (not merely active: idle-but-alive can wake mid-drain),
  with the refusal naming instances and their last-seen times. RabbitMQ's
  queue.delete(if-unused) is the precedent. The gate is a front door, not the
  correctness guarantee -- check-then-drain has an unavoidable TOCTOU window
  and heartbeat liveness has the usual false-alive (stale row until TTL) /
  false-dead (partitioned but alive) ambiguity -- so deleteTopic's bounded
  drop loop stays as the hard backstop. Probably wants a force override for
  the operator who knows better.
  also the natural substrate for the Default alerts entry below -- an alert
  that can say "destroy blocked: producer X seen 2s ago" instead of a bare
  threshold is the version of that layer that explains itself.
  second consumer: the circuit breaker's globalization quorum (LEARNING_PLAN
  Phase 13's circuit-breaker design bullet). Its two-tier design trips
  per-instance breakers on local evidence and escalates to a group-wide OPEN
  when K distinct instances are locally open -- and expressing K as a
  fraction of the group (rather than a brittle absolute) requires knowing
  how many instances are ALIVE right now, which is exactly this entry's
  heartbeat rows. First feature with a hard dependency on presence, not just
  an operator-visibility win.

Default alerts: a built-in layer for "approaching an operational limit" conditions
  problem: several failure modes in this project are silent until they happen --
  nothing warns a user before they cross an operational cliff, and the only way
  to know one is coming is to independently derive the math (the way this
  session had to, twice, in one sitting). Two concrete examples that motivated
  this, both from Phase 8c:
    - partition count climbing high enough that dropping a whole topic in one
      transaction risks exhausting Postgres's shared lock table -- ~1000
      partitions was fine, ~2000 wasn't, on stock Postgres settings (full
      mechanism in LEARNING_PLAN.md's 8c section, search "out of shared
      memory"). topic.Destroy itself has since been fixed with batched
      per-partition drops, but a user running their own DDL against topic
      tables still walks into the same fixed-size lock table blind.
    - a compacted topic's unbounded "is this key still the latest" read cost
      growing LINEARLY with the topic's accumulated partition history (measured
      ~10us/partition, no early termination -- LEARNING_PLAN.md's 8c "Open
      question" bullet) -- a backlog replay against an old, high-history topic
      can silently become minutes of pure query overhead with no signal telling
      the operator why.
  neither of these is a correctness bug -- both "work fine until they don't."
  decision (concept only, not designed in detail yet): a `default alerts` layer
  shipped with built-in opinions about known cliffs (partition count past N,
  compaction history depth past N partitions since a key's oldest surviving
  version, etc). each alert, when it fires:
    - explains WHY it matters -- the actual mechanism (e.g. "dropping this topic
      in one transaction risks exhausting Postgres's shared lock table"), never
      just "value > threshold".
    - lists the LEVERS available to address it, each with enough context to
      judge, not a generic "raise your limits" instruction (e.g. for partition
      count: widen PartitionSize, switch to batched drops, shorten retention).
    - is OVERRIDABLE per-check -- a user who's made an informed call that a
      check doesn't apply to them (bigger max_locks_per_transaction,
      intentionally short-lived topics) can raise or disable it.
  open questions, deliberately not resolved speculatively:
    - delivery mechanism -- log line, a metric/gauge, a health-check endpoint,
      or an active check the Janitor/Consume loop runs each tick? pkg/logger
      (a common Logger interface) and pkg/consumer/metrics (otel QueueState
      gauges) already exist now -- this should reuse one of those, not build a
      third extension point.
    - where defaults live -- hardcoded thresholds, computed live from the
      user's actual Postgres settings (e.g. query max_locks_per_transaction and
      derive a real partition-count ceiling), or user-configured per deployment?
    - v1 scope -- start with the two concrete, already-measured triggers above
      rather than designing a general-purpose rule engine up front.
  natural home is probably pkg/topic's Janitor loop once it exists, since that
  already runs periodically per topic.

message schema evolution

consider using named return function params for User public functions to be clear in what they mean

need to relook at what should be consumer vs topic vs producer config. Probably a decent amount of consumer config that should be topic config ie janitor stuff

consider adding a testing suite ie can easily add messages in 'inflight', 'ready' with attempts or 'dead' state. Be able to inject failures easily for chaos testing etc

cleanup go.mod file aftering factoring out examples into seperate project (they bring in excess deps that core lib doesn't need)

doc site needs a worked example of the side-effect footgun inside a ProducerFunc (or a
multi-target publish closure) -- eg calling sendEmailConfirmation() before the
transaction is known to commit fires the email even if a later step in the same call
rolls everything back. show the fix: defer any non-transactional side effect until
after Produce/the multi-target call returns nil, never inside the closure itself.
same underlying idea as the transactional-outbox doc callout, worth showing together.

circuit breaker for a known-dead downstream dependency
  raised during the delivery_log design discussion (Phase 11's attempt audit
  trail): a high-throughput topic whose consumerFunc all calls out to one
  external dependency (e.g. AWS) that goes down for an hour keeps retrying at
  full claim throughput the whole time -- every claimed message fails, gets
  parked/retried/dead-lettered, hammering deliveries (and message_id-log
  volume in general) at close to peak system throughput for the entire
  outage, worst-case exactly when the datastore is least able to absorb it
  (see the deliveries-per-topic TODO above for the write/vacuum/WAL cost this
  produces). a circuit breaker would stop the retry storm at its source
  instead of making the datastore layer absorb it better: after enough
  consecutive/recent failures against the same dependency, stop claiming (or
  fail fast without even attempting consumerFunc) until a cooldown or a
  health probe says it's safe again. this is a consumerFunc/retry-policy
  concern, not a pkg/consumer datastore concern -- likely lives as an
  optional wrapper around consumerFunc itself, or a new MessageConsumerConfig
  hook, not a new datastore method. not designed in detail yet -- open
  questions: per-topic or per-group, what counts as "the same dependency"
  (consumerFunc is opaque to this library), and how a breaker's open/closed
  state interacts with the existing backoff() formula.

  why this is the right layer to fix it at, not a faster ClaimExceptions
  query: worked through adding an index to speed up the exception-claim scan
  during exactly this outage, and concluded it would make things WORSE, not
  better. Commit's parkSql (fresh happy-path failures) isn't gated by
  ClaimExceptions' cost at all -- it parks new 'ready' rows at full
  production throughput regardless. ClaimExceptions' own retry-claim path IS
  gated by that query, but every retry against a dependency that's provably
  down for the next 50 minutes is guaranteed-wasted work -- going faster
  there just burns through maxAttempts sooner, permanently dead-lettering
  messages that would have succeeded fine once the dependency recovered,
  exactly counter to what backoff()'s increasing-delay design is trying to
  do. a slow, unindexed scan during a burst is closer to an accidental,
  crude backpressure on the one path where speed is actively undesirable
  right now than it is a bug -- so the deliveries.status/can_run_after index
  idea explored alongside this was dropped; TODO.md's original "revisit with
  real evidence, don't add speculatively" stance on that index holds. the
  actual fix is not letting the backlog get pointlessly huge in the first
  place (this breaker), not making the datastore survive scanning it fast.

we need to refactor our migration scripts into code -- ie we can't ask users to run a migration script to setup tables and relations
  - we need to decided if it should be automatic or something like Register or Ensure with Topic

this can be later but we need to think through security. Ideally we can easily setup and create users with least privledge AND easily enable / disable row level security on these tables or per topic


***IMPORTANT*** we need to understand how user schema changes happen. IE user starts topic with work struct one way -> they want to change it -> how does that work. What are the edge cases that break schema changes. How can we make it easy

a full testing strategy (likely based on labs) which not only do e2e tests but also provide benchmarking metrics that we can save and record somewhere to make sure key metrics like throughput are not degrading overtime

This is a much later thing but we should take the time to understand uber's design decisions for their internal highly resilant DB
- https://www.youtube.com/watch?v=g7FmEc5GLWs&t=387s
- Switching from FIFO to LIFO under overload conditions. We could think of this ourselves as maybe not a switch but a load shedding gauge where if lag is growing we being to park older lease ranges systematically to skip more and more until lag is caught up.
- This idea of priority tiers is also interesting. Where each piece of work has a priority to it 0-5. If under overload scenarios low tier work is dropped or skipped.
- The producer side of overloading is interesting to think through as well

decide on what is in internal vs pkg. Pkg should ideally be much smaller API surface only. Simple so even users can scan the code to find what they might be looking for

consider allowing parts of janitor process to be opt-out. Such that users could opt-out and then in seperate process run janitor directly. To allow for better scaling and seperation of concerns. Needs to be thought through well because boundaries depend on each other so what happens if janitor is 'down' in opt-out world

IMPORTANT related to above we need to think through the consequences of many workers / consumers all running janitor processes and how we can prevent overloading or lock contention. Ideally without leader-election but we will see. Perhaps we can utilize the same concept of lease ownership. So each janitor instance fights for a lease when work is available via (can_run_after). Janitors should have some jitter in poll. One concern is the sporadic and random load distrubtion across janitor instances in terms of cpu/mem. But that could be resolved with opting-out of in-consumer janitor process

related finding from the config-placement review (LEARNING_PLAN.md Phase 13): the "many workers all running janitor" scenario above isn't hypothetical future state, it's what happens TODAY with zero opt-in. Janitor() runs once per MessageConsumer instance, i.e. once per consumer GROUP process -- but every one of its four operations (EnsureNextPartition, DropExpiredPartitions, SweepExpiredPartitions, SweepExpiredIdempotencyKeys) is purely topicID-scoped, no consumerGroup param anywhere in any of their signatures. So N consumer groups reading one topic today already means N redundant janitor loops independently hammering the same partitions/idempotency_key rows -- this was true before any opt-out design exists, just currently invisible because JanitorPollRate/PartitionSafetyBuffer/JanitorSweepBatchSize lived on MessageConsumerConfig (now moved to topic.Config, so at least the N loops agree on the same values -- doesn't stop there being N of them). Whatever opt-out/scaling design lands here should also settle who actually OWNS running the janitor for a topic -- today's answer ("whichever consumer group processes happen to be up") was never a deliberate choice, just where the loop happened to get wired in.

group / order config options and placement of fields in tables via likeness. ie similiar fields should be logically next to each other for easier understanding.

Need to obsess and standardize over every potential error message. Need to make each error message when can control as understandable and actionable as possible probably via enriching / adding metrics or links to docs (eventually)

A couple things I don't want to forget about
  1. use of a bloom filter for idempotency checking -- basically much faster way to check if idempotency key does not exist in set (instead of using CTE constraint)
  2. Want a thorough throughput and latency test that is multi topic with high concurrency and ideally hits db limits (the single-topic skip-vs-claim comparison
  was measured in bench/idempotency/RESULTS.md before SkipIdempotency was removed; multi-topic contention is still in its deferred list)
  3. Need to confirm that us manually creating UUIDv7 via go code is compatible with how PG18 better optimizes storage / pages with their built in UUIDv7(). ie their isn't some
  metadata field that somehow gets set which tells tuples to be writen sequentially in pages it is just the values themselves

Look into using BRIN index for different tables
look into using a GIN index for a headers table. ie could consiladate routing key into a headers JSONB column with GIN index (for efficent lookup). And this could allow user arbitrary key value routing / ordering logic for: routing, delays, priority and load shedding capabilities.

we need to test compaction key with default produce and determine if deadlock contention by reverse ordered transactions is a problem or not.
  - ie and what extreme (or not extreme) example would it truly become a problem for users or can the system self heal through retries
  - I know we can move these users to ProduceFunc but just to know

named / defined errors for users to errors.Is on for convinence. How do we want to structure that? etc

does pgx send sql comments to db? if so is that wasted bytes over the network we should try to limit

for users public api need to abstract away required variables as plain params and optional params should be in the Config structs.
  Config structs should also be renamed as OptionalConfig to be more obvious

consider abstracting out the claim fence transaction xmax logic into its own async ticker and claimers read from shared mem var. It would better abstract away that complex logic and we can have the poll rate much faster because it is a pretty cheap query

We should contribute to postgres via adding similar functionality MIN_ACTIVE_ROWVERSION that sql server has to make claim fence easier for us and others

really need to think hard on our use of terminology Topic, Consumer and Producer. GCP pubsub uses topic, publisher, subscriber. NATS, SQS, Pulsar producer.Send, consumer.Receive

A specific DeadLetterTopic Consumer. You can consume on events to DLQ

**`topic.Exists`/`Register`/`Destroy`'s call shape** (admin object)
**Migrations-into-code.**
**Row-level security / least-privilege setup**

**Default alerts**
**Chaos-testing / fixture suite**

flesh out TEST.md (the shutdown/interruption scenarios recorded there so far are Setup/Action/Assert prose from a scratch harness, not code) and implement it as an actual pkg/producer/pkg/consumer test suite once the API stops moving