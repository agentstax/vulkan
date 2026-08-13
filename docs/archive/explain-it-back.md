# Explain-it-back archive

The user's own answers to the Explain-it-back exercises from the retired
NOTES.md (deleted 2026-08-13; full file in git history). Archived verbatim,
one section per phase, under each phase's original heading. Some decision
rationale exists only here — treat this as source material, not disposable.

## Phase 1 — The durable atom: append + atomic claim


**1. Why must the `DELETE` be in the same transaction as the claim? Walk both
orderings if it's separate.**

Because a separate delete is unsafe in *both* possible orderings:
- **Delete after processing (separate tx):** the work finishes, then the delete
  has a network blip / crash before committing → the row is still there → it gets
  claimed and processed *again*. Duplicate work.
- **Delete before processing (separate tx):** the row is gone, then the worker
  crashes mid-process → the work is lost forever and never completed. Worst case.

Same-transaction delete risks neither: either the commit lands (processed AND
deleted) or it doesn't (nothing happened, row still claimable). Atomicity is the
whole durability story of this phase.

**2. A worker is `kill -9`'d mid-process. What does Postgres do, and when is the
row claimable again?**

The connection drops with an open, uncommitted transaction. Postgres treats that
as a failed transaction and rolls it back, which releases the `FOR UPDATE` lock.
The row is claimable again as soon as the rollback completes — another consumer's
`SKIP LOCKED` query will see it on the next poll.

**3. What does `SKIP LOCKED` change about the result set, and why is skipping safe
here when it would normally be a correctness bug?**

It removes already-locked rows from the result set instead of blocking on them. A
locked row is "in process" by another worker, so skipping it is exactly what we
want — skipping prevents double-processing rather than dropping work. (The work
isn't lost; it's just owned by someone else right now.)

## Phase 1.5 — Transactional enqueue (the killer feature)


**1. Describe the dual-write problem, why neither ordering is safe, and why
retries don't fix it.**

The dual-write problem is writing to **two separate systems with no shared
transaction** — e.g. commit the business row to Postgres, then publish the event
to an external broker (Kafka/RabbitMQ). There's no safe ordering:
- DB commits, then publish fails → work done, event lost.
- Publish succeeds, then DB rolls back → phantom event for work that never
  happened.

Retries narrow the window for transient faults (a network blip), but can't close
it: the process can die *between* the two writes, and a permanent failure (e.g.
validation rejection on the second write) leaves the first one stranded with
nothing to retry against. The only real fix is to make both writes part of one
transaction — which is exactly what putting the queue *in* Postgres buys you, and
why this phase removes the dual-write entirely.

**2. Why can a consumer never observe a job from an uncommitted producer tx? Which
ACID property does the work?**

**Isolation.** Under read-committed, one transaction's uncommitted writes are
invisible to every other transaction. The producer's INSERT lives in the WAL but
isn't visible in the table to anyone but the producer until `COMMIT`, so the
consumer's claim query simply doesn't see the row. (Atomicity guarantees the
producer's own writes are all-or-nothing; Isolation is what governs what *other*
transactions are allowed to see.)

**3. What is the outbox pattern, and what part of it have you already built?**

The outbox pattern reliably gets events to an *external* system without the
dual-write problem: the business write and an insert into an **outbox table**
happen in one transaction, and a separate **relay** process reads the outbox table
and forwards downstream (to Kafka, Elasticsearch, etc.). What I've already built
is the outbox itself — `message_log` is the outbox table, and the atomic
business-write-beside-the-enqueue is the producer side of the pattern. The only
missing piece is the relay (Phase-9-ish; Debezium/CDC reading the WAL is the
canonical version).

## Phase 2 — Per-message lifecycle


**1. In Phase 1, what held the claim? In Phase 2, what holds it? Why did it have
to change?**

Phase 1: the DB lock (`FOR UPDATE`) held the claim for the entire processing
duration. Phase 2: the row *data* holds it — `status='processing'` plus
`locked_at`. It had to change because a long-running job in Phase 1 keeps a
transaction (and its connection) open for its whole lifecycle; under any real
concurrency that pins a huge number of connections open, which doesn't scale.
Phase 2 takes only a millisecond lock to claim, then relies on the row data +
the claim predicate to know what's "in flight" versus available.

**2. The full state machine, with every transition's trigger.**

- `ready` **→** `processing` — a claim matches the row (`status='ready' AND
  can_run_after <= now()`); the claim sets `locked_at`, `attempts++`, and
  releases the lock.
- `processing` **→** `done` — `consumerFunc` returns nil; `RecordSuccess`.
- `processing` **→** `ready` (retry) — `consumerFunc` returns an error and
  `attempts < maxAttempts`; `RecordFailure` sets `can_run_after = now() +
  backoff(attempts)` and records `last_error`.
- `processing` **→** `dead` — `consumerFunc` errors and `attempts >=
  maxAttempts`; terminal, no more retries (this set is the dead-letter queue,
  queryable as `WHERE status='dead'`).
- `processing` **→** `processing` (reclaim) — the worker crashed and the lease
  expired (`locked_at < now() - stuck_window`); the *next* claim matches the
  stuck row via the OR-branch, re-stamps `locked_at` and `attempts++`. No
  separate reaper process — reclamation is folded into the claim.

*Correction vs my recall:* my original answer listed the happy path + both
failure branches but omitted the `processing → processing` **reclaim** edge (the
transition that defines Phase 2), and didn't spell out that retry means
`→ ready` *plus* a backoff on `can_run_after`.

**3. Why does lease reclamation make delivery at-least-once rather than
exactly-once? What property must the consumerFunc have?**

If a `consumerFunc` runs longer than the lease window, the row looks "stuck" and
another worker reclaims and re-runs it — so the same message can be processed
more than once. Two mitigations, and you want both: keep the lease window
comfortably longer than the work timeout so live workers aren't reclaimed, and
make the `consumerFunc` **idempotent** so a genuine double-delivery (crash after
side effect but before `RecordSuccess`, slow worker past its lease, etc.) is
harmless. The timeout buffer reduces *how often* it happens; idempotency is the
only thing that makes it *correct*.

*Correction vs my recall:* I originally framed it as "long lease **OR**
idempotent." Wrong — they're not alternatives. Idempotency is mandatory; a
longer lease only lowers the *frequency* of double-delivery and never eliminates
it (a crash after the side effect but before `RecordSuccess` re-runs regardless
of lease length).

## Phase 3 — Competing consumers & batching


**1. Why is the partial index so much better than a full index on
`(status, run_at)` for this workload?**

A full `(status, run_at)` index indexes *every* row — including the entire
graveyard of `done`/`dead`. It grows with the queue's whole history and rots
(bloat, cache pressure, vacuum cost) even though we only ever query the tiny
live (`ready`) set. A partial index `WHERE status='ready'` contains only live
rows, so it stays small no matter how much history accumulates, and the claim
scan never touches dead entries. (`status` is also low-cardinality, so it's a
poor leading column for a composite index; the partial predicate drops it
entirely and the index orders purely by the useful key.)

*Correction vs my LEARNING_PLAN answer:* I compared *index vs no index* (bitmap
heap scan vs sequential scan, ~0.05ms vs ~0.215ms at 1000 rows). That's a real
effect but it's the wrong comparison — the question is partial vs *full
composite*, and the point is the graveyard: the partial index excludes it, the
full one carries it forever. **Deeper twist from the ceiling lab:** because the
claim does `ORDER BY id`, neither the `(status, run_at)` index *nor* the
`can_run_after` partial index is actually used — the planner takes the primary
key and filters inline, scanning the whole graveyard (0.057ms → 41.8ms, 730×, at
150k `done` rows). The fix was migration 005: a partial index keyed on `id`
covering both live states, so the ordered scan skips the graveyard.

**2. Batch claiming in Phase 1 had a failure-domain problem. Why doesn't
Phase 3's batching have it?**

Phase 1 claimed *and processed* a batch inside one transaction — all-or-nothing.
One message failing, or any mid-batch error/rollback, took down the whole batch;
unrelated messages shared a single success/failure fate. Phase 3 splits it: the
claim is its own fast transaction (flip `status→processing`, stamp the lease,
`attempts++`), and then each message is processed and acked **individually** —
`RecordSuccess`/`RecordFailure` per row, guarded by a `lease_token` CAS. A
failure is recorded only against that one row (its own backoff/retry/dead-letter).
The batch is now purely a round-trip optimization, not a failure unit — one bad
message can't poison its batch-mates.

*Correction vs my LEARNING_PLAN answer:* I described the connection/lock-holding
scalability problem (Phase 1 pinning a connection per in-flight job). True, but
that's the Phase 2 "what held the claim / why it had to change" answer — not the
failure-domain problem, which is about per-message ack isolation.

**3. What was the measured ceiling, what's the bottleneck, and how would you
tell?**

~20–22k msgs/sec at 64 workers on this box, and the bottleneck is the **ack
path** — one single-row `UPDATE` + commit per message, so each pays a WAL fsync
and a round-trip (`synchronous_commit = on`). Batching commits would lift it but
may not be worth it given upcoming topology changes. How I told: (a) sampling
`pg_stat_activity` wait events; (b) raising `batch` lifted throughput then
plateaued — so *not* supply-bound; (c) at a fixed large batch, throughput scaled
with worker count (8k→20k across 8→64) — so it's the concurrent-commit/ack path.

**4. Why must the in-memory buffer stay shallow? What goes wrong with a deep
buffer that didn't for the scrape queue?**

