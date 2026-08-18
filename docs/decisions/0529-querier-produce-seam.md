---
status: accepted
date: 2026-08-17
phase: 14b
---

# One Querier contract; the produce transaction is the one sanctioned crossing

## Context

The codebase draws exactly one line worth typing: owns the transaction
boundary vs runs statements inside someone else's. Yet producer datastore
publics took pgx.Tx, the producer Tx interface was a second hand-rolled
Querier, the controller opened InTransaction's transaction itself, and
cronscheduler's datastore exported producer.Tx-taking pass-throughs fed by
an i.datastore.Datastore reach-through.

## Decision

- datastore.Querier is the one statement seam: Exec/Query/QueryRow/
  SendBatch/CopyFrom — what pool, conn, and tx all share, minus
  Begin/Commit/Rollback (sqlc's DBTX draws the same line). No Beginner
  interface, no wrapping of pgx result types; *pgxpool.Conn stays concrete
  in migrate (the advisory lock pins a session).
- Producer Tx = { datastore.Querier; Raw() pgx.Tx }. Raw is the user escape
  hatch only — internal callers pass Tx/Querier down. newTx is private and
  concrete; Begin/Commit live in the datastore (InTransaction included).
- A method built to run inside the produce transaction takes Tx (runs a
  ProduceFunc) or q datastore.Querier (statements only); pgx.Tx appears
  only as Begin-owning locals and in the Tx adapter.
- Cronscheduler is a USER of the public InTransaction seam (option a), not
  an owner: its datastore methods narrow to q datastore.Querier, the
  execution holds its own *PostgresDatastore, and the CONVENTIONS
  no-crossing rule carries the produce-seam carve-out explicitly.
  Rejected (b): cronjob datastore owning Begin with producer work injected
  — inverts the dependency and bypasses the front door.

## Consequences

- Both chunks built 2026-08-17; the unexport-fields sweep is unblocked.
- Datastore methods running inside the produce transaction keep their
  public → same-named-private pairs even without a Wrap — the pair pattern
  is uniform across every datastore, pass-through or not (user-settled).
- AppendMessageBatch keeps its (results, failedIdx, error) shape — a
  BatchItemError type was built and rejected in review.
- CopyFrom has no internal caller — it stays because Tx is a user surface
  (bulk-load atomically with a produce is a deliberate API promise).
