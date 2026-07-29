okay we need to do some refactoring of our maintenance / duty system to make things more generic.

# entity table — SETTLED DESIGN (2026-07-28)

Entities are a first-class INTERNAL resource: a stable global id + the FK root for
lifecycle cascade. Users never see them.

Rule: CONTROL-PLANE rows enroll (topic, consumer_group); DATA-PLANE rows never do
(messages / deliveries / exceptions — enrolling them would add DELETE overhead and
vacuum churn; revisit only with explicit partition drops).

CREATE TABLE IF NOT EXISTS entity (
  id   BIGSERIAL PRIMARY KEY,
  type TEXT NOT NULL   -- 'topic' | 'consumer_group' | ... (classify+switch in code)
);

- NO name column — admin.Rename exists; a second copy of name drifts. Display resolution
  = switch on type + join to the owning table (internal-only, house classify+switch).
- entity is the LIFECYCLE ROOT: every enrolled table carries
    entity_id BIGINT NOT NULL UNIQUE REFERENCES entity(id) ON DELETE CASCADE
  admin.Destroy deletes the entity row and everything downstream goes transactionally
  (resource row, its cron_jobs, its alerts, ...).
- CASCADE is implicit deletion and is acceptable ONLY because pkg/admin is the sole
  lifecycle write path and Destroy is double-guarded. Do not add a second delete path.
- Each package's Register owns its entity insert (same txn) — enrollment is not a
  separate step anyone can forget.
- Enroll NOW: topic, consumer_group (cron_jobs need them as owners). Nothing else until
  a feature actually references it. cron_job itself is NOT enrolled (nothing targets
  cron_jobs yet; enroll later if e.g. alerts ever reference them).
- Alerts are the obvious second customer (typed identity in Data -> owner/target entity
  refs) but that is their refactor, not this one.

---

# cron_job / job scheduler — SETTLED DESIGN (2026-07-28)

CREATE TABLE IF NOT EXISTS cron_job (
  id                  BIGSERIAL PRIMARY KEY,
  owner_entity_id     BIGINT REFERENCES entity(id) ON DELETE CASCADE, -- whose lifecycle I'm bound to
  name                TEXT NOT NULL UNIQUE,         -- user identifier
  handler             TEXT NOT NULL,                -- valid 'runner' (like image for k8s cronjobs)
  schedule            TEXT NOT NULL,                -- compliant '* * * * *' cron schedule
  concurrency         TEXT NOT NULL DEFAULT 'allow' -- 'allow' | 'forbid' | 'defer' (later)
  timeout             INTERVAL NOT NULL,            -- job spec (k8s activeDeadlineSeconds
                                                    -- analogue); scheduler stamps it into
                                                    -- MessageOptions.WorkTimeout
  suspended           BOOLEAN NOT NULL DEFAULT false,
  data                JSONB NOT NULL DEFAULT '{}', -- OPAQUE handler payload incl. the actual
                                                   -- target; decoded only by handlers, never
                                                   -- resource refs the system must act on
  metadata            JSONB NOT NULL DEFAULT '{}'  -- labels / annotations / etc

  next_scheduled_time TIMESTAMPTZ NOT NULL,
);

CREATE INDEX ON cron_job (next_scheduled_time) WHERE NOT suspended;  -- the poll predicate

-- REJECTED: cron_job's own entity enrollment (nothing references cron_jobs yet);
-- target refs inside data JSONB as the GC mechanism (opaque blobs are
-- un-garbage-collectable — admin.Destroy can't find dependents it can't decode)

Registration-time validation (SETTLED — no handler registry needed):
- schedule parses; minimum firing gap >= work_timeout. Self-contained: work_timeout is the
  job's own spec, stamped into MessageOptions.WorkTimeout at produce — a producer concern,
  not a consumer concern.
- This check is a SANITY check, not a correctness guarantee — overlap correctness is
  enforced at dispatch by key_lease. It only prevents configs where every slot gets
  skipped. Consumer-side Grace/AckMargin are epsilon and ignored; a consumer MessageMax
  clamping the request down only makes the check more conservative. Both errors land safe.

Suspend semantics:
- scheduler predicate: ... AND NOT suspended
- unsuspend admin op RECOMPUTES next_scheduled_time from now() — resume on schedule, no
  surprise immediate fire from the stale past-due timestamp (k8s spec.suspend semantics)

## job scheduler
- 1 min poll loop (anything needing sub-minute frequency stays a long-lived worker)
- Single transaction per tick:
  - SELECT ... WHERE next_scheduled_time <= now() FOR UPDATE SKIP LOCKED
  - ProduceInTx to job_request topic (produce + advance commit atomically)
  - UPDATE next_scheduled_time computed FROM now() -- missed runs are dropped, fire once
