# cron_job + scheduler + job_request — build plan

Task 5 of 5, Phase 14a. Settled design = the LEARNING_PLAN 14a bullet.
Delete this file + `refactor-plan.md` at close-out; bullet resettles as-built.

## Schema (pkg/system/datastore.go register SQL, edited in place)

```sql
CREATE TABLE IF NOT EXISTS cron_job (
    id                  BIGSERIAL PRIMARY KEY,
    system_id           BIGINT REFERENCES system (id) ON DELETE CASCADE,
    topic_id            BIGINT REFERENCES topic (id) ON DELETE CASCADE,
    consumer_group_id   BIGINT REFERENCES consumer_group (id) ON DELETE CASCADE,
    name                TEXT NOT NULL UNIQUE,
    handler             TEXT NOT NULL,
    schedule            TEXT NOT NULL,
    concurrency         TEXT NOT NULL DEFAULT 'allow',   -- 'allow' | 'defer'
    timeout_ns          BIGINT NOT NULL,                 -- -> MessageOptions.Timeout
    suspended           BOOLEAN NOT NULL DEFAULT false,
    data                JSONB NOT NULL DEFAULT '{}',     -- opaque to everything but handlers
    metadata            JSONB NOT NULL DEFAULT '{}',
    next_scheduled_time TIMESTAMPTZ NOT NULL,
    last_scheduled_time TIMESTAMPTZ,                     -- slot most recently produced; tick-stamped, scheduler truth only
    CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) <= 1),  -- all NULL = standalone
    CHECK (concurrency IN ('allow', 'defer')),
    CHECK (timeout_ns > 0)
);
CREATE INDEX IF NOT EXISTS cron_job_due ON cron_job (next_scheduled_time) WHERE NOT suspended;
```

Owner = GC metadata only; nil `*common.Owner` = standalone.
`timeout_ns BIGINT` not INTERVAL — house duration convention.

## pkg/cron

```
pkg/cron/
  internal/robfig/   VENDORED robfig/cron v3.0.1 -- the dir IS the vendor
                     boundary: everything inside is upstream, everything in
                     pkg/cron proper is ours; internal/ keeps it unimportable
                     outside pkg/cron
    LICENSE          robfig MIT, required
    spec.go          SpecSchedule bitmasks + Next
    parser.go        grammar, TZ= prefix, @hourly/@every    <- 1 diff: time.Local -> time.UTC
    constantdelay.go @every's type
    schedule.go      Schedule interface hoisted from their cron.go
    *_test.go        their vectors, kept green (TZ-default vectors carry their zone explicitly)
  schedule.go        OURS: public Schedule interface (redeclared -- API never
                     names an internal type) + ParseSchedule + MinGap
  cronjob.go         OURS: CronJob + NewCronJob + CronJobDatastore
  jobrequest.go      OURS: JobRequest + TopicName + TopicConfig + v7 key
```

NOT vendored: cron.go/chain.go/option.go/logger.go — their in-process runner;
the duty is ours. Provenance header + pinned version on every vendored file
(package clause renamed cron -> robfig, noted in each header).

```go
func ParseSchedule(expr string) (Schedule, error)        // wraps vendored ParseStandard
func MinGap(s Schedule) (time.Duration, error)           // min gap over 1000 firings / 400 days

const TopicName = "__system.job_requests"                // compacted, ~1 row per job
func TopicConfig() *topic.Config                         // DeliveryLog 'all' (status derives from it);
                                                         // RetentionTTL 35d -- status history horizon,
                                                         // must exceed the widest firing gap (monthly covered;
                                                         // fired-truth survives on the row regardless)

type JobRequest struct { CronJobId int64; Name, Handler string;
    ScheduledTime time.Time; Data, Metadata json.RawMessage }

func slotKey(slot time.Time, cronJobId int64) uuid.UUID  // v7 layout: 48-bit ms(slot) + id VERBATIM
                                                         // in the 74 payload bits -- NO hash: the
                                                         // idempotency table is shared per-topic, a
                                                         // same-ms hash collision would silently
                                                         // swallow another job's slot; int64 fits
```

## Register validation (sanity only — key_lease owns overlap)