Every buffered row carries a live lease (`lease_until` stamped, `attempts++` at
claim time). In a deep buffer rows dwell past the lease window, get reclaimed by
another worker, and are double-processed (the reclaim logic bounds the damage but
can't prevent it) — and idle rows burn attempts toward `dead`. The scrape queue
in `examples/simple` had no lease and no durability: losing or redoing ephemeral
work was free, so deep buffering was harmless there. Here a shallow buffer is all
you need — just enough to mask claim-SQL latency and keep workers fed.

## Phase 3.5 — Throughput: the commit wall


Sharpened from my LEARNING_PLAN answers (originals kept there); corrections noted.

**1. Why is fsync-per-commit the throughput wall, and why is the *ack* (not the
claim) the half hardest to amortize?**

fsync is a costly physical mem→disk flush. The claim is **one commit per batch**
(amortized over ~100 rows); the ack is **one commit per message** via autocommit
`Pool.Exec`, so it can't amortize — that's the hard half. The on/off gap is
biggest at low concurrency (6× @1w) and shrinks at high concurrency (1.3× @64w)
because **group commit** auto-batches *concurrent* committers' fsyncs, so `on`
already amortizes when many commits are in flight.
*Correction:* the knob is `synchronous_commit=off` (defers the fsync to the WAL
writer), NOT `fsync=off` (skips it entirely → risks corruption). Group commit
needs concurrent in-flight commits, so conc=1 (one at a time) gets the biggest
win from `off`.

**2. Why is `off` a free lunch here but not for a bank ledger?**

at-least-once ⇒ duplicates are already possible ⇒ consumers are idempotent ⇒ a
lost commit is harmless because reclaim reruns the work. So relaxing durability
buys throughput against risk already priced in.
*Correction (two):* (a) what `off` loses on crash is the **acked-but-not-yet-
flushed** window — work the app *did* ack but whose commit wasn't durable, so it
*looks* unacked after recovery (crash lab: 899 lost under `off` vs 85 under `on`).
Not "unackd work" generically. (b) The ledger contrast is **no replay path for a
lost commit**, not "exactly-once needs distributed transactions." A queue can
replay (the message is still there + idempotency); a ledger that told the customer
"done" then lost the commit cannot, so durability is mandatory. Deciding question:
*is there a recovery path for a lost commit?*

**3. Which of the four levers survive Phase 4, and why do the rest dissolve/relocate?**

Four levers: **`synchronous_commit`** (survives — a global durability knob, blind
to table layout); **batch-ack** (dissolves — the cursor *is* the ultimate batched
ack: N messages acked by one integer write `position=$last`); **archive terminal
rows** (relocates → Phase 9 retention/partition-drop; an append-only log has no
`done`/`dead` rows to archive; returns for `deliveries` in Phase 6); **claim-
hotspot sharding** (dissolves — each cursor reads its own `offset > position`
range, so no competing claim on a shared hot row; returns when competing claims
return on `deliveries` in Phase 6, formalized as Phase 8's `partition_key`).
*Correction:* I named 3 of 4 and missed **claim-hotspot sharding** (the 4th);
also archive *relocates* rather than purely vanishing.

## Phase 4 — The log/queue split: retention + replay


**1. What can a cursor not express that per-row status could?**

Per-row lifecycle. With one integer I can't represent "5 failed, 6/7/8 succeeded" —
a hole in the middle. On a failure my only moves are *stop* (leave the cursor before
the bad row and retry it forever — head-of-line block) or *skip* (advance past it
and lose it). Per-row `status`/`attempts`/`dead` could mark exactly that one row
failed while its neighbours finished. That hole is the tension Phases 6–6.5 resolve
with a sparse exception side-table.

**2. Why does replay cost nothing?**

Reading position is decoupled from the data, and the log is append-only, so any
position is valid — replay is just `UPDATE cursor SET position = 0` (or to a
timestamp's offset) and the consumer re-reads history. Phase 1 could never do this:
it *deleted* on consume, so there was no history to replay. Replay is free because I
stopped destroying the thing I'd want to replay.

**3. Crash after processing, before the cursor update?**

On restart the cursor still points before that message, so it's claimed and
processed again → at-least-once. Same contract as Phase 2's lease, now enforced by
*ordering* (process-then-advance) instead of a lease: everything at or below the
cursor is durably done, so the consumerFunc must stay idempotent.

*Correction — the real Phase 4 lesson (caught in review):* my first cut of
`ClaimMessagesV2` had `WHERE id > $1 LIMIT $2` with **no `ORDER BY`**. SQL guarantees
no row order without `ORDER BY`, so `LIMIT` returns an *arbitrary* N rows — and since
`ProcessV2` advances the cursor to each returned `id`, the high-water mark can jump
*past* unread offsets and silently drop them forever (cursor=0, ids 1–5, limit 2
returns {4,5} → cursor=5 → 1,2,3 gone). It passed casual testing only because a small
table happens to get a forward PK index scan — coincidence, not a guarantee. The fix,
and the whole point of the phase, is `ORDER BY id`: a high-water mark is only correct
over an *ordered* claim. (My dead V1 claim already had `ORDER BY id`; V2 had regressed
it.)

## Phase 5 — Fan-out to independent consumers


**1. Why is fan-out structurally impossible in the Phase 1–3 design?**

Because consumption *mutates or destroys shared message state*. In Phase 1 consume =
`DELETE`; in Phase 2–3 consume = `UPDATE status='done'` on the one row. Either way
"has this been processed?" is a single bit attached to the message itself, not to
any consumer — a one-to-one mapping. The instant one worker finishes, the row is
gone (or `done`) and every other consumer sees it as handled; there's nowhere to
record that consumer B still hasn't read it. Fan-out needs one-to-many: the log
holds the facts immutably and each consumer carries its *own* position. That's
exactly the Phase 4 split — independent `cursor` rows over an append-only log.

**2. Operational risk of a permanently-slow consumer group once retention (Phase 9)
exists?**

It's consumer lag taken to the failure case: its `position` falls so far behind that
retention deletes log rows *the group hasn't read yet* — the data is dropped out from
under the cursor. On the next read the cursor points below the oldest surviving
offset, so those messages are gone for that group, never processed, with no error at
claim time (the `WHERE id > position` read just returns the surviving tail). This is
Kafka's "consumer fell off the retention window." The defense is operational, not
structural: monitor `head − position` (the `just lag` metric) and alarm before lag
approaches the retention horizon — retention and the slowest consumer's lag are in a
race, and you have to guarantee retention wins by a margin.

## Phase 6.5a — Claim-from-log: the happy path


**1. Where did the write amplification go, and what carries "this offset
succeeded"?**

The `committed` waterline carries it: every offset **≤ committed** is in a terminal
state (success-only for now). The amplification didn't relocate — on the happy path
it's *gone*. Phase 6 wrote O(N) `deliveries` rows; now a successful message writes
**no row at all**, and N successes collapse into advancing one integer (`committed`)
on one `cursor` row. O(N) row writes → O(1) integer advance.

**2. What do `claimed` and `committed` mean, and how do they relate in the
single-worker, no-failure happy path?**

Three zones on the log: **≤ committed** = resolved/terminal (success only right
now); **`(committed, claimed]`** = claimed-but-not-yet-resolved (in-flight); **>
claimed** = unclaimed (waiting). `claimed` is the read frontier, advanced atomically
at claim time; `committed` is the waterline. In this happy path the gap is
*transient* — `committed` marches up behind `claimed` message by message and catches
it whenever the consumer drains/idles. The gap only becomes a *persistent* structure
in 6.5b (open leases pin it) and 6.5c (unresolved exceptions pin it).

## Phase 6.5b — Lease the range: crash recovery


**1. A worker crashes mid-range — walk the recovery. Why rotate the lease token
instead of just refreshing `lease_until`?**

Worker claims `(lo, hi]` (lease inserted, token T) → crashes before `CommitRange` →
lease just sits there → its `until` passes → on a later poll another worker's
**Reclaim-before-Claim** scans `lease WHERE until < now()`, grabs it
`FOR UPDATE SKIP LOCKED`, and re-reads the exact `(lo, hi]` under a **new** lease
(new token T′). It reprocesses (at-least-once → processing must be idempotent).

Rotating the token defends against the **zombie**: the original worker can resurrect
(GC pause, slow syscall) and call `CommitRange`, which is token-guarded
(`DELETE FROM lease WHERE consumer_group=$1 AND token=$2`). If reclaim had merely
bumped `until` and kept token T, the zombie's commit would match T and free the
**live** lease the reclaimer now holds — double-free, and the waterline would
advance over a range still being processed. With T′, the zombie's `DELETE` hits 0
rows: a harmless no-op. (In this impl reclaim is a DELETE + fresh INSERT, so a new
token is structural, not an extra step.)

**2. What does an open lease do to `committed`, and what breaks if the waterline
passes an in-flight range?**

An open lease **pins `committed` at its `low`**: the advance is `committed =
GREATEST(committed, LEAST(min open-lease low, claimed))`, so the lowest open lease
caps it. The reason is what `committed` *means* — **every offset ≤ `committed` is
terminally resolved.** Let it pass an in-flight range and that promise is a lie: if
the worker then crashes, those offsets were never processed, but everything that
trusts the waterline (compaction/GC, "caught up", the durability guarantee) already
counts them done → **silent loss.** (Reclaim itself doesn't depend on where
`committed` sits — it scans the `lease` table — so the failure is the broken
*guarantee*, not a broken reclaim.)

## Phase 6.5c — The exception window: park only failures


**1. Why must `Commit` free the lease *before* parking exceptions (and check it
still owns it)? What does a slow/reclaimed worker inject if it parks first?**

Parking is a plain `INSERT` with no ownership check of its own — nothing about the
statement knows or cares whether the worker calling it still holds the range's
lease. If `Commit` parked first and freed second, a worker that's already been
reclaimed (lease expired, a new owner is re-reading and re-processing the exact
same range under a rotated token) could still successfully write exception rows
for messages in that range — a stale worker injecting phantom failure rows into a
range someone else now owns and may be concurrently resolving differently. Freeing
first collapses "am I still the owner" and "give up ownership" into the same
statement: the token-guarded `DELETE` either matches (still owner, proceed to
park) or matches 0 rows (`ErrLeaseLost`, bail before touching `deliveries` at
all). There's no window between a check and an action for a race to land in,
because the check *is* the action.

**2. Why is there no `done`/`acked` state? When a happy-path message succeeds,
what row changes — and when an *exception* succeeds, what row changes?**

A `deliveries` row's existence is itself the "still needs attention" signal, not a
status value written onto it — so success is definitionally "no row," not a
terminal status. On the happy path (a message that never failed) success writes
nothing at all, the same 6.5a win of zero row writes per success. On the exception
path a row already exists (from the earlier failure), so `RecordExceptionSuccess`
**deletes** it rather than flipping it to some `done` state — the row's only
reason for existing was "needs tracking," and once resolved there's nothing left
to track. Both cases converge on the same rule, they just start from different
places (never-written vs. written-then-removed).

**3. What sits in the gap `(committed, claimed]` now — and why is it *not only*
the failed/in-flight work?**

Three things layered together: ranges under an open lease, offsets covered by an
unresolved `ready`/`inflight` exception, and — easy to miss — every
already-*succeeded* offset sitting **above** the lowest of those two blockers.
`committed` is a single high-water mark, not a bitmap, so it can only certify a
prefix; it has no way to say "everything succeeded except message 47." If message
47 is parked and 48–200 all finished cleanly, `committed` still sits at 46 —
48–200 are done and simply head-of-line-blocked behind 47's unresolved retry, even
though nothing is wrong with them. Quarantine (chunk 8) makes this concrete at
range scale too: once a whole range is dumped into the window, its perfectly-fine
sibling messages sit in the same gap as the one that's actually poison, only
distinguishable once each resolves individually via `ClaimExceptions` +
`RecordExceptionSuccess`/`RecordExceptionFailure`.

## Phase 7 — Routing


**1. Where does the routing decision execute, and why there rather than at
claim time or produce time? What changes if a binding is added after events
exist?**

At claim/fan-out time: inside `readMessages`'s `WHERE`, evaluated as part of
the claiming transaction, or inside `FanOut`'s `SELECT`, evaluated whenever
`FanOut` runs — never at produce time. `AppendMessage` writes `routing_key`
and never touches `binding` at all; a consumer evaluates the predicate
against whatever rows are in `binding` *right now*, not whatever existed when
the message was written. Consequence: a binding added after a message already
exists still applies to it, as long as that message hasn't been claimed
(CURSOR) or fanned out (LIFECYCLE) yet — verified live in `routinglab`, where
a message published before any binding existed still correctly matched a
binding added afterward. It has zero effect on anything already resolved:
already-`committed` offsets or an already-materialized `deliveries` row don't
get re-evaluated. Routing reach is bounded by what's still unclaimed/
un-fanned-out, not by publish order relative to when the binding was created.

**2. What can a depth-precise selector (NATS-style `*`/`>`) express that a
true wildcard can't — and does this system's routing actually need that?**

NATS-style splits `*` (exactly one dot-delimited token) from `>` (one-or-more
trailing tokens), so `orders.*.created` matches *only* a single token in that
slot — `orders.us.created` yes, `orders.us.central1.created` no (that needs
`>` to absorb the variable-length tail). A true wildcard collapses every `*`
to greedy `.*`, so there's no way to write "exactly this many segments, no
more" — depth becomes unpinnable. Nothing this system currently does depends
on that distinction (no phase needs to tell "this depth" from "any deeper"
apart), so the simpler true wildcard covers every real need so far; the
depth-precise upgrade path is documented and deferred in `TODO.md` rather than
built speculatively.

## Phase 8a — Retention: partition-drop, and the low-volume hybrid


**1. Why is partition-drop retention so much cheaper than `DELETE WHERE
created_at < X`? (Think WAL, vacuum, indexes.)**

Every `DELETE` is a transactional write to the WAL that has to be committed
and flushed, plus every index entry for that row has to be cleaned up, plus
the freed page adds pressure on vacuum. A partition drop is `DROP TABLE` — a
catalog operation, no per-row WAL, no index maintenance, no vacuum debt. Just
a disk-level removal of the whole relation.

**2. Retention is time-based — so why partition by `id` and not
`created_at`? What exactly goes wrong at claim time with 365 daily
partitions?**

`message_log` is append-only, so `id` is approximately time-ordered — retention
stays decidable per partition using `id` alone. Partitioning by `created_at`
instead would force the primary key to widen to `(id, created_at)` (Postgres
requires the partition key inside any PK), adding write/delete overhead for
no benefit, since nothing actually queries by `created_at`. Worse, every hot
read (the claim range, `MAX(id)`) filters by `id`, and the planner can only
prune partitions using columns in the `WHERE` — partition by `created_at` and
a claim's `id`-range query can't be pruned at all, so with a year of daily
partitions every claim probes all 365 of them instead of the 1–2 an
id-partitioned claim touches.

**3. The hybrid reintroduces `DELETE` — why doesn't it reintroduce the
problem partition-drop exists to avoid?**

Because the sweep never touches the active, high-volume partition —
`SweepExpiredPartitions` only walks the oldest surviving *non-active*
partition. At high volume, drop consumes whole partitions fast enough that by
the time a partition is old enough to sweep, it's already been dropped whole
— the sweep finds an empty prefix, not a `DELETE` under load. At low volume
there's no whole partition to drop yet, so the `DELETE`'s cost is what's
paying for correctness, and it's cheap exactly because the row count is small
by definition. The two mechanisms cover each other's weak end instead of both
running at once.

**4. What does the drop floor protect, and what precisely happens to a
consumer group when you turn it off and drop past its `committed`? (Kafka's
"consumer fell off the retention window," now in your own system.)**

The floor (`MIN(committed)` across `cursor`) protects a lagging group from
having unprocessed messages deleted out from under it. With it off, nothing
detects the gap — `FreshClaimMessagesWithCursor` advances `claimed` by pure
id arithmetic against `MAX(id)` (`claimed = LEAST(claimed + limit, MAX(id))`),
never checking whether rows still exist in that range. The lease still gets
created for `(low, high]` and `readMessages` still runs its `SELECT`; if the
partition backing that range is gone, the `SELECT` just returns fewer rows,
even zero, with no error. `claimed` and then `committed` both advance past the
hole exactly as they would for a normal batch — the group doesn't "jump
ahead" via any special-cased skip, it was always going to advance on
schedule. The dropped rows just silently never get delivered, and there's no
in-band signal that it happened — only an external one, like the Phase 5 lag
metric going quiet.

## Phase 8b — Per-topic tables: independent logs, routing stays within them


**1. Why does each topic need its own dense id sequence rather than sharing
the system-wide one? What specifically breaks if they share it?**

Cursors and partitions. When many topics share one sequence, each topic only
ever occupies a sparse subset of it — conflating what should be a per-topic
concern into a cross-cutting one. Retention is the clearest case: partition
drop decides "expired" by the timestamp of `MAX(id)` in a partition, and
under a shared sequence that max id could belong to any topic. Worse, with
the drop floor enabled, a single lagging consumer forces every topic to wait
on it, because `MIN(committed)` across `cursor` was scoped to the whole
datastore, not to the one topic that consumer actually lags on.

**2. Why do `cursor`/`deliveries`/`lease` need a `topic_id` added to their
keys, when they didn't need one before this phase?**

`lease` technically doesn't need it in its *key* — the lease `token` is
already a unique random id, so it disambiguates a row on its own. But every
table needs the *column* to make what it's tracking unambiguous: `cursor`
needs to know which `message_log_<id>` sequence a group's `claimed`/
`committed` actually refer to; `deliveries` needs it because a bare
`message_id` can point to completely different messages in two different
topics' tables once each has its own sequence.

**3. Why is topic registration explicit, when partition creation
(`EnsureNextPartition`) is allowed to self-heal silently?**

Topic registration creates a durable, lasting resource commitment — it
constructs a physical table and locks in configuration, some of it
immutable. Making that explicit forces a deliberate moment instead of
letting it happen as an incidental side effect of a produce/claim call,
lowering the chance of mistakes or drift. Partitions don't carry the same
risk: their naming is a strictly computed value (`id / partitionSize`), not
something a caller supplies, so there's no equivalent of a topic-name typo
silently forking a whole new resource into existence. Partitions are also an
implementation detail users generally shouldn't have to think about at all,
where a topic name is a first-class thing an application deliberately
chooses.

**4. `routing_key`/`bindings` survive this phase with their matching logic
completely unchanged — so what did splitting into per-topic tables actually
fix, and what did it deliberately leave unfixed?**

It fixed the cross-topic version of both problems named in the What/aha
above: a lagging group's drop floor and a compaction lookup's probe cost are
now bounded by one topic's own volume, not the whole system's. What it
didn't fix, on purpose: retention and partitioning are topic-scoped now, not
per-consumer or per-`routing_key`-slice — two slices sharing *one* topic
still share that topic's one floor. A lagging group reading only
`orders.us.*` still blocks a drop that `orders.eu.*` (same topic, different
slice) would otherwise be free to have happen. Re-scoped from system-wide to
within-one-topic, not eliminated; splitting into separate topics is the
deliberate, manual escape hatch if that ever becomes a real problem.

## Phase 8c — Log compaction: latest-per-key, filtered at claim time


**1. Why doesn't this design need a watermark-safe floor to gate
correctness, unlike Kafka's own compacted topics (and this repo's
`reference/waterline/compaction.go`)? What does the floor become instead?**

Because correctness is guaranteed at produce/write time — it's not an async
process that needs an additional correctness gate due to potential lag. The
floor for us is just the standard cursor `committed` value (not `claimed` —
`claimed` can regress on a crash/reclaim, `committed` is the crash-safe
frontier), and it's no longer a correctness gate: it downgrades to an
optional, whenever-convenient disk-space cleanup, decoupled from what a
claim can return.

**2. Why can a brand-new consumer group get latest-per-key on its very
first claim under this design, when a background-delete design can't give
it that for free?**

Because "latest" is guaranteed the moment the producer's transaction
completes, so the claim query always gets the latest id for a
`compaction_key`. A background-delete design has some amount of lag before
compaction is actually complete, so it has no strong guarantee you'll see
the latest — that depends on the size of the background-delete lag.

**3. Why does the filter search unboundedly for a key's latest write
instead of pinning to the claim's own high (`id <= $hi`)?**

Because the guarantee held for a compacted topic isn't "at-least-once per
message," it's "at-least-once per latest `compaction_key`." A bounded check
would only be wrong on reclaim specifically: a lease's `high` is pinned once
and reused on every retry, so after a crash a newer write landing outside
that fixed window would be invisible to a bounded check — the reclaimed row
would look "locally latest" within the stale window even though it's
actually been superseded. Unbounded means the check re-evaluates live
against current state every time, not the state pinned at claim time.

**4. Why is the `compaction_key` index partial (`WHERE compaction_key IS
NOT NULL`) instead of covering every row?**

Because `compaction_key` isn't the standard consumer setup, and a full index
would incur write overhead for no reason. (This index was dropped entirely
later in this same phase, once `latest_key` made it a dead consumer — this
answers why it *was* partial, not what exists in the final schema.)

**5. Phase 8b split every topic into its own physical table and its own
dense id sequence. Why does that help *this* phase's compaction lookup
specifically — what did a shared, system-wide `BIGSERIAL` cost a single
topic's own key-latest search before 8b existed?**

It doesn't matter for `latest_key` itself — that lookup is O(1) regardless
of partition count or sequence density by construction. It still matters
for the ground-truth scan underneath it, though: before 8b, proving a
negative meant scanning across every *other* topic's interleaved traffic
sharing the same `BIGSERIAL`, not just this topic's own volume. 8b bounds
that scan to one topic's own history — it just doesn't buy the index
anything, since the index sidesteps the scan entirely.

## Phase 9 — Consumer fault isolation & recovery


**1. Why does a recovered panic have to go through the *exact same*
retry/backoff/dead path as an ordinary error, instead of its own
special-cased handling?**

Answer: recovered panics are not necessarily permanent errors (nil map
write, index out of range, bad type assertion). The fact is we don't know
if a retry will help or not b/c we don't know the consumerFuncs code. So it
is better to go on side of caution and follow standard expected path
instead of making assumptions

**2. Why is `context.WithTimeout` alone insufficient to enforce
`WorkTimeout`, and what does the detached-goroutine race actually buy you
given Go has no goroutine kill?**

Answer: context timeouts expect to be explicitily handled. Normally via a
call to ctx.Err or ctx.Done. Our own internal code we can do that for.
However we cannot gauruntee that the user does that within their
consumerFunc. Because of that we have a detached-goroutine race that allows
us to internally exit the consumerFunc work such that we may retry or mark
dead within the users expected WorkTimeout + Grace period. The one caveat
this brings is that the goroutine that was raced is still running and as
such we have a abanonded routine which we track via metrics

**3. Why key the abandoned-goroutine registry by (message, attempt) rather
than by message alone?**

Answer: If first and second attempt of a message was abandoned. The second
attempt would overwrite the first within registry despite their
potentially being two real live abandoned go routines. message & attempt
is the uniquness identifier for the goroutine and as such should be the key

## Phase 10 — Observability: logging & the rollup model


**1. What's the tradeoff between a lazy periodic rollup and a synchronous
one — what do you gain and what do you pay for each?**

Answer: for lazy - its an async rollup so you have some lag between what
has actually been processed vs where committed sits. This lag causes
partition drop and deliveries sweep to have a few seconds of lag. However
b/c it is lazy the committed movement is off the hot path and so that
cursor movement does not slow or degrage throughput.
for synchronous - it is mostly the opposite. Partition drops and delivery
sweeps happen nearly right after committed changes (no lag) which better
shows exactly where committed is. but it is at the cost of an extra query
on the claim release hot path which slows down throughput. Specifically
this isn't just an extra query's fixed cost -- `Commit` today never touches
`cursor` at all (only `lease`/`deliveries`), so concurrent committers in
the same group commit fully in parallel right now. A synchronous rollup
adds an `UPDATE cursor` that those same committers now serialize on, which
is why the 20-worker case measured 1.3x-1.9x slower, not just the flat
+30-50% fixed-cost hit.

**2. Why does a live debug readout of claimed/committed/exception-count
matter even though the underlying data was always queryable in Postgres
directly?**

Answer: its a better developer experience, they don't have to know the
underlying typology they just call a method

**3. For each number in the metrics snapshot: which failure mode is it the
early warning for?**

Answer:
backlog - the classic consumer lag metrics. Means you are trailing behind
head which is normally not good.
exceptions dead - how many messages have truly failed, how numbers normally
indicate a bug or outage
abandoned total / self-clear - number of routine timeouts and how long they
take to resolve if they do. Can indicate not handled ctx close or async
code hanging
inflight (claimed-committed gap) - batches out for processing right now;
distinguishes rollup lag from real backlog
ready exceptions - retry queue depth building up
inflight exceptions - currently mid-retry
oldest unacked age - flags a single stuck message even when the counts
otherwise look fine
open leases - a crashed/never-committed consumer, exactly what
scenarioCrash in metricsreactionlab exercises
abandoned outstanding - goroutines hung right now, vs. total's lifetime
count

**4. Why does the OTel integration depend on
`go.opentelemetry.io/otel/metric` (the API package) but never the SDK or a
specific exporter like Prometheus's or Datadog's client?**

Answer: go.opentelemetry.io/otel/metric is only api code ie very light not
many dependencies go.opentelemetry.io/otel has a lot of extra code and
dependencies that make this library heavier

## Phase 11 — Architecture cleanup: datastore boundary & producer API


Deliberately skipped for this phase — see Decisions.

## Phase 11.5 — Admin surface, migrations-into-code & control-plane CLI


**1. Why `AlterConfig` pointer-per-field, not `topic.Config` reuse or
every-field-required?** The failure a sparse patch prevents is **silent
clobbering of a live value to zero**. `topic.Config` uses zero-means-default
semantics (`WithDefaults` fills unset fields before the row is written) — fine
for create, useless for patch, because a value field can't tell "caller wants
RetentionTTL = 0" from "caller didn't mention RetentionTTL." A full-replace
struct forces the operator to restate every field to change one, and any they
forget becomes zero — and zero is a *real, often destructive* setting here
(RetentionTTL 0 = keep forever; a tuned JanitorSweepBatchSize silently reset).
"Require every field" isn't even enforceable in Go: a missing struct field is
a silent zero, not a compile error, so the destructive version is the one the
language hands you by default. Pointers give tri-state — nil = leave, non-nil
= set (including set to zero) — so a forgotten field is nil → no-op. Same
mistake, opposite blast radius: sparse fails safe, full-replace fails
destructive.

**2. Why does `COALESCE($n, col)` keep the current value, and why beat a
dynamic SET list?** pgx encodes a nil pointer as SQL `NULL`, and
`COALESCE(NULL, col)` returns `col` — the stored value untouched; a non-nil
param is a concrete value (even `0`, which is not NULL) and overwrites. So the
"leave alone" case lives in the SQL as NULL rather than in Go as
string-assembly. That keeps the statement fully **static** — the same six-column
SET every call — so it reads like every other query in the file, prepares/plans
once, and drops the placeholder-counting closure the dynamic version needed.
The subtlety it gets right for free: an *explicit* zero (RetentionTTL back to
forever) still lands, because COALESCE branches on NULL, never on zero-value.

**3. Why is `PartitionSize` immutable, what breaks, and what's the unlock?**
Partitions are created `FOR VALUES FROM (n*size) TO ((n+1)*size)`, and the
runtime computes partition *identity* from `head/partitionSize` at every hot
site — producer `ensureCoveringPartition`, consumer `EnsureNextPartition`,
`DropExpiredPartitions`, `dropPartition`'s delivery-cleanup range, sweep
batching. Name and bounds are the same number viewed through `size`. Change
`size` mid-life and `head/size` names a partition whose real on-disk bounds no
longer match — wrong partition dropped, overlapping-partition CREATE errors, a
message routed to a table that doesn't cover its id. It's immutable by
construction (absent from `AlterConfig`), not by a runtime check. Settled
unlock (TODO.md "dynamic partition bounds"): stop computing bounds, read the
real ones from `pg_inherits` + `pg_get_expr(relpartbound)`; keep `_<n>` naming
but never reconstruct a name from math — use the `(relname, lower, upper)`
triples as handed back; mint each new partition at `from = max existing upper
bound, to = from + current size`. That yields Kafka `segment.bytes` semantics:
altering size touches only *future* partitions. Deferred because it rewrites
every hot partition-math path for an admin nicety, and it's fully
backward-compatible (existing bounds already sit in the catalog).

**4. Why `WHERE id = $1`, not `WHERE name = $1`, for a rename?** Retry safety
under ambiguous commit. `DatastoreRetry` reruns on an ambiguous outcome — a
connection killed mid-commit, where "did it land?" is unknowable. If the first
attempt *did* commit the rename and the retry runs `UPDATE ... WHERE name =
oldName`, no row carries `oldName` anymore → zero rows affected → the code
reports `ErrTopicNotFound` for a rename that actually **succeeded**. The name
is the very thing being mutated, so it's the worst possible key across a
retry. So `renameTopic` reads the row first to pin the immutable `id`, then
keys the UPDATE on it: the retry re-applies to the same row (sets name to
`newName` again — an idempotent no-op) and returns success.

**5. Why "latest-by-`id` where `status='success'`," not `MAX(schema_version)`?**
A downgrade (intentional release rollback) inserts a row with a *lower*
`schema_version` than current. Under `MAX(schema_version)`, that rollback row
is invisible — MAX still returns the higher pre-rollback number — so the system
would believe it's still at the new version, never re-apply the up step on a
later roll-forward, and every status readout would lie. Latest-by-`id` follows
insertion order regardless of the version value, so the rollback row becomes
current and history reads truthfully ("went to v4, rolled back to v3"). MAX is
only correct if versions monotonically increase, which downgrades break by
design.

**6. Why xact-scoped lock for `RegisterSystem` but session-scoped for a
`Migrate` run?** `RegisterSystem` is one short transaction —
`pg_advisory_xact_lock` auto-releases at commit/rollback, so it can't leak and
holds for the minimum time. A `Run` walks *multiple* per-step transactions
(each step commits its DDL + version stamp atomically). A txn-scoped lock would
release at the **first** step's commit, letting a concurrent migrate interleave
between steps — precisely what the lock exists to prevent. So `Run` takes a
SESSION-scoped `pg_advisory_lock` on a pinned connection, held across all the
steps' transactions and released explicitly (or on connection death). Swap
them and both failure modes appear: a session lock on `RegisterSystem` must be
manually released (leak risk on an error path), and a xact lock on `Run` loses
inter-step serialization — two migrations could both proceed and corrupt the
version sequence.

**7. Why a nested `go.mod` for the CLI, and what does a consumer avoid?** A
directory with its own `go.mod` is a *separate module*, excluded from the
parent's package graph entirely — so the root library module never imports
cobra/fang/lipgloss; they're absent from its `go.mod`/`go.sum`. A consumer
running `go get github.com/agentstax/vulkan` gets **zero CLI dependencies in
their `go.sum`**. That's stronger than "the CLI code isn't compiled in"
(unimported packages never ship either way) — it's the module-graph / go.sum
pollution that a shared module can't avoid: no transitive cobra version
constraints leaking into their build, no CLI supply-chain surface. The cost:
the CLI versions/releases independently, and local dev needs a `go.work` to
resolve both modules — gitignored, because committing it would force the
workspace on every consumer/CI checkout. Deliberately *not* a `replace`
directive, which would break `go install ...@version` for everyone once
published.

## Phase 14a (schema evolution) — Epoch-versioned topics, `CompactionRank`, `MessageMeta` & the bridge pattern


**1. Why is a schema-evolution epoch a new physical topic (own log, id
space, duties) instead of a version column on `message_log`?**

**2. Why generalize compaction's winner rule to `max(compaction_rank, id)`
rather than, say, giving the bridge a way to write with a backdated
`created_at`?**

**3. Why is `CompactionRank` signed, and what specifically breaks if the
bridge could only write at rank 0 or higher?**

**4. `MessageMeta` carries `Id`, `RoutingKey`, `CompactionKey`,
`CompactionRank`, `CreatedAt` — why does it deliberately NOT carry the
idempotency key?**

**5. `FamilyHealth` never reports `Safe: true` for a compacted topic, even
once a bridge has actually finished migrating it. Why is that the correct
behavior rather than a gap the library should close?**

**6. Why is the bridge a plain user-space consumer group instead of a
library-provided `BackfillTopic` verb?**

**7. Why does the bridge's `IdempotencyKey` need to be derived
deterministically from the source message's id (UUIDv5-style) rather than
generated fresh per attempt?**

## Phase 14a (worker system) — `worker`/`worker_instance`, the manager & the consumer inversion


**1. Why is `target_instances` a single number instead of the original
`min_instances`/`max_instances` pair?**

Answer: the claim gate reads exactly one question — "are there already
`target` live instances?" — so one number is all the mechanism needs.
Min/max are rails on whoever MUTATES the target (an alter surface or
autoscaler), and neither exists yet; carrying their columns now would be
schema for a feature that doesn't exist. 0 doubling as "suspended" also
only works because target is a single declarative number.

**2. Why must the instance-slot claim be one atomic statement?**

Answer: the claim is "count live instances, and insert mine if count <
target." Split into two statements, two registering processes can both read
count = target−1 in overlapping snapshots and both insert — the classic
read-then-write race, the same class AdvanceWaterline hit (and fixed by
reading claimed+leases in one snapshot). One statement makes the count and
the insert see the same snapshot, so overshoot is unrepresentable.

**3. The factory invariant: why must `New*` never touch the DB, and why
does `Register` RETURN the product instead of mutating the factory?**

Answer: a constructor that does I/O can fail for operational reasons, which
turns every construction site into an error-handling site and makes the
value's validity time-dependent. With `New*` pure, validation is the only
failure and the factory is reusable forever. Register returning an instance
(instead of flipping internal state) means one factory can register many
independent lives, a dead instance is replaced by calling Register again —
wound-down-stays-down is gone — and callers hold exactly the thing whose
lifecycle they manage.

**4. Why does the manager respawn ONLY on `ErrInstanceLost` and propagate
every other error?**

Answer: losing a claim is expected weather — a lease expired under a stall,
another process won arbitration — and the healing response is to reconcile
and re-claim; it says nothing about the code being broken. Any other error
from a spawned instance's Run is a real fault, and silently respawning
would turn a crash loop into an invisible hot loop. Upkeep workers only
exit on loss or cancel, so in practice only consumer-type instances can
surface a fatal error.

**5. Why was `EnsureNextPartition` deleted without a replacement home?**

Answer: partition creation is provisioning, not cleanup — it belongs to the
write path, the way Kafka's writer rolls segments; the janitor is cleanup
only. Correctness never depended on the create-ahead: partition 0 exists
from RegisterTopic, and the produce path's reactive ensureCoveringPartition
heals every later boundary (and had been the only live creator since chunk
5 anyway). Proactive create-ahead returns, if ever, as a producer-side
sentinel-id trigger — best-effort by design, because the reactive heal is
the only layer allowed to matter for correctness.

**6. Why one system manager row (and no per-topic manager rows)?**

Answer: nothing would ever claim a per-topic manager — the daemon runs at
deployment scope, and the per-topic kill switch already exists as the
topic's own janitor row. The system manager row earns its place twice over:
it is the daemon's claim anchor (N daemons arbitrate over it with the same
instance machinery as everything else) and the deployment-wide suspend
switch. System scope then costs one clause in listWorkers — only a system
owner has both topic and group unset, and every worker row resolves to its
system through the topic join.

**7. Why did the producer's `lifecycleCtx` get dropped, and what did that
trade away?**

Answer: after the factory split, Register is a stateless build step — its
ctx bounds only that call's I/O, and the batcher already refuses a
cancelled ctx before enqueue. A stored lifetime was therefore a pure
admission gate duplicating what each produce call's own ctx already
expresses. Dropping it deleted a whole error family
(ErrShutdownRequested, DisableGracefulShutdown, Done()==nil checks). The
trade: a caller producing on context.Background() is no longer refused
during app shutdown — the same contract as any database call, which is
exactly what a produce is.

## Phase 14a (consumer layering) — the layered package pattern & the pkg/consumer refactor


**1. Why does ALL input validation live at the controller, with datastores
trusting their inputs?**

Answer: the controller is the only door — every call path goes through it,
so validating there is once-per-operation and can return typed errors in
domain vocabulary. Re-checking in the datastore would be a second copy of
the same rules that drifts independently (and the datastore can't produce
good errors anyway; it only sees shaped data). One deliberate exception
survives: guards on values the DATABASE produced, like the cursor
`low >= high` check — that catches a cursor row gone backwards, not bad
caller input, so it stays beside the SQL that read it.

**2. Why table-exact `*Data` structs plus `to*` adapters, instead of
scanning straight into the vocabulary read-models?**

Answer: it pins each layer to the thing it actually models. `*Data` mirrors
the table (nullable columns, ns-integer durations, string enums), so a
schema change is visible in exactly one file; the vocabulary type carries
resolved Go-native meaning (time.Duration, validated enums, coalesced
owners). Scanning into vocabulary types smears column handling across every
query and forces the read-model to carry database shapes. The adapter is
also where stored values get re-validated on the way OUT (an unknown enum
in a row errors the read instead of silently misbehaving).

**3. Why did MessageMeta need a home BELOW the rows, and why was pkg/common
rejected for it?**

Answer: the rows stamp meta into ctx under an unexported key and
MetaFromContext must read that same key back — duplicated per row, the keys
are different values and the lookup returns false. So it needs exactly one
package the rows all import, below them and still user-reachable.
pkg/common was rejected because it is for what BOTH sides need, and meta is
consumer-only — verified by checking the producer's options usage: it only
ever Fills defaults, never Clamps or resolves concurrency. The controller
was rejected because it would put the persistence door inside a user's
callback (`consumercontroller.MetaFromContext(ctx)`).

**4. Why do the rows get their own narrow configs while WithDefaults stays
at the door?**

Answer: measured usage said the rows aren't using the whole config —
messageconsumer reads 12 of ConsumerConfig's 21 fields, exceptionconsumer
7, deliveryconsumer 4 — so handing each row the full struct hides what it
actually depends on. But the DEFAULTS can't move down: the derivations are
interdependent (QueueSize from BatchLimit, MessageMax from Message,
ShutdownTimeout from MessageMax.Timeout + grace), so they must resolve once
against the whole config. Door resolves, rows receive slices; that also
fixed the live smell of three constructors re-running WithDefaults+Validate
on an already-resolved config.

**5. `ConsumerDatastore[Message any]` was deleted as a phantom type
parameter. What made it phantom, and why is removing it a correctness-shaped
cleanup rather than cosmetics?**

Answer: the parameter appeared in zero fields, signatures, or bodies — the
payload is json.RawMessage end to end and unmarshalling happens in the
rows. A phantom parameter still monomorphizes the API: every caller must
name a type argument (16 lab call sites), two datastores instantiated with
different M are different types even though they are behaviorally
identical, and the signature claims a type-dependence the code doesn't
have. Deleting it makes the persistence layer's real contract — bytes in,
bytes out — visible in its type.

**6. Why collapse MessageException / MessageTerminal / MessageSuperseded /
MessageDeferred into one kinded MessageOutcome?**

Answer: they were structurally identical ({MessageId, Err}), constructed in
exactly one place, and consumed only by Commit/PartialCommit — four names
for one shape whose only difference is WHICH outcome it is, i.e. data, not
type. One type carrying a kind collapses both four-armed switches into a
single walk and shrank Commit from 10 params to 7 and PartialCommit from 11
to 8. Four types would earn their keep only if the variants diverged
structurally or needed compiler-enforced separation; they don't.

**7. Controller read-models dropped the `*Row` suffix (MessageRow →
Message) while datastore kept `*Data`. Why is that split right?**

Answer: the controller layer's whole job is abstracting the database away,
so names that say "row" leak the thing being hidden — and this exact naming
had already been rejected once, and the conventions rule is that a bad name
that turns out to be a pattern gets fixed everywhere. In the datastore,
row-shaped names are the point: `*Data` structs ARE the table row, and
saying so is the honest contract of that layer.

## Phase 14a (cron) — `cron_job`, the scheduler worker & derived job-request status


**1. The scheduler produces every due job in its own transaction (`FOR
UPDATE SKIP LOCKED` recheck → ProduceInTx → advance in one UPDATE) instead
of one shared tick transaction. What two failure modes does the shared-txn
shape have that the per-row shape avoids?**

Answer: two failure modes. First, blast radius: one poisoned row (say a
schedule column corrupted to garbage) errors its produce, and in a shared
transaction that rolls back EVERY job's produce for the tick — and since the
error backs off the worker, one bad row stalls the entire scheduler forever.
Per-row transactions make a row failure a WARN + skip; siblings produce.
Second, lock hold: ProduceInTx takes the topic's consumer-progress lock and
its own doc says to call it LAST — a shared tick txn would hold that
whole-topic lock from the first produce until the end of the tick, blocking
consumer commits for as long as the tick runs.

**2. After downtime, the scheduler walks `for Next(t) <= db_now { t =
Next(t) }` and produces only the final `t`. Why is producing every missed
scheduled time the wrong default, and what bounds how stale the produced
time can be?**

Answer: backfilling missed times is almost never what an operator wants
from downtime — k8s CronJobs made the same call — and here it would be extra
wrong: the topic is compacted on the job id, so a burst of stale requests
would supersede each other instantly, and a keyed consumer only ever sees
the newest anyway. Producing the newest due time is the same drop-missed
semantic with zero waste. The bound: the walk lands on the last due time
before db_now, so the produced ScheduledTime trails now by at most one
schedule gap — after any length of downtime.

**3. The idempotency key packs the scheduled time's unix ms into the v7
time bits and the job id VERBATIM into the payload bits. Why must the job
id not be hashed, and what property makes a fresh v7 per `RunCronJob` call
the correct opposite choice?**

Answer: the idempotency table is shared per topic, so the key must be
globally collision-free across jobs. Hashing the id into the payload bits
means two different jobs due in the same millisecond can collide, and a
collision is silent — the second job's request is treated as a duplicate
and swallowed. The id verbatim makes cross-job collision impossible (an
int64 fits the payload bits exactly). Run-now is the opposite contract: it
must NEVER dedupe against the schedule's request for the same moment — a
manual run is deliberately its own request — so it takes a fresh random v7,
and only its own ambiguous-commit replay can dedupe it.

**4. Why does `job_requests` need `DeliveryLogModeAll` for status to be
derivable at all, and why doesn't every topic pay that cost?**

Answer: on the normal path, success is recorded by DELETING the delivery
row and writing nothing — deletion leaves no row to derive from, so "this
group ran this request successfully" is unknowable after the fact. Mode
'all' writes a 'success'@attempts log row inside the same success
transaction, which is exactly the positive record the status join reads.
Every topic doesn't pay because that is one extra row per successful
delivery — on a hot topic that erases the O(1)-per-range success-cost story
— while job_requests produces at most one message per job per minute, so
the volume is noise.

**5. A run-now request defaults to Concurrency 'allow'. Explain why an
'allow' request can never be blocked by (or block) a running request, in
terms of what actually takes and holds a key lease.**

Answer: the key lease is only ever touched when dispatching a 'defer'
message — the dispatch gate acquires it, the outcome transaction releases
it. An 'allow' message bypasses the gate entirely: it never attempts the
acquisition, so a held lease is invisible to it (it runs beside the
holder), and it never holds the lease itself, so nothing that later checks
the lease can be waiting on it. Blocking is a property of the lease, and
'allow' simply never participates in the lease.

**6. In the outcome classification, `!Head` only means "superseded" because
`Succeeded` and `Raised` are checked first. What real scenario would be
misreported if the `!Head` case came first?**

Answer: any request that actually ran and was later replaced. Take an
@every-1m job with one consumer group: request 101 runs at 12:00 and
succeeds; at 12:01 the scheduler produces 102, which takes the head. With
`!Head` first, 101 now reports "superseded" even though the delivery log
holds its success row — every completed request would flip to superseded
one schedule gap after finishing, erasing the history the log still proves.
Succeeded/Raised first means a terminal outcome is permanent; `!Head` only
speaks for requests that never ran.

**7. `GroupStatus.Superseded` is per group even though supersession happens
per key. Give the concrete case where the same request counts as Ran for
one group and Superseded for another, and why both are correct.**

Answer: two groups bound to the same job name, one fast and one slow (or
simply not running). Request 101 is produced; the fast group claims and
runs it while it is still the head — success row, so 101 counts in Ran and
Succeeded for that group. Before the slow group ever claims it, the
scheduler produces 102 and the head moves; the slow group can now never
claim 101, so for it 101 is Superseded. Both are correct because delivery
is per-group: each group receives its own copy of the request, and one
group's execution does nothing for another group's.

**8. Unsuspend re-seeds `next_scheduled_time` from now() instead of keeping
the stale value. What surprising behavior does the stale value cause, and
which system's semantics does the re-seed copy?**

Answer: next_scheduled_time freezes at suspend, so after a month
suspended it is a month stale; if unsuspend kept it, the very next tick
would see it due and immediately produce a request stamped with a
month-old scheduled time — a surprise run of the past the moment an
operator flips the switch. Re-seeding from now() means unsuspend resumes ON
SCHEDULE, producing nothing until the next genuinely-due time. That copies
k8s spec.suspend semantics (unsuspending a CronJob doesn't fire the missed
runs).

**9. Why is a superseded *pending* request invisible to delivery_log (no
'superseded' row is ever written for it), while a dispatched-then-outraced
defer request DOES get one?**

Answer: because nothing ever happens to it. The claim query only returns
a keyed message while it IS the compaction head, so a pending request that
loses the head is simply never claimed — there is no delivery event, and
delivery_log records delivery events, so writing a row there would invent
one (and pay a write per superseded message on compacted streams, for a
fact message_log + compaction_head already prove — one mechanism per
fact). The dispatched-then-outraced defer request is different: it WAS
claimed, and losing the head race at dispatch is that attempt's real
outcome, so the 'superseded' log row is the standing invariant doing its
job — no attempt number ever vanishes from the log.

**10. The status/requests datastore reads were rebuilt from one CTE
statement into four flat per-fact queries composed in Go, fetched per
consumer group. What made the single statement wrong beyond taste, and what
consistency property was deliberately given up in the split?**

Answer: the statement welded four independent facts into one query — which
groups match the name, which messages belong to the job, which is the head,
and what each group did — forcing SQL-side machinery for what plain Go does
better: jsonb destructuring for a payload field the controller can
unmarshal, a CROSS JOIN to build the (request, group) matrix, a LEAD window
for successor attribution that in Go is the slice's previous element, and a
matching_group CTE duplicated verbatim across two verbs. Rebuilt, each fact
is one flat query and the composition is a loop. Given up: the reads no
longer share one snapshot, so a request produced mid-read can skew a single
listing — accepted for a status view, and explicitly not defended with a
repeatable-read transaction (user call).

## Phase 14a (alerts) — the default checks, `classify` & the `__system.alerts` executor


**1. A run that finds nothing still calls `Record(ctx, name, owner, nil)`
for every owner instead of skipping the produce path. What breaks if the
nil finding short-circuits before classify?**

**2. `NewAlertController` rejects `repeat >= retention`. Walk the failure
that invariant prevents: what physically happens to an active alert's head
when the repeat interval outlives the topic's retention TTL?**

**3. The resolve arm builds the published alert from the HEAD's fields
(name, owner, severity, message) rather than from anything the run
produced. Why can't the run supply them, and what must carry over from the
head for the key and the log line to be right?**

**4. Why is the WARN/INFO logging restart-proof with zero in-process
state? Name the exact comparison that makes an edge an edge, and where
both sides of it live.**

**5. One consumer group per check, bound to exactly its job name — a
single `alert.*` consumer switching on job name was built and killed.
What does the binding table already own that the switch re-invents, and
what per-check operations fall out of the split for free?**

**6. The executors were first hosted as errgroup goroutines inside
`SystemManager.Run` and rebuilt as worker definitions the manager claims.
List what the worker rows bought that the goroutines could not.**

**7. The register-time pass runs with threshold 0 (derive live) and is
log-only. Why must a Register path never write to the alerts topic, and
why does derive-live keep the pass silent on a healthy system?**

**8. In `evaluateTopics` one owner's failure is joined and the loop
continues. Trace what that failed attempt does to the job request's
delivery, and why the eventual retry is safe for the owners that already
succeeded in the failed attempt.**

**9. The condition (`Evaluate`) lives once, in each check's controller,
and producer/consumer INJECT it — the first build copied the
queries/thresholds/texts into each side's datastore per the "each side's
datastore" convention. What marks that convention as misapplied here?**

**10. alertlab suspends both check jobs while its executor runs. What
spawns a cron scheduler inside the lab process, and exactly how did a due
@hourly request make the lab's run-now request unclaimable?**


---

# Part 2 — answers written inside LEARNING_PLAN.md itself

Some phases' answers were written directly in LEARNING_PLAN.md (deleted
2026-08-13; full file in git history) rather than NOTES.md. Every
Explain-it-back block from that file containing an answer is archived below
verbatim, under its original phase heading.

