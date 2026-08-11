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
    name                TEXT NOT NULL UNIQUE,            -- also the routing key every firing is produced with
    schedule            TEXT NOT NULL,
    concurrency         TEXT NOT NULL DEFAULT 'allow',   -- 'allow' | 'defer'
    timeout_ns          BIGINT NOT NULL,                 -- -> MessageOptions.Timeout
    suspended           BOOLEAN NOT NULL DEFAULT false,
    data                JSONB NOT NULL DEFAULT '{}',     -- opaque payload
    metadata            JSONB NOT NULL DEFAULT '{}',
    next_scheduled_time TIMESTAMPTZ NOT NULL,
    last_scheduled_time TIMESTAMPTZ,                     -- firing most recently produced; tick-stamped, scheduler truth only
    CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) <= 1),  -- all NULL = standalone
    CHECK (concurrency IN ('allow', 'defer')),
    CHECK (timeout_ns > 0)
);
CREATE INDEX IF NOT EXISTS cron_job_due ON cron_job (next_scheduled_time) WHERE NOT suspended;
```

Owner columns = GC metadata only; all NULL = standalone. No Go owner param
anywhere yet — an Owner must never be nil; add the param when something
actually creates owned jobs.
`timeout_ns BIGINT` not INTERVAL — house duration convention.
NO handler/routing column (2026-08-01): every firing produces with
RoutingKey = name; consumers bind names — exact, several per group, or their
own naming-convention wildcards (`reports-*`). Rejected: mandatory handler
segment (`cronjob.<handler>.<name>`), optional Config.RoutingKey, and
name-prefix synthesis (`<name>.<suffix>`) — each re-invents grouping the
binding table already owns, asymmetric with how topics route.

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
  schedule.go        OURS: public Schedule STRUCT (parsed form + source expr;
                     robfig bitmasks can't serialize back to the TEXT column)
                     + ParseSchedule + MinRate method -- API never names an internal type
  cronjob.go         OURS: CronJob + NewCronJob
  config.go          OURS: Config (sparse; WithDefaults/Validate)
  datastore.go       OURS: CronJobDatastore + admin-verb queries
  jobrequest.go      OURS: JobRequest + TopicName + TopicConfig + v7 key
```

NOT vendored: cron.go/chain.go/option.go/logger.go — their in-process runner;
the duty is ours. Provenance header + pinned version on every vendored file
(package clause renamed cron -> robfig, noted in each header).

```go
func ParseSchedule(expr string) (*Schedule, error)       // wraps vendored ParseStandard; rejects
                                                         // never-fires and sub-1m-rate schedules
func (s *Schedule) MinRate() time.Duration                // min rate over 1000 firings / 400 days,
                                                         // computed at parse

const TopicName = "__system.job_requests"                // compacted, ~1 row per job
func TopicConfig() *topic.Config                         // DeliveryLog 'all' (status derives from it);
                                                         // RetentionTTL 35d -- status history horizon,
                                                         // must exceed the widest firing rate (monthly covered;
                                                         // fired-truth survives on the row regardless)

type JobRequest struct { CronJobId int64; Name string;
    ScheduledTime time.Time; Data, Metadata json.RawMessage }

func firingKey(firing time.Time, cronJobId int64) uuid.UUID  // v7 layout: 48-bit ms(firing) + id VERBATIM
                                                         // in the 74 payload bits -- NO hash: the
                                                         // idempotency table is shared per-topic, a
                                                         // same-ms hash collision would silently
                                                         // swallow another job's firing; int64 fits
```

## Register validation (sanity only — key_lease owns overlap)

- schedule is a parsed *cron.Schedule -- parse-don't-validate, an invalid
  spec can't reach Register: ParseSchedule itself rejects never-fires and
  `MinRate < 1m` (scheduler resolution); exactly ONE firing in the MinRate
  horizon = pass, rate unbounded (Feb-29-style schedules)
- no free validate func -- Config.Validate covers concurrency ∈ {allow, defer}
  and timeout > 0; registerCronJob guard clauses cover the rest:
  name slug, schedule non-nil, `timeout <= MinRate`
- name matches `^[a-z0-9._-]+$` — name is the produced routing key. '*' is
  banned: it's the binding wildcard and Bind has no escape syntax, so a name
  holding one could never be bound exactly. Dots are ALLOWED (settled
  2026-08-01, reversing the earlier ban): binding literals are QuoteMeta'd
  and '*' is a plain any-characters glob, so '.' adds no collision surface
- seed `next_scheduled_time = Next(db_now)` — DB clock, like every scheduler
  time (below); Next zero at seed → reject. UNIFORM Next-zero rule:
  Register + unsuspend ERROR, tick suspends + WARN — all three sites