- schedule parses; `MinGap >= timeout`; `MinGap >= 1m` (scheduler resolution);
  exactly ONE firing in the MinGap horizon = pass, gap unbounded (Feb-29-style
  schedules) — only zero firings reject
- concurrency ∈ {allow, defer}; timeout > 0; name/handler match `^[a-z0-9_-]+$`
  — a '.' in either cross-delivers to other handlers' `cronjob.<handler>.*`
  bindings (anchored '*'-wildcard regexes), and '*' in a name is unbindable
- seed `next_scheduled_time = Next(db_now)` — DB clock, like every scheduler
  time (below); Next zero at seed → reject. UNIFORM Next-zero rule:
  Register + unsuspend ERROR, tick suspends + WARN — all three sites
- duplicate name (23505) → wrapped "cron job %q already registered" error,
  not a raw pg error

## Scheduler = maintenance duty `'scheduler'`, system-owned, first of its kind

- `system.scheduler_poll_rate_ns` column + `SchedulerPollRate` (default 1m, floor 1m) — AlertPollRate pattern
- duty row seeded at RegisterSystem: `INSERT INTO maintenance (duty, system_id) SELECT 'scheduler', $1 WHERE NOT EXISTS (...)`
- `maintain.DutyScheduler` const + `scheduler_duty.go` on the janitor_duty shape
  (Register: GetConfig → AssertSystemSchemaSupported → NewSystemOwner → `Producer[cron.JobRequest]` + dutyRunner)
- **listDuties gap**: `JOIN topic ON t.id = COALESCE(m.topic_id, g.topic_id)` is INNER —
  system-owned rows invisible today. LEFT it + `WHEN 'scheduler' THEN s.scheduler_poll_rate_ns`
  + no-topic FleetDuty + `dutybuilder case DutyScheduler`. Same check on dutySnapshots.
  Hardening while there: topic columns + rate scan NULLABLE, kind list and rate CASE
  in lockstep (a kind in WHERE without a CASE arm NULLs the rate and errors the WHOLE
  list — every duty fleet-wide); the no-topic row needs its schema gate at Register
  (AssertSystemSchemaSupported), topic schema_version doesn't apply.

Tick: unlocked due-scan (`SELECT id WHERE next_scheduled_time <= now() AND NOT
suspended`), then ONE TXN PER ROW — a shared tick txn would let one bad row
roll back every job's produce and back off the whole duty forever, and holds
ProduceInTx's whole-topic consumer-progress lock (its own doc: "call this
LAST") from the first produce to the end of the tick:

```go
// per id, own txn:
row := SELECT *, now() AS db_now FROM cron_job
       WHERE id = $1 AND next_scheduled_time <= now() AND NOT suspended
       FOR UPDATE SKIP LOCKED                       -- recheck makes the unlocked scan safe
if row == nil { continue }                          // raced away: run-now/suspend/other claimer

slot := row.NextScheduledTime                       // the slot the message represents
for n := sched.Next(slot); !n.IsZero() && n <= row.DbNow; n = sched.Next(slot) {
    slot = n                                        // fire the NEWEST due slot -- after downtime
}                                                   // staleness <= one firing gap, uniform with
                                                    // missed-runs-dropped, no knob; the !IsZero
                                                    // guard keeps an unsatisfiable schedule from
                                                    // spinning (zero time <= everything)
res, err := p.ProduceInTx(ctx, tx, fn, ProduceOptions{
    RoutingKey:     "cronjob." + row.Handler + "." + row.Name,   // bindings: cronjob.<handler>.*
    CompactionKey:  strconv.FormatInt(row.Id, 10),               // id not name (k8s-UID rule)
    IdempotencyKey: slotKey(slot, row.Id),                       // replay-safe, fire once
    Message:        &common.MessageOptions{Concurrency: row.Concurrency, Timeout: row.Timeout},
})
if !res.Landed { /* WARN "slot deduped -- ambiguous-commit replay" */ }   // still advance, not an error

next := sched.Next(row.DbNow)                       // DB clock ONLY (claimDuty precedent) -- Go/DB
                                                    // skew double-fires tight schedules
if next.IsZero() {
    // unsatisfiable at tick (tzdata drift): keep the produce + last_scheduled_time,
    // but suspended = true + WARN instead of the advance (column is NOT NULL)
    UPDATE cron_job SET suspended = true, last_scheduled_time = slot WHERE id = $1
} else {
    UPDATE cron_job SET next_scheduled_time = next, last_scheduled_time = slot WHERE id = $1
}
```