## Phase 1 — The durable atom: append + atomic claim ✅

**Explain it back** (from memory, no peeking):
1. Why does the `DELETE` have to be in the same transaction as the claim? Walk
   through what can go wrong with each of the two orderings if it's separate.
Answer: If delete is not in tx then the delete command could have a network blip error and we end up with completed work that is 'retried' essentially.
If delete is handled before processing and the worker crashes mid process the work is lost forever and never handled (worst case)
2. A worker is killed with `kill -9` mid-process. Step by step, what does
   Postgres do, and when does the row become claimable again?
Answer: When the connection is closed without a committed transaction it is assumed failed and is rolled back
3. What does `SKIP LOCKED` change about the query's *result set*, exactly? Why
   is that safe here when skipping rows would normally be a correctness bug?
Answer: a locked row can be assumed to be a row 'in process'. Because we don't want to double process work this is correct functionality


## Phase 2 — Per-message lifecycle (the part you care about most)

**Explain it back:**
1. In Phase 1, what held the claim? In Phase 2, what holds it? Why did it have
   to change? (Hint: what would a 10-minute job do to a Phase 1 transaction?)
Answer: Phase1 the db lock, Phase2 the db row data (status='processing' and locked_at).
A long running job in phase1 would hold open a transaction the entire lifecycle. With high concurrency a huge number of connections would remain open which is not scalable. With phase2 we have a millisecond lock and instead rely on queries and row data to understand what is 'locked' vs not.
2. Walk the full state machine including every transition's trigger.
Answer: 
- 1. Select and lock work from queue
- 2. Update work to 'processing' -> release lock
- 3. Do consumer job on work
- 4a. If success record success on work row in db
- 4b. If failure:
  - i. If has attempts left -> retry work
  - ii. If no attepts left -> mark as dead (do not retry)
