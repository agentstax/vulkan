graceful database recovery (handles it fine now but could be done better with a retry backoff policy and better error messages)
debug field option which prints queue metrics like, how many are left
consider using database/sql from stdlib to remove pgx dependency (might be a bad idea)
current impl of a transactional enqueue (producer) doesn't support fanning out ie publishing to multiple queues
consider normalizing message log attempts into seperate append only table - so we can better track each attempt / attempted_at / error mainly for debugging / auditting. main code should not read this as it would slow things down
internal pkg logging needs to be able to pass a logger interface that is common
better abstract out the datastore from the consumer. ie consumer should not know or care about the internals of the datastore
need a logger that takes in an abitrary writer users can pass
for any contextWith like Timeout or Deadline should you alternative With*Cause to enrich error with better message
Reconsider the lazy rollup (AdvanceWaterline)
  - should we consider making it syncronous ie after lease batch has completed -> advance
  - on the metrics side of things how will users track and see when a message or batch is inflight and completed
Process's loop (CURSOR/LIFECYCLE both) still exits entirely the moment CursorClaim/LifecycleClaim returns any error. A blip that clears within pkg/retry's MaxRetries is now correctly absorbed (Wrap returns nil, Process never sees it -- fixed as part of Phase 9's fault-isolation-lab). What's still open: retries exhausted (a sustained outage) or a genuinely permanent error still kill Process outright rather than backing off and trying again on the next tick.
  - open question: should Process itself retry/backoff at the poll-loop level (treat a claim-level failure as one bad tick, not fatal), or is "the caller of Consume restarts the process" the intended failure boundary?
Consider further splitting the ClaimPollRate for each caller (project, process, rollup, drain)

Commit's exception park is a loop of individual INSERTs inside one transaction (one Exec per failed message, one commit). switch to pgx.Batch: same per-row SQL, queued and sent together instead of one round-trip per row. deferred on purpose -- exceptions are the sparse/rare path by design, so the round-trip cost is unlikely to matter, and a plain loop is the simplest code to read while the exception-drain machinery around it is still being built. revisit once that's stable, since it's a small change (swap Exec-in-a-loop for Batch/SendBatch) that shouldn't touch the surrounding logic.

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

FanOut rescans the entire message_log on every call instead of tracking a per-group
high-water mark. `INSERT INTO deliveries ... SELECT ... FROM message_log_<topic_id>`
has no `WHERE id > <last fanned-out id>` and no LIMIT, so cost grows with total log
size, not with new-messages-since-last-call -- `ON CONFLICT DO NOTHING` makes it
correct regardless of how many times a row gets re-selected, just wasteful. fine at
demo scale (the SQL comment already says so), but a real fix means giving the
LIFECYCLE path its own per-group cursor, which is a bigger scope decision than a
quick batch LIMIT -- revisit alongside any future work on the LIFECYCLE path itself.

  narrowed by Phase 8b: each topic now has its own message_log_<id>, so the rescan
  cost is bounded by ONE topic's volume instead of the whole system's -- still a
  full rescan within that topic though, not actually fixed.

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

topic.Destroy can exhaust Postgres's lock table on a topic with enough partitions
  problem: DeleteTopic (pkg/topic/datastore.go) wraps `DELETE FROM topic` +
  `DROP TABLE message_log_<id>` in ONE transaction. message_log_<id> is
  partitioned, so dropping the parent requires an ACCESS EXCLUSIVE lock on every
  partition AND every object each partition owns -- confirmed empirically each
  partition owns 5 lockable relations (the partition table, its pkey index, its
  partial compaction_key index, its TOAST table, and the TOAST table's own
  index). Postgres's shared lock table is fixed-size at server start
  (max_locks_per_transaction * (max_connections + max_prepared_transactions) --
  6400 on this dev Postgres's stock defaults, 64*100). Measured directly while
  building Phase 8c's compaction-width-lab: a topic with ~1000 partitions
  destroyed fine (~5000 locks needed), ~2000 partitions failed with "out of
  shared memory" (~10000 locks needed, past the 6400-slot ceiling). Not
  compaction-specific -- any topic (compacted or not) that accumulates enough
  partitions hits this on Destroy. 8a's own retention janitor (dropPartition,
  sweepBatch) already avoids this exact trap by dropping ONE partition per call
  in small batches; Destroy never got that treatment because it's a full-topic
  teardown, not an incremental janitor.
  decision: reimplement DeleteTopic using the same batched-drop shape as 8a's
  dropPartition/sweepBatch -- DETACH + DROP each partition individually, batched
  across multiple transactions (e.g. 50-100 at a time), THEN drop the
  now-empty parent + delete the topic row in one final small transaction.
  Raising max_locks_per_transaction server-side is NOT a fix by itself -- it
  requires a restart, and a topic can always eventually outgrow whatever new
  ceiling gets picked; batching removes the ceiling instead of just raising it.
  full mechanism + the exact math above is in LEARNING_PLAN.md's 8c section
  (search "out of shared memory").

Default alerts: a built-in layer for "approaching an operational limit" conditions
  problem: several failure modes in this project are silent until they happen --
  nothing warns a user before they cross an operational cliff, and the only way
  to know one is coming is to independently derive the math (the way this
  session had to, twice, in one sitting). Two concrete examples that motivated
  this, both from Phase 8c:
    - partition count climbing high enough that Destroying/dropping a whole
      topic in one transaction risks exhausting Postgres's shared lock table
      (see the topic.Destroy entry directly above) -- ~1000 partitions was fine,
      ~2000 wasn't, on stock Postgres settings.
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
      or an active check the Janitor/Consume loop runs each tick? this needs
      the SAME logger/metrics extension point the "internal pkg logging" and
      "track abandoned goroutines" TODO items above already call for, not a
      separate one -- don't build a second one.
    - where defaults live -- hardcoded thresholds, computed live from the
      user's actual Postgres settings (e.g. query max_locks_per_transaction and
      derive a real partition-count ceiling), or user-configured per deployment?
    - v1 scope -- start with the two concrete, already-measured triggers above
      rather than designing a general-purpose rule engine up front.
  depends on: a common logger/metrics interface (already a TODO above); natural
  home is probably pkg/topic's Janitor loop once it exists, since that already
  runs periodically per topic.

