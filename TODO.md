## AFTER V1

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

Look into using BRIN index for different tables

***REALLY WANT*** consider abstracting out the claim fence transaction xmax logic into its own async ticker and claimers read from shared mem var. It would better abstract away that complex logic and we can have the poll rate much faster because it is a pretty cheap query

A specific DeadLetterTopic Consumer. You can consume on events to DLQ

A Shadow or Mirror functionality - ie watch exactly the same cursor for cursor group (if could potentially watch same message by message that would be better but probably not possible)

Consider storing messages a binary data (maybe as an compressed option). Would prevent users from easily searching fields in db but could make for more efficent network requests and faster unmarshalling if we did something similar to protocol buffers (grpc). https://github.com/Apaezmx/pgproto

How could we generalize this system to be a debezium like replication system. ie which a generic table and stream data to ??? (different table etc)

***REALLY WANT*** our exception claiming logic really does need a complete revamp. It should better follow our topic / cursor logic instead of being queue based we end up having a lot of custom logic because of that. The problem with converting it is we would really need the async ordered-index claim table such that we could have a materialized ordered view. The thought is with cursor: exception message is tried and fails -> pushed to unordered topic -> unordered topic materialized new retry attempt to top of ordered-index which is picked up soon because materilization is only slightly ahead of claiming (some buffer size like how our claiming buffer works)

Could do something intresting with APIs and make a standardized API design for producing and consuming. Producing would really just be a POST request with batching as first class imo. Consuming is a bit more interesting, SSE and websockets are intresting and would need some kind of ACK mechanicsm to advance the cursor. Also adding just a basic GET or QUERY http method could be the simpler alternative. Should check out https://github.com/durable-streams/durable-streams for inspiration and a potential protocol to implement

should consider abstracting out WorkerManager into two: WorkerScheduler and WorkerSpawner
- Scheduler would act like CronJob Scheduler except it would submit two kinds of topic requests 'spawn' and 'destroy' (reconciler logic)
- WorkerSpawner would read topic and either spawn or destroy new instances depending

should consider using https://antithesis.com for production hardening and bug hunting

## BEFORE V1

comment / code review sweeps:
- review / refine the comments in fanOut
  (pkg/consumer/deliveryconsumer/controller/datastore/fanout.go) -- both the Go
  comments and the ones inside snapshotSql/scanSql. remember SQL comments ship to
  Postgres, so every comment edit needs a live lab re-run (routing-lab is the
  cheapest). (verified 2026-08-13: pgx sends comments verbatim, but the default
  QueryExecModeCacheStatement sends query text only at prepare time -- once per
  connection per unique query -- so the cost is observability noise, not network
  bytes.)
- Review code / comments in:
  - pkg/metrics
  - pkg/admin (health / metrics specifically)
  - pkg/consumer/metrics (comments specifically)
- the config-struct comment boilerplate ("pass your own *slog.Logger (own
  Handler) or anything satisfying logger.Logger...", the Retry field comment,
  and "Validate runs after WithDefaults...") is copied verbatim across ~40
  config files. improving it must be one codebase-wide sweep so the files stay
  identical -- never a per-package rewording. the "(own Handler)" fragment looks
  like a copy artifact worth fixing in that same sweep.
- Still open: `pkg/admin/health.go` carries a `// TODO - probably makes more
  sense to use TopicSnapshot and derive Safe / Reason from that` comment that
  contradicts LEARNING_PLAN 14a's recorded decision (verdict logic deliberately
  kept in admin, separate from `pkg/metrics/controller`). Whoever picks this up:
  confirm which is current before changing anything — either delete the stray
  comment (settled design wins) or do the refactor and update the LEARNING_PLAN
  record to match.

naming pass:
- need to rename consumer waterline stuff to something like cursor.committed. Waterline is useful for understanding should not dictate code naming and terminology
- our controllers have redundant verbage: topicController.GetTopic -- should just be get
- consider rename split again to:
  *Definition
  - Name() string
  *Declarer
  - Declare(*Definition) error
  *Provisioner
  - Provision(*Definition) *Instance, error
  *Instance
  - Run() error
  -- Right now we have Definition and Provisioner mixed which doesn't make sense logically
- Consider standardizing errors into a Handler (where), Description (why/what), Action (how to resolve if needed), Link (potential future enhancment to docs for more info).

refactor rest of packages in same patterns as worker and topic:
- pkg/migrate/(version/support.go) and pkg/migrate/datastore(system/version.go) is not in line with our dependency injection patterns
  - common.Owner.name not being a required field because of above is a code smell
  - having to have random SystemOwner in pkg/migrate/datastore/system.go not good
  - really just the entire pkg/migrate codebase needs a comb through and update
- pkg/consumer/consumer.go and pkg/consumer/base/{consumer,definition,execution}.go could use a bit more cleaning up in code, its not bad but it can be improved.
- should probably move pkg/context and pkg/logger into pkg/common to unify for now until finalized public surface api is achieved

config / field organization:
- group / order config options and placement of fields in tables via likeness. ie similiar fields should be logically next to each other for easier understanding.
- Need to do a pass through of config, options, vars , params etc and cleanup any dead fields
- tick rate of consumers should be set in worker metadata - in fact we need to rethink where config of these individual consumers will live long term it might all be in the metadata and that way we can split out the config per consumer type more easily and have specific metadata per consumer type

compaction API shape:
- DONE 2026-08-13: standalone head reads moved off the producer into their own
  read domain -- pkg/compaction/controller
  (GetCompactionHead/ListCompactionHeads) + datastore. The producer keeps only
  GetCompactionHeadInTx + the head upsert (the write protocol); MessageRow is
  canonical in pkg/common, aliased by producer/producercontroller.