3. Why does lease reclamation make delivery at-least-once rather than
   exactly-once? What property must the consumerFunc now have?
Answer: If the consumerFunc takes longer than the lease than another concurrent worker could pickup the work as it is considered reclaimable
To fix that the lease timeout must be greater than the consumerFunc timeout OR the consumerFunc must be idepotent


## Phase 3 — Competing consumers & batching 🔨

**Explain it back:**
1. Why is the partial index so much better than a full index on `(status, run_at)`
   for this workload?
Answer: I only seeded around ~1000 and it is already apparent. The main difference is between a bitmap heap scan (with partial indexes) vs a full sequential scan (without indexes). Full scans are just much slower than a map lookup. In the actual time case with ~1000 rows it was .215 (without index) and .05 (with index) and this read difference I would assume only grows when not using index
2. Batch claiming in Phase 1 had a failure-domain problem. Why doesn't Phase 3's
   batching have it?
Answer: I'm not 100% sure but I do know with Phase1 we held the claim / lease via a lock at the db level. While this is effective it is not scalable. Each in process claim holds open a connection the entire time processing is occurring. For a long running job with many concurrent workers and many consumers this is a resource nightmare for the postgres database.
3. What was your measured ceiling, and what do you think the bottleneck was —
   lock contention, WAL, round-trips, or the worker code itself? How would you
   tell?