EXPLAIN (ANALYZE, BUFFERS, TIMING) 
UPDATE message_log
SET 
	status = 'processing',
	lease_until = now() + make_interval(secs => $2),
	lease_token = gen_random_uuid(), -- 'owner' claims this uuid
	attempts = attempts + 1
WHERE id IN (
	SELECT id FROM message_log
	WHERE (status = 'ready' AND can_run_after <= now())
		OR (status = 'processing' AND lease_until < now()) -- retreive any 'expired' work
	ORDER BY id
	LIMIT $1
	FOR UPDATE SKIP LOCKED
)
RETURNING *;

do we need idempotency_keys_topic_created_at (adds further write overhead to only benefit the janitor process)

message schema evolution

rethink producer having both idempotencyKey AND skipIdempotency. Feels weird like ideally we should only have one. It is b/c our default is opt out
which I like however that causes this problem which I don't like
we have a lot of seperate round-trip queries that would benefit from pgx.Batch

consider using named return function params for User public functions to be clear in what they mean

need to relook at what should be consumer vs topic vs producer config. Probably a decent amount of consumer config that should be topic config ie janitor stuff

consider putting logTable on base postgres datastore instead of redefining everywhere

consider adding a testing suite ie can easily add messages in 'inflight', 'ready' with attempts or 'dead' state. Be able to inject failures easily for chaos testing etc

cleanup go.mod file aftering factoring out examples into seperate project (they bring in excess deps that core lib doesn't need)

docs should mention that ProducerFunc's tx IS the transactional outbox pattern collapsed
into one hop -- the caller's business-row write and the message_log insert commit
together with no separate outbox table or relay process, as long as both live in the
same Postgres. worth calling out explicitly since it's a pattern people search for.

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
  optional wrapper around consumerFunc itself, or a new WorkConsumerConfig
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