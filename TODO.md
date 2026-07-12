graceful shutdown
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
Consumer Consume's Project, Process and RollWaterline when error stop the consumer ie a database network blip crashes the consumer
  - we likley don't want this to happen and instead would want a retry backoff logic
On graceful shutdown should update active inflight lease low to last processed work so it does not retry already processed work
Consider further splitting the ClaimPollRate for each caller (project, process, rollup, drain)

Commit's exception park is a loop of individual INSERTs inside one transaction (one Exec per failed message, one commit). switch to pgx.Batch: same per-row SQL, queued and sent together instead of one round-trip per row. deferred on purpose -- exceptions are the sparse/rare path by design, so the round-trip cost is unlikely to matter, and a plain loop is the simplest code to read while the exception-drain machinery around it is still being built. revisit once that's stable, since it's a small change (swap Exec-in-a-loop for Batch/SendBatch) that shouldn't touch the surrounding logic.

recover() around consumerFunc invocations
  problem: today an ordinary Go panic (nil map write, index out of range, bad type assertion
  on an unexpected payload shape) is indistinguishable from a true process crash - nothing
  wraps the call, so it kills the worker outright. because the crash happens mid-range, it
  takes the WHOLE claimed range down with it (lease expires, gets reclaimed, re-reads the
  exact same range, hits the exact same panic again) instead of failing just the one message.
  decision: wrap the consumerFunc call in defer/recover; a recovered panic becomes an
  ordinary error, routed through the same per-message retry/backoff/dead-letter path as any
  other failure. capture the panic value + runtime/debug.Stack() into last_error so it's
  visible, not swallowed.
  gotchas:
    - recover() only catches genuine Go panics. it does NOT catch OS-level faults - stack
      overflow, SIGSEGV via cgo, OOM-kill, or an external kill (liveness probe, watchdog).
      those still crash the process outright and still need the range-level quarantine cap
      as the backstop; recover() narrows what reaches that backstop, it doesn't replace it.
    - a panic almost always indicates a real bug (vs. a transient failure like a network
      blip), so it probably deserves louder logging / a distinct last_error tag rather than
      blending into ordinary retry noise.
  depends on: the exception/park machinery (sparse deliveries + retry/backoff/dead) needing
  to exist first, so a recovered panic has somewhere to land as a normal per-message failure
  instead of the current behavior of aborting the whole range.

hard per-message timeout via a detached goroutine race (not bare context.WithTimeout)
  problem: WorkTimeout is declared and validated today (consumer.go) but never actually
  enforced as a deadline on consumerFunc - it only feeds the lease-duration math
  (leaseDuration = WorkTimeout + QueueTimeout + AckMargin). the parked V1/V2 code used to
  wrap the call in context.WithTimeout, but that wrapping was dropped when those paths were
  commented out. and context.WithTimeout alone is a partial fix anyway: it's cooperative -
  it just closes ctx.Done() - so it does nothing for a call that never checks its context
  (a blocking cgo call, a tight CPU loop from a pathological regex/ReDoS, a library that
  ignores ctx entirely). the calling goroutine stays blocked regardless of what the context
  says, because nothing forces the callee to stop.
  decision: race consumerFunc's completion against a timer using a buffered channel (or an
  errgroup's derived context) + select, so the CALLER can give up and move on independent of
  whether the callee ever cooperates:
    done := make(chan error, 1) // buffered so a late send never blocks
    go func() { done <- consumerFunc(ctx, &work) }()
    select {
    case err := <-done:
        // finished in time
    case <-time.After(p.Config.WorkTimeout):
        err = ErrWorkTimeout
        // consumerFunc's goroutine is NOT killed - it's abandoned, not stopped
    }
  gotchas:
    - do NOT call errgroup.Wait() synchronously here. Wait() is a join - it blocks until
      EVERY goroutine you Go()'d has returned - so if consumerFunc is genuinely hung, Wait()
      hangs right alongside it and defeats the entire point. if using errgroup for structural
      consistency with the rest of the package, select on the derived ctx.Done() instead of
      calling Wait(), and only call the real Wait() (to drain/log the late result) from a
      separate detached goroutine that nothing else waits on.
    - go has no goroutine kill. this does not free the abandoned call's resources - its
      stack, any DB connection it checked out of the pool, any locks it holds all keep
      existing. it converts "one message hangs the whole range, worker gets externally
      killed, whole range gets reclaimed and re-crashes" into "one message leaks one
      goroutine forever" - strictly better containment (isolated to that message instead of
      poisoning the whole range) but not a full fix. repeated hangs (the same message retried
      up to MaxAttempts, or a systemic downstream degradation) can still accumulate leaked
      goroutines/connections into the very OOM this was meant to avoid.
    - doesn't help at all against message-size OOM (memory can blow up well inside the
      timeout window) or an instant fatal fault (stack overflow, SIGSEGV) - those still need
      the range-level quarantine cap.
  depends on: the exception/park machinery, so a hard-timeout abort has somewhere to land
  (tag last_error distinctly, e.g. "hard timeout after Ns, goroutine abandoned") instead of
  aborting the whole range.