Answer: you did this analysis but with current topology acks are the bottleneck. With a single ack per commit the full roundtrip is costly. We could batch commits but with upcoming changes in topology soon it may not be worth it.
4. Why must the in-memory buffer stay shallow? Walk through what goes wrong with a
   deep prefetch buffer that did *not* go wrong for the scrape queue in
   `examples/simple`.
Answer: A deep prefetch buffer means that claimed work and their associated leases will regularly be lost while waiting in the buffer. While their is logic to handle this it is not perfect and can lead to excess double processing. Additionally it is just extra unnecessary work the pressure queue only needs to have a shallow buffer that hides or masks the claim sql latency such that it improves throughput.


## Phase 3.5 — Throughput: the commit wall (measure, don't over-build)

**Explain it back:**
1. Why is the fsync-per-commit the throughput wall, and why is the *ack* (not the
   claim) the half that's hardest to amortize in the queue model?
Answer: fsync-per-commit flushes data from mem to disk for every commit. The difference between operations in-mem vs on disk is costly in time. Because our current architecture makes an ack per work, unlike our claim process which does batching, a large amount of time is spent fsyncing / disk writing. Turning fsync off trades commit durability for speed however that is not an issue as we have a reclaimation process and a at-least-once fire policy. So the durability risk is not there. The intresting part is that at low throughput / concurrency fsync gives huge gains while at high throughput / concurrency it is more modest. That is because postgres automatically batches the fsync disk writes at high commit throughput so the fysnc off setting does less.
2. Why does the at-least-once contract make `synchronous_commit=off` a free lunch
   here when it wouldn't be for a bank ledger?
