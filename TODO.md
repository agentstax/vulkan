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