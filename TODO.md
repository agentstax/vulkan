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

consider abstracting out the claim fence transaction xmax logic into its own async ticker and claimers read from shared mem var. It would better abstract away that complex logic and we can have the poll rate much faster because it is a pretty cheap query

A specific DeadLetterTopic Consumer. You can consume on events to DLQ

A Shadow or Mirror functionality - ie watch exactly the same cursor for cursor group (if could potentially watch same message by message that would be better but probably not possible)

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