Answer: At least once means un ackd work CAN be processed again, double processing or technically even more. This does imply consumers must be idepotent but it allows us to lose unackd work because we will simply try it again via our reclaimation process. However for bank ledgers where exactly-once is required this is not a good fit. These normally need distrubuted transactions (or something like this)
3. Which of the four levers survive the Phase 4 topology change, and why do the
   other three dissolve or relocate? (This is the real point of the phase.)
Answer: synchronous_commit=off survives. Archiving rows is not needed for optimization as an indexed cursor will replace it. Batch acks are not needed because the cursor is the lifecycle tracker. Not sure what the third one is.


## Phase 4 — The log/queue split: retention + replay

**Explain it back:**
1. What exactly can a cursor not express that per-row status could? Give the
   concrete failure scenario.
Answer: per-row lifecycle. If a row fails you either have to stop / exit OR skip it.
2. Why does replay cost nothing extra in this design? What Phase 1 decision
   would have made it impossible?
Answer: because we have decoupled messages from reading position and messages are an append only log, you can freely process from any position in the log just by changing the cursor position. Phase 1 could never do this because we delete messages after processing.
3. When the consumer crashes *after* processing but *before* the cursor
   update, what happens on restart? What delivery guarantee does that imply?
Answer: In this case it would retry the already processed message. To that extent this is an at-least-once guarantee