- Scheduler produces EVERY due slot unconditionally — concurrency policy is enforced at
  consume time (see exclusive consumption below). No last_message_id / resolved-check on
  cron_job (rejected: dual enforcement layers).

## job_request topic
- compacted; compaction key = cron_job identity (bounds topic at ~1 row per job)
- routing_key: cronjob.<handler>.<name>  (handler first -> bindings are cronjob.<handler>.*)
- idempotency key: deterministic v7-layout UUID — scheduled_time in the 48 timestamp bits,
  hash(cron_job.id) in the rest (preserves index locality). Manual "run now" uses a RANDOM
  key so it doesn't dedupe against the slot.
- Message { data, metadata } — stamped with the slot time it represents
- handler consumes with existing retry machinery; handlers must respect ctx cancellation
  (WorkTimeout); overlap guarantee is only as strong as ctx-respect (abandoned goroutines
  can't be killed — same caveat k8s escapes only by SIGKILLing pods)

## exclusive consumption (concurrency policy — the generic primitive)

Per-MESSAGE, set at produce time via MessageOptions (see message options section):

  MessageOptions.Exclusive: None | Skip | Defer   (persisted with the message)

- Skip  = k8s Forbid: key busy -> slot is dropped, never runs
- Defer = reconciler/workqueue semantic: key busy -> park in exception window, run latest when free
- rule: all messages sharing a compaction key MUST carry the same policy (upheld naturally:
  one scheduler produces all messages for a cron_job)
- exclusive produce onto a non-compacted topic errors at produce time

Dispatch predicate (base.go, before consumerFunc) — ORDER IS LOAD-BEARING, supersede
check MUST run before the lease attempt or a hot key floods the exception window:

  if CompactionHead(msg.key) > msg.rank:  resolveSuperseded(msg)      # stale slot
  else if !acquireKeyLease(msg):
      Skip:  resolveSuperseded(msg)                                   # slot dropped
      Defer: resolveDeferred(msg)                                     # exception window
  else:
      runHandler(msg); recordOutcome + releaseKeyLease                # ONE transaction

Invariant: a message runs iff it is the compaction head at the moment it wins the key lease.

key_lease table (tiny, hot; NOT partitioned, NOT append-only — locks are irreducibly
mutable state; append-only acquire/release event log rejected, permanent rows rejected):

  CREATE TABLE key_lease (
    topic_id       BIGINT NOT NULL,
    consumer_group TEXT NOT NULL,
    compaction_key TEXT NOT NULL,
    lease_token    UUID NOT NULL,        -- PER-ATTEMPT fence (reuse claim lease token identity)
    expires_at     TIMESTAMPTZ NOT NULL, -- resolved WorkTimeout + Grace + AckMargin
    PRIMARY KEY (topic_id, consumer_group, compaction_key)
  );

  -- acquire (steal-on-expiry built in; rowcount 0 = busy):
  INSERT ... ON CONFLICT (pk) DO UPDATE SET lease_token=..., expires_at=...
    WHERE key_lease.expires_at < now();
  -- release (same txn as RecordSuccess/RecordFailure):
  DELETE FROM key_lease WHERE ... AND lease_token = $mytoken;

- fence by lease_token, NOT message_id: same message re-attempted after a steal would let
  the stale holder free the new attempt's lease -> concurrent runs under Forbid (real bug)
- per-group IgnoreExclusive bool on consumer Config (zero value = honor, which is the safe
  default for executors) — observer/audit groups set true so an executor's policy can't
  silently drop their messages. (Renamed from HonorExclusive: sparse-struct zero value must
  be the safe case.)
- Defer bookkeeping: 'deferred' exception kind, distinct from failure — own short/fixed
  retry cadence, does NOT consume the message's resolved Retry.MaxRetries attempts. No
  defer-TTL needed: a crash-looping holder burns its own attempts and frees the key; newer
  slots supersede parked ones (torture lab must prove the wedged-key case)
- superseded terminals ALWAYS recorded in delivery log (debugging Forbid = "why didn't
  12:05 run?"); cron status derives last-run/last-skipped from the log, no new columns

new vocabulary (one concept one name): terminal kind 'superseded', exception kind 'deferred'

---

# message options & config layering — SETTLED DESIGN (2026-07-28)

Resolution model, one sentence: consumer bounds > message > consumer defaults > system
defaults. Messages REQUEST, consumers PROTECT THEMSELVES, defaults FILL SILENCE.

## retry.Policy consolidation (prerequisite sweep)
- ONE construct for message redelivery: *retry.Policy {MaxRetries, BaseDelay, MaxDelay,
  Exponent}. The count lives INSIDE the policy — delete the root consumer.Config.MaxAttempts
  field; Retry.MaxRetries is now actually read (today it's dead weight in the Backoff role).
- The infra-retry family (consumer.Config.Retry, producer.Config.Retry, datastore Retry,
  metrics Retry — transient-error loops for the process's own Postgres calls via retry.Wrap)
  NEVER enters the message-options system. Consumer-only mechanics, untouched.
- consumer.Config.Backoff / datastore MessageRetry are the delivery-curve role — they fold
  into Config.Message.Retry below.

## the shape

  type MessageOptions struct {        // sparse; zero value = "consumer decides"
      Exclusive   ExclusivePolicy     // None | Skip | Defer
      WorkTimeout time.Duration
      Retry       *retry.Policy      // nil = consumer default; field-wise merge
  }

  producer side:
    ProduceOptions{ IdempotencyKey, RoutingKey, Message: MessageOptions{...} }
    producer.Config.Message MessageOptions   // per-topic defaults, merged under every produce

  consumer side:
    type Config struct {
        // root = consumer-only mechanics: QueueMargin, AckMargin, WorkTimeoutGrace,
        // ShutdownTimeout, Retry (infra), ... — knobs a message could never own
        Message         MessageOptions  // defaults  — fill what the message left unset
        MessageMin      MessageOptions  // floors    — consumer wins upward
        MessageMax      MessageOptions  // ceilings  — consumer wins downward
        IgnoreExclusive bool            // veto — not a bound, deliberately not in the trio
    }

  resolution (field-wise):
    resolved = clamp( fill(msg, cfg.Message), cfg.MessageMin, cfg.MessageMax )

## why this shape
- Override fields are the SAME struct as the message options -> names can never drift;
  a new MessageOptions field is automatically defaultable + boundable, compiler-enforced.
  Clamp DIRECTION is structural (which trio field you set), not named (no MaxWorkTimeout /
  MinBackoff names).
- Direction follows the consumer's risk: WorkTimeout bounds via MessageMax (can't demand
  more runtime than the group sized leases for); Retry.BaseDelay bounds via MessageMin
  (threat = 1ms curve hot-looping the group); Retry.MaxRetries via MessageMax.
- Exclusive is an enum, not ordered: Validate() REJECTS it in MessageMin/MessageMax; the
  veto is the flat IgnoreExclusive bool (a veto is not a bound — don't force the shape).
- Zero values: MessageMin/MessageMax zero = unconstrained; Message zero = system defaults
  via WithDefaults(); empty Config behaves like today.
- Rejected: consumer-supplied resolver hook (policy-as-code: can't introspect, can't show
  in status, can't validate at Register). Rejected: separate Overrides struct with
  Max*/Min*-named fields (names drift from the fields they override).

example:
  consumer.Config{
      Message:    MessageOptions{Retry: &retry.Policy{MaxRetries: 3, BaseDelay: time.Second}},
      MessageMin: MessageOptions{Retry: &retry.Policy{BaseDelay: time.Second}},
      MessageMax: MessageOptions{WorkTimeout: 8*time.Minute, Retry: &retry.Policy{MaxRetries: 5}},
  }
  message:  WorkTimeout=10m, Retry={MaxRetries: 8, BaseDelay: 10ms}
  resolved: WorkTimeout=8m (clamped), MaxRetries=5 (capped), BaseDelay=1s (floored),
            MaxDelay/Exponent from Message default

## discipline rule
An option joins MessageOptions only when a real producer needs per-message variance, and
joins the bounds only when a real consumer needs to distrust messages. The root/Message
boundary IS the consumer-only/shared classification, made structural.

## migration cost (budget for it)
Root Config.WorkTimeout, Config.Backoff, Config.MaxAttempts move into Config.Message.* —
touches every existing consumer construction site. Rename sweep, not pure addition.
Grep labs for hand-copied config/queries while sweeping (labs go silently stale).

---

## open
- implementation-shaped (deferred until work is picked up): consumer-side compaction-head
  read (inline datastore method per house style, no shared pkg), entity enrollment inserts
  in topic/consumer_group Register paths, MessageOptions persistence shape on the message row (typed
  columns vs metadata — internals TBD), torture-lab coverage (two consumers fighting one
  key through crash / lease-steal / supersession chains / wedged-key starvation)