- Still open for the v1 API review: a dedicated compacted-topic handle
  (Compact(Producer|Consumer) idea; NATS JetStream KV precedent -- one typed
  handle doing Get + CAS-produce with CompactionKey required). Would sit on
  top of the compaction controller unchanged.
- Also for the v1 API review: consumerFunc could hand users a
  common.MessageRow[Message] instead of payload-arg + context MessageMeta --
  the typed row moved to pkg/common 2026-08-13, so both sides could share it;
  the consumer's raw internal row (payload + options columns) stays its own
  struct either way.

we need to test compaction key with default produce and determine if deadlock contention by reverse ordered transactions is a problem or not.
  - ie and what extreme (or not extreme) example would it truly become a problem for users or can the system self heal through retries
  - I know we can move these users to ProduceFunc but just to know

binding lifecycle -- bindings are create-only today (Bind inserts with ON
CONFLICT DO NOTHING; ClearBindings is clear-all and nothing calls it), and
fanout ORs across a group's bindings, so a changed pattern on the same group
silently matches the union of old and new:
- make the alert Declare declarative instead of additive: one controller verb
  (SetBindings(ctx, groupId, patterns) shape) whose datastore method deletes
  what's not in the set + inserts the rest in ONE transaction -- never
  ClearBindings-then-Bind, which has a window where the group matches
  everything. re-registers then converge binding state instead of growing it.
- orphan cleanup on alert rename (old group + cursor + worker row + binding
  linger; inert but permanent debris): the group destroy verb + CLI (last 14a
  bullet) is the removal story -- alert renames are its motivating case.
- read surface DONE 2026-08-13: `vulkan alert bindings` lists every binding
  (ConsumerController.ListBindings -> MessageAdmin.ListBindings).

reconsider if latest_key should be a per topic latest_key_(topic_id) table. High update churn from many tables could be an issue. Should really do an evaluation on all system tables cursor / lease / binding / topic / latest_key tables

see if our new Querier interface could be used to make stronger contracts with internal or public code

producer proactive partition create-ahead (settled design 2026-08-11; replaces the old
janitor create-ahead deleted with pkg/maintain -- creation is the write path's job,
Kafka-style segment roll; janitor is cleanup only): fire ensureCoveringPartition when
an append's returned id range contains the partition's sentinel id (~80% mark) -- ids
are unique fleet-wide, so exactly one producer process fires per partition with zero
coordination. gate intra-process with an atomic per-topic attempted flag (or x/sync
singleflight) so batch pipelining can't double-fire while the DDL round trip is in
flight. best-effort BY DESIGN: DatastoreRetry absorbs blips, then warn and drop --
the reactive heal at the boundary is the only layer allowed to matter for
correctness, so every failure above it is drop-and-log. the heal path keeps the
residual thundering herd (every in-flight produce at a missed boundary fires
ensureCoveringPartition concurrently): cap it with
pg_try_advisory_xact_lock(topic_id, partition) before the CREATE -- one winner does
DDL, losers return instantly and just retry their insert. optional second sentinel
(~95%) only if the drop warn ever shows up in practice.

lab binaries should be produced into a /bin folder that it .gitignored except for a .gitkeep

Consider creating a DefaultProducer and DefaultConsumer for easier quickstarts which has comments and maybe a log statement recommending not to use in prod

document the "consumerFunc hard timeout, goroutine abandoned" error (CallSafely in
pkg/consumer/base/consumer.go): what it means and how to prevent it -- handle ctx.Done() inside
consumerFunc, or raise TimeoutGrace. it should be rare; the abandoned goroutine is a
real side effect, not just a warning.

decide the otel metrics exposure story -- nothing constructs pkg/metrics/metrics.Metrics
yet, so every Register*Metric gauge is dormant. the research (2026-08-09): libraries
take a Meter in config and the host app owns the exporter (River otelriver, otelgrpc;
our MetricsConfig.Meter noop-default already matches); server processes host their own
scrape endpoint (Temporal listenAddress, k8s /metrics, RabbitMQ :15692). mapping:
SystemManagerConfig grows a Meter for embedders; `vulkan manager run` grows an
opt-in --metrics-address flag that builds the sdk/metric MeterProvider + prometheus
exporter (sanctioned deps, currently imported by nothing) and serves /metrics
CLI-side, keeping sdk/metric out of the library. also settle where per-group
RegisterConsumerGroupMetric gets called -- likely consumer Register, where topic
identity resolves. when this lands, instrument the alert pipeline too: counts of
alerts published/resolved and per-topic publish failures inside an
otherwise-succeeded handler run -- cron run status only shows the joined error,
not how many topics failed or fired.

Need a destroy system

AlertRepeatInterval we need to do something with it should not live on system.
when it moves, revisit the repeat-vs-retention invariant in
alertcontroller.NewAlertController: it validates repeat against
alert.TopicConfig()'s default retention, not the live topic row, so an
operator lowering __system.alerts retention below the repeat interval
silently breaks the guarantee that an active head republishes before the
janitor sweeps it.

Need to look at the new functionality it go 1.27 before deciding on the final public API shape. Their new features with generics could actually make generics work well and completely infer they type via method and type inference.

Should think through making all tables append only by nature this would make us apache cassandra compliant have audit / debuggability for all operations and improve some levels of efficancy (partion based drops on everything) the main trade off is in complexity and in hot-path throughput explicitly for reads

should use LOCK TIMEOUT for any ALTER sql migration commands (likely just need to document this)

Need a convention for file content ordering such as:
- vars / const at top
- struct, new, validates
- public -> private pairs
- helper funcs at bottom (with helper block comment that seperates them)