## Phase 5 — Fan-out to independent consumers

**Explain it back:**
1. Why is fan-out structurally impossible in the Phase 1–3 design?
Answer: lifecycle is directly tied to the message log in phase 1-3 meaning it is a one-to-one mapping. Once it is processed by something anything else will also consider it processed. New design is one-to-many
2. What's the operational risk of a consumer group that's permanently slow,
   once retention (Phase 8) exists? (This is Kafka's "consumer fell off the
   retention window" failure.)
Answer: This is consumer lag at its extreme. This would mean it risks messages not being processed at all


### 6.5a — Claim-from-log: the happy path

**Explain it back:**
1. Phase 6 wrote one row per (group, message); this movement writes none. Where,
   exactly, did the write amplification go — and what now carries the "this offset
   succeeded" fact instead of a row?
Answer: The cursor now carries the successfull messages via the committed waterline. Anything past this waterline can be considered in a terminal state.
2. What do `claimed` and `committed` each mean, and — in this single-worker,
   no-failure happy path — how do they relate? (The gap between them only opens in
   6.5b/6.5c; you'll revisit what lives in it there.)
Answer: Anything before committed is considered in a terminal state (success only right now). Anything between committed and claimed can be considered 'in-flight'. And anything past claimed can be considered 'waiting'.


### 6.5b — Lease the range: crash recovery

**Explain it back:**
1. A worker crashes mid-range. Walk the recovery step by step. Why does the
   reclaimer **rotate** the lease token, and what goes wrong if it merely refreshes
   `lease_until`?
Answer: Worker crashes mid-range -> Lease is 'lost' -> Lease expires -> worker reclaims on new claim cycle. Just bumping lease_until means we still have the wrong token owner so the worker does not own that claim anymore
2. What does an open lease do to `committed`, and why must it — what breaks in 6.5a
   if the waterline advances past an in-flight range?
Answer: An open lease prevents committed from moving past its low range. If we advanced committed past the leases low then we can no longer reclaim a lease if worker crashed mid lease.


### 8a — Retention: partition-drop, and the low-volume hybrid

**Explain it back:**
1. Why is partition-drop retention so much cheaper than `DELETE WHERE
   created_at < X`? (Think WAL, vacuum, indexes.)
Answer: Every delete is a transactional write to the WAL which then has to be committed / flushed.
Additionally indexes have to be cleaned up as well and then of course each page has to be deleted which is pressure on the vacuum.
With partition drop none of those things happen it is just a pure disk delete.
2. Retention is time-based — so why partition by `id` and not `created_at`?
   What exactly goes wrong at claim time with 365 daily partitions?
Answer: because message_log is append only, id is approxametly time ordered. Because of that we can use id to our advantage.
Time based partitions require our table primary key to include created_at which adds write/delete overhead.
Additionally if we did time based partitions with a ttl of a year claim queries would have to scan 365 partitions which would slow down the hot path
and degrade throughput quality.
3. The hybrid reintroduces `DELETE` — why doesn't it reintroduce the problem
   partition-drop exists to avoid?
Answer: Because the sweep never touches the active, high-volume partition — SweepExpiredPartitions only walks the oldest surviving *non-active*
partition. At high volume, drop consumes whole partitions fast enough that by the time a partition is old enough to sweep, it's already been
dropped whole — the sweep finds an empty prefix, not a DELETE under load. At low volume there's no whole partition to drop yet, so the DELETE's
cost is what's paying for correctness, and it's cheap exactly because the row count is small by definition. The two mechanisms cover each other's
weak end instead of both running at once.
4. What does the drop floor protect, and what precisely happens to a consumer
   group when you turn it off and drop past its `committed`? (Kafka's
   "consumer fell off the retention window," now in your own system.)
Answer: A drop floor protects against messages not being processed. If we didn't have floor protection partitions or messages could be deleted before a
cursor / consumer group has reached them.
Precisely: nothing detects the gap. FreshClaimMessagesWithCursor advances `claimed` by pure id arithmetic against MAX(id)
(`claimed = LEAST(claimed + limit, MAX(id))`), never checking whether rows still exist in that range. The lease still gets created for
`(low, high]` and readMessages still runs its SELECT — if the partition backing that range is gone, the SELECT just returns fewer rows, even
zero, with no error. `claimed` and then `committed` advance past the hole exactly as they would for a normal batch. So a lagging group doesn't
"jump ahead" via any special-cased skip — it was always going to advance on schedule; the dropped rows just silently never get delivered, and
there's no in-band signal that it happened (only an external one, like the Phase 5 lag metric going quiet).


### 8b — Per-topic tables: independent logs, routing stays within them

**Explain it back:**
1. Why does each topic need its own dense id sequence rather than sharing
   the system-wide one? What specifically breaks if they share it?
Answer: Cursors and partitions. When many topics share a sequence id they each have a subset of the full sequence conflating what should be
topic concerns to cross cutting concerns. For example retention: a system-wide id forcing retention to also by system-wide because of how we drop partitions
by looking at the timestamp of the max(id) in a partition. that max(id) could come from any topic. Additionally if a lagging consumer exists and we have 'don't 
drop past floor' functionality enabled we are forced to wait on that lagging consumer for EVERYTHING which is scoped to the entire datasource instead of just a
topic due to min(id) of cursors being system wide.
2. Why do `cursor`/`deliveries`/`lease` need a `topic_id` added to their
   keys, when they didn't need one before this phase?
Answer: technically leases does not because they token is bound to whatever entity that claims it ie group/topic consumer. But in general it is to make these entities unambiguous. Cursors needs to know which message_log id sequence (ie topic) they are keeping track of. Deliveries needs to know the same because a message_id could be very different messages in different message_log tables.
3. Why is topic registration explicit, when partition creation
   (`EnsureNextPartition`) is allowed to self-heal silently?
Answer: topic registration creates durable lasting user concerns. It has to construct a table and manage configuration some of which is immutable. Making a topic creating explicit forces the user to take a second and think through what they want and lowers the chance of mistakes or mismanagement. Partitions are abstracted away
constructs that users in general don't need to be concerned about and thier naming are strictly computed values while topic names are user defined.
4. `routing_key`/`binding` survive this phase with their matching logic
   completely unchanged — so what did splitting into per-topic tables
   actually fix, and what did it deliberately leave unfixed?
Answer: It fixed most of what was explained in Q1. However retention / partitions are no longer system-wide they are topic scoped which is better but still a constraint ie you could not have per consumer retention / partition configuration.


### 8c — Log compaction: latest-per-key, filtered at claim time

**Explain it back:**
1. Why doesn't this design need a watermark-safe floor to gate correctness,
   unlike Kafka's own compacted topics (and this repo's
   `reference/waterline/compaction.go`)? What does the floor become instead?
Answer: Because correctness is garenteed at produce / write time it is not an async process that needs an additional correctness gate due to potential lag.
The floor for us is just the standard cursor committed value (not claimed -- claimed can regress on a crash/reclaim, committed is the crash-safe frontier),
and it's no longer a correctness gate -- it downgrades to an optional, whenever-convenient disk-space cleanup, decoupled from what a claim can return.
2. Why can a brand-new consumer group get latest-per-key on its very first
   claim under this design, when a background-delete design can't give it
   that for free?
Answer: Because 'latest' is garenteed after producer transaction is complete. So the claim query will always get lastest id for compaction_key
A background delete design has some amount of lag before compaction is complete and as such has not strong garentee you will get latest, it is dependent on
size of background-delete lag
3. Why does the filter search unboundedly for a key's latest write instead
   of pinning to the claim's own high (`id <= $hi`)?
Answer: Because the gaurentee we hold by for a compacted topic is not 'at-least-once per message' it is 'at-least-once per latest compacted_key'.
A bounded check would only be wrong on reclaim specifically: a lease's high is pinned once and reused on every retry, so after a crash a newer write
landing outside that fixed window would be invisible to a bounded check -- the reclaimed row would look 'locally latest' within the stale window even
though it's actually been superseded. Unbounded means the check re-evaluates live against current state every time, not the state pinned at claim time.
4. Why is the `compaction_key` index partial (`WHERE compaction_key IS NOT
   NULL`) instead of covering every row?
Answer: Because compaction_key is not the standard consumer setup and we would incur write overhead for no reason.
(Note: this index was dropped entirely later in 8c once latest_keys made it a dead consumer -- this answers why it WAS partial, not what exists today.)
5. Phase 8b split every topic into its own physical table and its own dense
   id sequence. Why does that help *this* phase's compaction lookup
   specifically — what did a shared, system-wide `BIGSERIAL` cost a single
   topic's own key-latest search before 8b existed?
Answer: It doesn't matter for latest_keys itself -- that lookup is O(1) regardless of partition count or sequence density by construction. It still matters
for the scan though, which is still the spec this phase's read path has to satisfy underneath latest_keys: before 8b, proving a negative meant scanning
across every OTHER topic's interleaved traffic sharing the same BIGSERIAL, not just this topic's own volume. 8b bounds that scan to one topic's own
history -- it just doesn't buy the index anything, since the index sidesteps the scan entirely.


## Phase 9 — Consumer fault isolation & recovery

**Explain it back:**
1. Why does a recovered panic have to go through the *exact same*
   retry/backoff/dead path as an ordinary error, instead of its own
   special-cased handling?
Answer: recovered panics are not necessarily permanent errors (nil map write, index out of range, bad type assertion). The fact is we don't know if a 
retry will help or not b/c we don't know the consumerFuncs code. So it is better to go on side of caution and follow standard expected path instead
of making assumptions
2. Why is `context.WithTimeout` alone insufficient to enforce `WorkTimeout`,
   and what does the detached-goroutine race actually buy you given Go has no
   goroutine kill?
Answer: context timeouts expect to be explicitily handled. Normally via a call to ctx.Err or ctx.Done. Our own internal code we can do that for. However
we cannot gauruntee that the user does that within their consumerFunc. Because of that we have a detached-goroutine race that allows us to internally exit
the consumerFunc work such that we may retry or mark dead within the users expected WorkTimeout + Grace period. The one caveat this brings is that the goroutine
that was raced is still running and as such we have a abanonded routine which we track via metrics
3. Why key the abandoned-goroutine registry by (message, attempt) rather than
   by message alone?
Answer: If first and second attempt of a message was abandoned. The second attempt would overwrite the first within registry despite their potentially being two
real live abandoned go routines. message & attempt is the uniquness identifier for the goroutine and as such should be the key


## Phase 10 — Observability: logging & the rollup model

**Explain it back:**
1. What's the tradeoff between a lazy periodic rollup and a synchronous one —
   what do you gain and what do you pay for each?
Answer: for lazy - its an async rollup so you have some lag between what has actually been processed vs where committed sits.
This lag causes partition drop and deliveries sweep to have a few seconds of lag. However b/c it is lazy the committed movement is off the hot path and so that cursor
movement does not slow or degrage throughput.
for synchronous - it is mostly the opposite. Partition drops and delivery sweeps happen nearly right after committed changes (no lag) which better shows exactly where
committed is. but it is at the cost of an extra query on the claim release hot path which slows down throughput. Specifically this isn't just an extra query's fixed cost --
`Commit` today never touches `cursor` at all (only `lease`/`deliveries`), so concurrent committers in the same group commit fully in parallel right now. A synchronous
rollup adds an `UPDATE cursor` that those same committers now serialize on, which is why the 20-worker case measured 1.3x-1.9x slower, not just the flat +30-50% fixed-cost hit.
2. Why does a live debug readout of claimed/committed/exception-count matter
   even though the underlying data was always queryable in Postgres directly?
Answer: its a better developer experience, they don't have to know the underlying typology they just call a method
3. For each number in the metrics snapshot: which failure mode is it the
   early warning for?
		"queue:      head=%d claimed=%d committed=%d  (backlog=%d, inflight=%d)\n"+
			"exceptions: ready=%d inflight=%d dead=%d  (oldest unacked: %s)\n"+
			"leases:     open=%d\n"+
			"abandoned:  total=%d outstanding=%d  (avg self-clear: %s)",
Answer: 
backlog - the classic consumer lag metrics. Means you are trailing behind head which is normally not good.
exceptions dead - how many messages have truly failed, how numbers normally indicate a bug or outage
abandoned total / self-clear - number of routine timeouts and how long they take to resolve if they do. Can indicate not handled ctx close or async code hanging
inflight (claimed-committed gap) - batches out for processing right now; distinguishes rollup lag from real backlog
ready exceptions - retry queue depth building up
inflight exceptions - currently mid-retry
oldest unacked age - flags a single stuck message even when the counts otherwise look fine
open leases - a crashed/never-committed consumer, exactly what scenarioCrash in metricsreactionlab exercises
abandoned outstanding - goroutines hung right now, vs. total's lifetime count
4. Why does the OTel integration depend on `go.opentelemetry.io/otel/metric`
   (the API package) but never the SDK or a specific exporter like
   Prometheus's or Datadog's client?
Answer: go.opentelemetry.io/otel/metric is only api code ie very light not many dependencies
go.opentelemetry.io/otel has a lot of extra code and dependencies that make this library heavier