- get-or-create, the registerTopic shape: get → assertConfigMatches →
  advisory lock `cron_job:<name>` → re-check → INSERT (no 23505 catch — the
  lock makes it unreachable). Identical schedule/data/cfg resolves to the
  existing job; a differing one → ErrCronJobConfigMismatch. The match compares
  the found row in Go like topic's (no extra query); data/metadata via
  jsonEqual (unmarshal + DeepEqual — stored jsonb comes back normalized, raw
  bytes don't compare)

## Scheduler = maintenance duty `'scheduler'`, system-owned, first of its kind

- `system.scheduler_poll_rate_ns` column + `SchedulerPollRate` (default 1m, floor 1m) — AlertRepeatInterval's sparse-column pattern (AlertPollRate itself deleted 2026-08-01: each alert check is its own cron job now, so per-job schedules replaced the shared poll rate) — **SUPERSEDED 2026-08-02, maintain refactor piece 1**: every duty's poll rate now lives in `maintenance.metadata` `{"poll_rate": <ns>}`; scheduler_poll_rate_ns / SchedulerPollRate / the CLI knob are deleted, the scheduler seed writes poll_rate 1m
- duty row seeded at RegisterSystem: `INSERT INTO maintenance (duty, system_id) SELECT 'scheduler', $1 WHERE NOT EXISTS (...)`
- `maintain.DutyScheduler` const + `scheduler_duty.go` on the janitor_duty shape
  (Register: GetConfig → AssertSystemSchemaSupported → NewSystemOwner → `Producer[cron.JobRequest]` + dutyRunner)
- **listDuties gap**: `JOIN topic ON t.id = COALESCE(m.topic_id, g.topic_id)` is INNER —
  system-owned rows invisible today. LEFT it + `WHEN 'scheduler' THEN s.scheduler_poll_rate_ns`
  + no-topic FleetDuty + `dutybuilder case DutyScheduler`. (The `LEFT JOIN system s`
  went away with the alert duty on 2026-08-01 — re-add it for the scheduler arm.) Same check on dutySnapshots.
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

firing := row.NextScheduledTime                       // the firing the message represents
for n := sched.Next(firing); !n.IsZero() && n <= row.DbNow; n = sched.Next(firing) {
    firing = n                                        // fire the NEWEST due firing -- after downtime
}                                                   // staleness <= one firing rate, uniform with
                                                    // missed-runs-dropped, no knob; the !IsZero
                                                    // guard keeps an unsatisfiable schedule from
                                                    // spinning (zero time <= everything)
res, err := p.ProduceInTx(ctx, tx, fn, ProduceOptions{
    RoutingKey:     row.Name,                                    // consumers bind job names
    CompactionKey:  strconv.FormatInt(row.Id, 10),               // id not name (k8s-UID rule)
    IdempotencyKey: firingKey(firing, row.Id),                       // replay-safe, fire once
    Message:        &common.MessageOptions{Concurrency: row.Concurrency, Timeout: row.Timeout},
})
if !res.Landed { /* WARN "firing deduped -- ambiguous-commit replay" */ }   // still advance, not an error

next := sched.Next(row.DbNow)                       // DB clock ONLY (claimDuty precedent) -- Go/DB
                                                    // skew double-fires tight schedules
if next.IsZero() {
    // unsatisfiable at tick (tzdata drift): keep the produce + last_scheduled_time,
    // but suspended = true + WARN instead of the advance (column is NOT NULL)
    UPDATE cron_job SET suspended = true, last_scheduled_time = firing WHERE id = $1
} else {
    UPDATE cron_job SET next_scheduled_time = next, last_scheduled_time = firing WHERE id = $1
}
```

Row error → WARN + skip, siblings proceed; only scan/conn errors reach
dutyRunner backoff. Every due firing produced unconditionally — concurrency
enforced at consume time by key_lease. Once-per-firing rides the committed
advance + SKIP LOCKED; the v7 key covers exactly ONE case, replay after an
AMBIGUOUS COMMIT — produce + advance + idempotency claim share the txn, so a
rollback rolls the claim back too and the replay lands fresh (don't ever
"fix" IdempotencyKeyTTL for the scheduler's sake).

Documented semantics (decided, not bugs — each gets a godoc/CLI sentence):
- compaction key = id means NEWEST WINS for 'allow' jobs too — a backlogged
  consumer skips to the latest firing (claim-time compaction; the topic holds
  each job's latest request; per-firing keys rejected — unbounded stale-firing
  queue). run-now shares the key, so it SUPERSEDES a pending unclaimed firing
  and the next firing supersedes an unconsumed run-now.
- destroy/suspend do NOT retract an already-produced unclaimed firing — it
  still runs (until retention). Name reuse mints a new id/compaction key, so
  a destroyed job's orphan firing runs BESIDE the recreated job's firings.
  `vulkan cron destroy` prints this.
- suspend racing a due tick can still fire that one firing (the suspend UPDATE
  blocks on the tick's row lock, applies after commit) — suspend is effective
  at the next tick boundary.
- N consumer groups bound to one job name = N executions per firing; the
  key_lease overlap guarantee is PER-GROUP (k8s users expect one execution —
  expected topology is one group per job or per name-convention family,
  replicas share the group).
- TZ= schedules inherit robfig DST behavior: a spring-forward firing is
  skipped, a fall-back one fires once; MinRate sees the 23h fall-back rate
  (conservative direction — fine).

## Admin verbs (MessageAdmin → CronJobDatastore)

```go
RegisterCronJob(ctx, name string, schedule *cron.Schedule, data any, cfg *cron.Config) (*cron.CronJob, error)
    // identity + data (marshaled payload, nil = {}) in the signature --
    // args-beside-schedule precedent (River/Celery/BullMQ); generics rejected:
    // no generic methods in Go, and register-side D can't constrain the
    // handler anyway. cron.Config sparse (house WithDefaults/Validate):
    // Timeout 30s, Concurrency allow, Metadata any like data (both driver-
    // marshaled, nil = {} via COALESCE). NO owner param anywhere --
    // an Owner must never be nil, and nothing yet creates owned jobs; the
    // owner columns stay NULL (standalone). GC-owned jobs add the param to
    // CronJobDatastore.RegisterCronJob when they actually exist
AlterCronJob(ctx, name string, cfg *cron.AlterConfig) (*cron.CronJob, error)
    // AlterTopic shape: sparse patch (Schedule/Timeout/Concurrency/Data/Metadata,
    // unset = unchanged), COALESCE UPDATE, (nil, nil) -> ErrCronJobNotFound at
    // the admin layer. Effective schedule/timeout pair re-checked against
    // MinRate; a schedule change re-seeds next_scheduled_time = Next(db_now)
GetCronJob / ListCronJobs / DestroyCronJob
SuspendCronJob(ctx, name)
UnsuspendCronJob(ctx, name)     // next_scheduled_time = Next(db_now) -- no stale-firing fire;
                                // Next zero (schedule went unsatisfiable) -> error, stays suspended
RunCronJob(ctx, name)           // uuid.NewV7() key -- random enough to never dedupe, time-ordered
                                // for the idempotency index (ProduceOptions' own godoc: v4 hurts);
                                // Concurrency 'allow' (force-run idiom) + Timeout stamped from the
                                // row like the tick; ScheduledTime = db_now; works while suspended;
                                // SUPERSEDES a pending unclaimed firing (shared compaction key)
```

Status: fired = `cron_job.last_scheduled_time` (scheduler truth, survives
retention); succeeded/failed = delivery_log rows per consumer group joined
through message_log on the job's compaction key — needs the DeliveryLog mode
refactor below. Window: verified at build (2026-08-11) — the janitor's drop
and sweep reap delivery_log rows in the same pass and at the same retention
cutoff as their message rows, so the window is exactly the 35d RetentionTTL;
there is no separate delivery_log lifetime.

## DeliveryLog mode — BUILT 2026-08-11 (was: lands first in Chunk 2)

Success today = delivery-row deletion + NO log row (cursor-path successes are
O(1) per range — the throughput story; don't break it platform-wide). Fold the
existing bool into one enum: topic config `DeliveryLog: 'off' | 'failures' |
'all'`, default `'failures'` (value renamed from 'exceptions' 2026-08-11; today's behavior; `DisableDeliveryLog=true`
maps to 'off' — pre-v1, edit in place). `'all'` adds `'success'@attempts` rows
inside the SAME success txns (no extra WAL flush): Commit/PartialCommit take
the buffer's resolved-success message ids as an explicit param (same shape as
`superseded`) + RecordExceptionSuccess logs its own. REJECTED: deriving
successes from a message_log range scan + binding predicate — claim-time-
compacted messages (the sanctioned silent drop) match the binding and would
log 'success' for firings that NEVER RAN; on job_requests that is every
superseded firing, poisoning exactly the status this mode exists for. The
buffer already holds the true list in memory. Invariant amends cleanly: every attempts
increment ends in exactly one log row OR the success-deletion — under 'all'
the success-deletion also logs, in the same txn.
`cron.TopicConfig()` sets 'all' (per-job-per-firing volume, floor 1/min
— noise); hot user topics never pay unless they opt in.

Built as specced, with these shape notes: `topic.DeliveryLogMode` enum
('off'/'failures'/'all'; type renamed from DeliveryLog and the middle value from 'exceptions' on review -- bare
`delivery_log` read as the table, not a setting) replaces the bool
everywhere it was threaded
(claim/commit/exception/kill/quarantine/janitor-reap paths take the enum;
gates read `!= off`); success is an `OutcomeSuccess` outcome kind
(log row only, never a delivery row -- same as superseded; user-picked over
the earlier `successes []int64` param, which widened Commit/PartialCommit for
one mode), and the runner's buffer walks include success outcomes only under
'all' (messageRunner.logSuccesses), so the common case keeps its zero-alloc
happy path; RecordExceptionSuccess and the parked lifecycle path's RecordSuccess
write their 'success' row via a CTE in the same statement as the
deletion/'done' mark; `delivery_log_mode` column TEXT NOT NULL DEFAULT
'failures', baseline DDL edited in place (the CHECK was later dropped in favor of the read-side guard); metrics + alerts topics
pin 'off', job_requests pins 'all'; CLI flag `--delivery-log-mode`.
`toTopic` returns `(*topic.Topic, error)` and maps the stored string through
an exhaustive switch (`deliveryLogModeEnum`) rather than casting it, so a row
that ever holds an unknown mode fails the read instead of silently behaving
as the failures mode — same shape as NewCronJob validating its stored
concurrency. Verified:
deliveryloglab reshaped (new mode-'all' scenario covering both success write
paths) + a real-Consumer end-to-end success-row check + affected-lab batch
on a drop+recreate fresh DB, go test -race green.

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
   + admin verbs (minus RunCronJob). Verify: validation matrix, MinRate vectors,
   vendored suite green, fresh-DB psql shape. BUILT 2026-08-01.
2. **Scheduler**: BUILT 2026-08-02, user-ordered ahead of the two platform
   pre-reqs — DeliveryLog mode + ProduceResult are NOT built (the scheduler
   doesn't need them to fire; they move to the status/consumer work): the
   tick has no `!Landed` WARN yet, and job_requests runs today's default
   delivery-log behavior, not 'all'. Built: jobrequest.go (TopicName 35d TTL
   / JobRequest / FiringKey with id-verbatim payload bits);
   scheduler_poll_rate_ns + SchedulerPollRate (floor 1m) + CLI get/alter;
   RegisterSystem seeds topic + system-owned duty row (+
   maintenance_system_duty index); DutyScheduler + scheduler_duty.go
   (janitor shape, Producer[JobRequest] registered in Register) +
   scheduler.go tick (per-row txns, newest-due firing, DB-clock advance,
   Next-zero suspends+WARN, NULL-rate rows skipped in listDuties instead of
   erroring the list). Verified live: seed/discovery, backdated job → 1
   message w/ correct payload/keys, row advance, same-firing re-backdate
   deduped by FiringKey. Maintain refactor piece 1 (2026-08-02) then moved
   the poll rate onto maintenance.metadata and deleted scheduler_poll_rate_ns
   / SchedulerPollRate / the CLI knob; piece 2 (same day) reshaped Register
   to `(ctx, duty, owner, meta) (bool, error)` with a shouldRegister kind
   check (owner/rate now come from the offered maintenance row, NewSystemOwner
   call gone) — the tick is unchanged.
3. **CLI + status**: `vulkan cron` tree (register/get/list/suspend/unsuspend/run/destroy,
   destroy double-guarded); RunCronJob; derived status in `get` — one line per
   consumer group whose binding matches the job's name (normally one). Verify:
   live CLI pass.
4. **cronlab + close-out**: sections — validation rejections (incl. charset,
   Feb-29 single-firing pass, Next-zero seed); fire-once + backdated row → 1
   message stamped with the NEWEST due firing; v7 dedupe proven by RE-BACKDATING
   next_scheduled_time to the SAME firing (a plain double tick is masked by the
   committed advance and proves nothing); suspend/unsuspend semantics; defer under held key (spot — deferlab owns
   depth); run-now beside busy key + run-now supersedes a pending unclaimed firing;
   owner cascade vs standalone survival; consumer end-to-end (bind name, ctx, retry);
   one poisoned row (failing produce) — siblings still fire, duty keeps ticking;
   status: 'success' rows land, `get` shows fired/succeeded/failed. Then: lab-mirror
   grep sweep, full fresh-DB suite, resettle bullet, delete refactor-plan.md + this
   file, NOTES.md, memory update.