Row error → WARN + skip, siblings proceed; only scan/conn errors reach
dutyRunner backoff. Every due slot produced unconditionally — concurrency
enforced at consume time by key_lease. Once-per-slot rides the committed
advance + SKIP LOCKED; the v7 key covers exactly ONE case, replay after an
AMBIGUOUS COMMIT — produce + advance + idempotency claim share the txn, so a
rollback rolls the claim back too and the replay lands fresh (don't ever
"fix" IdempotencyKeyTTL for the scheduler's sake).

Documented semantics (decided, not bugs — each gets a godoc/CLI sentence):
- compaction key = id means NEWEST WINS for 'allow' jobs too — a backlogged
  consumer skips to the latest slot (claim-time compaction; the topic holds
  each job's latest request; per-slot keys rejected — unbounded stale-slot
  queue). run-now shares the key, so it SUPERSEDES a pending unclaimed slot
  and the next slot supersedes an unconsumed run-now.
- destroy/suspend do NOT retract an already-produced unclaimed slot — it
  still runs (until retention). Name reuse mints a new id/compaction key, so
  a destroyed job's orphan slot runs BESIDE the recreated job's slots.
  `vulkan cron destroy` prints this.
- suspend racing a due tick can still fire that one slot (the suspend UPDATE
  blocks on the tick's row lock, applies after commit) — suspend is effective
  at the next tick boundary.
- N consumer groups bound to one handler = N executions per slot; the
  key_lease overlap guarantee is PER-GROUP (k8s users expect one execution —
  expected topology is one group per handler, replicas share the group).
- TZ= schedules inherit robfig DST behavior: a spring-forward slot is
  skipped, a fall-back slot fires once; MinGap sees the 23h fall-back gap
  (conservative direction — fine).

## Admin verbs (MessageAdmin → CronJobDatastore)

```go
RegisterCronJob(ctx, name, handler, schedule string, timeout time.Duration,
    concurrency common.ConcurrencyPolicy, data, metadata json.RawMessage,
    owner *common.Owner) (*cron.CronJob, error)
GetCronJob / ListCronJobs / DestroyCronJob
SuspendCronJob(ctx, name)
UnsuspendCronJob(ctx, name)     // next_scheduled_time = Next(db_now) -- no stale-slot fire;
                                // Next zero (schedule went unsatisfiable) -> error, stays suspended
RunCronJob(ctx, name)           // uuid.NewV7() key -- random enough to never dedupe, time-ordered
                                // for the idempotency index (ProduceOptions' own godoc: v4 hurts);
                                // Concurrency 'allow' (force-run idiom) + Timeout stamped from the
                                // row like the tick; ScheduledTime = db_now; works while suspended;
                                // SUPERSEDES a pending unclaimed slot (shared compaction key)
```

Status: fired = `cron_job.last_scheduled_time` (scheduler truth, survives
retention); succeeded/failed = delivery_log rows per consumer group joined
through message_log on the job's compaction key — needs the DeliveryLog mode
refactor below. Window = min(35d RetentionTTL, delivery_log row lifetime) —
verify at build which the janitor drops first. No AlterCronJob this task.

## DeliveryLog mode (platform-wide decision, lands first in Chunk 2, own commit)

Success today = delivery-row deletion + NO log row (cursor-path successes are
O(1) per range — the throughput story; don't break it platform-wide). Fold the
existing bool into one enum: topic config `DeliveryLog: 'off' | 'exceptions' |
'all'`, default `'exceptions'` (today's behavior; `DisableDeliveryLog=true`
maps to 'off' — pre-v1, edit in place). `'all'` adds `'success'@attempts` rows
inside the SAME success txns (no extra WAL flush): Commit/PartialCommit take
the buffer's resolved-success message ids as an explicit param (same shape as
`superseded`) + RecordExceptionSuccess logs its own. REJECTED: deriving
successes from a message_log range scan + binding predicate — claim-time-
compacted messages (the sanctioned silent drop) match the binding and would
log 'success' for slots that NEVER RAN; on job_requests that is every
superseded slot, poisoning exactly the status this mode exists for. The
buffer already holds the true list in memory. Invariant amends cleanly: every attempts
increment ends in exactly one log row OR the success-deletion — under 'all'
the success-deletion also logs, in the same txn.
`cron.TopicConfig()` sets 'all' (per-job-per-firing volume, floor 1/min
— noise); hot user topics never pay unless they opt in.

## ProduceResult (platform-wide decision, lands in Chunk 2, own commit)

Every produce path returns a result struct instead of the bare payload:

```go
type ProduceResult[M any] struct {
    Message *M     // the built payload (NOT the original on dup -- unrecoverable
                   // by design: the idempotency table is not message-correlated)
    Id      int64  // stored message id; 0 when !Landed
    Landed  bool   // false = idempotency claim already existed
}

func (p *Producer[M]) Produce(ctx, msg *M, opts) (*ProduceResult[M], error)
// same shape on ProduceInTx + InTransaction's inner produce
```

Why: today a dedupe returns `(msg, nil)` — indistinguishable from landed, and
no stored identity either, weaker than every precedent (SQS returns the
original MessageId, Stripe flags the replay, River returns
`UniqueSkippedAsDuplicate`). `Landed` is already the datastore's own internal
name (`insertProtected` returns it; the savepoint path swallows it in its
ErrNoRows branch — thread it out); `Id` is already RETURNINGed by the insert
CTE and thrown away. One struct keeps all three produce signatures uniform
and future fields non-breaking. Rejected: comma-ok bool (every call site pays
a blank, next field breaks again); sentinel error (the retry path — the one
idempotency exists to protect — would read as failure); opts callback
(policy-as-code, same reason the consumer resolver hook died).
Retry users ignore the fields; business-dedupe users branch on `Landed`;
the tick WARNs on `!Landed` (an ambiguous-commit tell).

Call-site sweep: labs/examples/bench + the metrics producer; grep labs for
hand-copied produce shapes (lab-staleness rule). Constructor per house rule.

## Chunks

1. **Registry**: DDL + vendor copy + schedule.go wrappers + CronJob/CronJobDatastore
   + admin verbs (minus RunCronJob). Verify: validation matrix, MinGap vectors,
   vendored suite green, fresh-DB psql shape.
2. **Scheduler**: TWO platform pre-req commits first — (a) the DeliveryLog
   mode refactor (enum + buffer-derived 'success' arm, threads where
   disableDeliveryLog threads today), (b) ProduceResult (struct + Landed/Id
   threading + call-site sweep). Then:
   TopicConfig + JobRequest + slotKey; scheduler_poll_rate_ns;
   RegisterSystem seeds topic + duty row; DutyScheduler + scheduler_duty.go +
   listDuties system arm + tick. Verify: dev-DB single pass — due job → 1 message,
   second pass → 0; a consumed message on an 'all' topic → 1 'success' row.
3. **CLI + status**: `vulkan cron` tree (register/get/list/suspend/unsuspend/run/destroy,
   destroy double-guarded); RunCronJob; derived status in `get` — one line per consumer
   group on the handler's binding (expected topology = one group per handler, so
   normally one line). Verify: live CLI pass.
4. **cronlab + close-out**: sections — validation rejections (incl. charset,
   Feb-29 single-firing pass, Next-zero seed); fire-once + backdated row → 1
   message stamped with the NEWEST due slot; v7 dedupe proven by RE-BACKDATING
   next_scheduled_time to the SAME slot (a plain double tick is masked by the
   committed advance and proves nothing); suspend/unsuspend semantics; defer under held key (spot — deferlab owns
   depth); run-now beside busy key + run-now supersedes a pending unclaimed slot;
   owner cascade vs standalone survival; handler end-to-end (binding, ctx, retry);
   one poisoned row (failing produce) — siblings still fire, duty keeps ticking;
   status: 'success' rows land, `get` shows fired/succeeded/failed. Then: lab-mirror
   grep sweep, full fresh-DB suite, resettle bullet, delete refactor-plan.md + this
   file, NOTES.md, memory update.
