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

*REALLY WANT* consider abstracting out the claim fence transaction xmax logic into its own async ticker and claimers read from shared mem var. It would better abstract away that complex logic and we can have the poll rate much faster because it is a pretty cheap query

A specific DeadLetterTopic Consumer. You can consume on events to DLQ

A Shadow or Mirror functionality - ie watch exactly the same cursor for cursor group (if could potentially watch same message by message that would be better but probably not possible)

Consider storing messages a binary data (maybe as an compressed option). Would prevent users from easily searching fields in db but could make for more efficent network requests and faster unmarshalling if we did something similar to protocol buffers (grpc). https://github.com/Apaezmx/pgproto

How could we generalize this system to be a debezium like replication system. ie which a generic table and stream data to ??? (different table etc)

*REALLY WANT* our exception claiming logic really does need a complete revamp. It should better follow our topic / cursor logic instead of being queue based we end up having a lot of custom logic because of that. The problem with converting it is we would really need the async ordered-index claim table such that we could have a materialized ordered view. The thought is with cursor: exception message is tried and fails -> pushed to unordered topic -> unordered topic materialized new retry attempt to top of ordered-index which is picked up soon because materilization is only slightly ahead of claiming (some buffer size like how our claiming buffer works)

## BEFORE V1

review / refine the comments in fanOut (pkg/consumer/datastore.go) -- both the Go
comments and the ones inside snapshotSql/scanSql. remember SQL comments ship to
Postgres, so every comment edit needs a live lab re-run (routing-lab is the cheapest).

replace the two `SELECT * FROM cursor` queries (pkg/consumer/datastore.go:1035,
pkg/consumer/datastore_lifecycle.go:54) with explicit column lists --
conventions.md now bans SELECT * outright: any column ADD breaks old binaries
via pgx scan-count mismatch, turning even additive migrations into breaking
ones for exactly the rolling-deploy window that should be safe

group / order config options and placement of fields in tables via likeness. ie similiar fields should be logically next to each other for easier understanding.

Need to confirm that us manually creating UUIDv7 via go code is compatible with how PG18 better optimizes storage / pages with their built in UUIDv7(). ie their isn't some metadata field that somehow gets set which tells tuples to be writen sequentially in pages it is just the values themselves

we need to test compaction key with default produce and determine if deadlock contention by reverse ordered transactions is a problem or not.
  - ie and what extreme (or not extreme) example would it truly become a problem for users or can the system self heal through retries
  - I know we can move these users to ProduceFunc but just to know

does pgx send sql comments to db? if so is that wasted bytes over the network we should try to limit

reconsider if latest_key should be a per topic latest_key_(topic_id) table. High update churn from many tables could be an issue. Should really do an evaluation on all system tables cursor / lease / binding / topic / latest_key tables

see if our new Querier interface could be used to make stronger contracts with internal or public code

Need to consider how in the case where janitor is not fast enough to create a new partition and there are many concurrent partitions. The instance one producers hits the retry create new partitions point it is likely many producers will hit the exact same and potentially spam the database with the same request. The question is, is that okay can postgres handle that or do we need a way to have a blocking single request and other instances or producer waits for it to complete

lab binaries should be produced into a /bin folder that it .gitignored except for a .gitkeep

Consider creating a DefaultProducer and DefaultConsumer for easier quickstarts which has comments and maybe a log statement recommending not to use in prod

refactor out queue concurrency.Queue[Buffered], poolLimiter concurrency.PoolLimiter from consumer

need to rename consumer waterline stuff to something like cursor.committed. Waterline is useful for understanding should not dictate code naming and terminology

Consider standardizing errors into a Handler (where), Description (why/what), Action (how to resolve if needed), Link (potential future enhancment to docs for more info).

Review code / comments in:
- pkg/metrics
- pkg/admin (health / metrics specifically) 
- pkg/consumer/metrics (comments specifically)

Still open: `pkg/admin/health.go` carries a `// TODO - probably makes more
sense to use TopicSnapshot and derive Safe / Reason from that` comment that
contradicts LEARNING_PLAN 14a's recorded decision (verdict logic deliberately
kept in admin, separate from `pkg/metrics/monitor`). Whoever picks this up:
confirm which is current before changing anything — either delete the stray
comment (settled design wins) or do the refactor and update the LEARNING_PLAN
record to match.

Need a destroy system

don't like how listDuties has a case statement joining many tables. Makes it feel like duties needs to be a higher level abstraction
that is set with a poll rate instead of these values being set on topic, system etc.

rethink making GetCompactionHead live on producer. It could want to be used and called in many different places

having to have func (d *MaintenanceDatastore) GetGroupId(ctx context.Context, name string) (int64, error) in maintenance datastore because of circular dependencies with consumer is a code smell and means we have coupled things incorrectly

pkg/migrate/(version/support.go) and pkg/migrate/datastore(system/version.go) is not in line with our dependency injection patterns
- common.Owner.name not being a required field because of above is a code smell
- having to have random SystemOwner in pkg/migrate/datastore/system.go not good
- really just the entire pkg/migrate codebase needs a comb through and update

DONT FORGET - you just spent a lot of time making the cron jobs system a lot better we should make the same time investment for long lived background workers like janitor and waterline