track abandoned goroutines for metrics / alerting
  problem: once the hard-timeout race above exists, some calls get abandoned rather than
  killed (go has no goroutine ID or introspection API to observe them externally). tracking
  has to happen at the semantic layer WE control - the moment we decide to stop waiting - not
  by trying to observe the raw goroutine.
  decision: a small in-process registry, keyed per-ATTEMPT, not per-message. this matters: if
  message X times out on attempt 1 (abandoned, still running) and gets retried later on
  attempt 2 which also times out, there are now TWO outstanding abandoned goroutines for the
  same message - keying by message id alone would let the second overwrite the first's entry.
  mechanism sketch:
    // on the timer branch of the select:
    id := registry.add(abandonment{group, msg.Id, attempts, time.Now()})
    metrics.HardTimeouts.Inc(group)      // counter - rate of hangs
    metrics.Outstanding.Inc(group)       // gauge - currently-leaked count
    go func() {                          // detached: drains the late result, if it ever comes
        err := <-done
        metrics.LateFinish.Observe(group, time.Since(deadline)) // histogram
        registry.remove(id)
        metrics.Outstanding.Dec(group)
    }()
  three signals, three questions:
    - counter (hard_timeouts_total) - how OFTEN this happens. a spike means a poison payload
      or a degrading downstream dependency; a steady trickle means the timeout is tuned tight.
    - gauge (abandoned_outstanding) - how many are leaked RIGHT NOW. the direct
      leak-prediction signal - alert when it climbs and doesn't come back down.
    - histogram (late-finish latency) - for the ones that DO eventually return, how late.
      seconds-late means the timeout is tuned too tight; "never observed" confirms genuinely
      stuck, not just slow.
  deep-dive when an alert fires: don't hand-roll stack capture. mount net/http/pprof and hit
  /debug/pprof/goroutine?debug=2 - every abandoned call is blocked in the same closure
  (wherever the race wraps consumerFunc), so they're trivially greppable by function name.
  free synergy: tag last_error on the Nack path distinctly for a hard-timeout abort (e.g.
  "hard timeout after Ns, goroutine abandoned") - gives a durable, queryable record
  (SELECT * FROM deliveries WHERE last_error LIKE 'hard timeout%') on top of the live
  metrics, no extra infrastructure.
  depends on: the hard-timeout race above (this tracks its abandonment event) and a common
  logger/metrics interface (ties to the existing "internal pkg logging needs to be able to
  pass a logger interface that is common" / "need a logger that takes in an arbitrary writer"
  items above) - there's no metrics abstraction in this repo yet, so this needs an extension
  point a caller can implement rather than a hardcoded backend.

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
  today `bindings.pattern` is a true wildcard: `*` matches any run of characters
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

topic.Destroy can exhaust Postgres's lock table on a topic with enough partitions
  problem: DeleteTopic (pkg/topic/datastore.go) wraps `DELETE FROM topics` +
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
  now-empty parent + delete the topics row in one final small transaction.
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