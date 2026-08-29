---
status: accepted
date: 2026-08-29
phase: pre-v1
---

# 0617 — ordered delivery per key; concurrency values parallel / exclusive / ordered

## Context

`defer` is exclusivity only: the key lease is held for the run and
released when the outcome is recorded, so a failed delivery's `ready`
row leaves the key free and the next same-key message runs before the
retry (playground scenario 10 reorders balance deltas). The ROADMAP
sketch defined "earlier unresolved" as exception_queue rows; tracing
it showed a second case -- an earlier same-key message claimed but not
yet recorded has no row anywhere, so under MessageConcurrency > 1 or
two instances the later one could still run first.

Naming: `allow` / `defer` are verbs for what the new message does;
adding a guarantee word (`ordered`) mixed registers, and no single
third word paired with `defer`. Peers name the key, not a policy
ladder (Pub/Sub ordering key, SQS message group, Service Bus session,
Pulsar Key_Shared) -- none has three rungs.

## Decision

- Values renamed to what the key permits, loosest to strictest:
  `parallel` (zero) / `exclusive` / `ordered`. Row status `deferred`
  stays -- it names what happened to the row, not the policy.
- `ordered`: a keyed message runs only after every earlier same-key
  message is resolved for this group (success, dead, superseded), and
  never concurrently with one. `dead` does not hold the lane;
  MaxRetries bounds a stall. Needs a message key; refuses
  Compaction.Enable at produce time.
- exception_queue gains `message_key` and `concurrency` (the resolved
  policy, written at insert) plus an index on (consumer_group_id,
  message_key, message_id). The exception claim skips an ordered row
  while an earlier unresolved same-key row exists.
- The ordered key-lease claim adds two predicates: no earlier
  unresolved same-key exception row; no same-key message_log id in
  (committed, id) outside the claimer's own range. No row back is the
  existing busy verdict -> the existing deferred outcome.
- Inside one instance's range, same-key ordered messages are chained
  in memory and run back-to-back in id order in one goroutine; a
  predecessor's failure/delay/deferral defers the rest of the chain
  without running it.

## Consequences

- Rejected: bool/enum-only naming (`ConcurrencyOrdered` beside
  `defer`), resolution logic in the claim SQL (`COALESCE($override,
  options->>'concurrency')`), defer-everything inside a range (one
  message per ClaimPollRate on a hot key).
- Stored `options->>'concurrency'` strings change (pre-v1 drop and
  recreate); `allow`/`defer` join the Vocabulary registry.
- Cost: a deferred ordered message runs one exception poll after its
  predecessor resolves; a cross-instance wait lasts until the other
  range commits and the cursor advancer moves committed.
