---
status: accepted
date: 2026-08-30
phase: "pre-v1"
---

# 0622 — IdempotencyKey is a caller string, resolved to the claim table's UUID

**Context.** `ProduceOptions.IdempotencyKey` was a `uuid.UUID`, so a caller whose natural key is a string (an upstream event id) had to hand-derive a v5 UUID — playground 09's worst trap, and against the industry norm (Stripe, SQS FIFO, the IETF Idempotency-Key draft all take opaque strings). The claim table's UUID PK is load-bearing: bench/idempotency showed right-edge v7 inserts flat to 10M rows while random-shaped keys cost 2.2x CPU / 10x WAL. This reverses the 2026-08-29 ROADMAP settlement "IdempotencyKey stays uuid.UUID".

**Decision.** The field becomes `string`. The controller resolves it to the UUID the claim table stores: `""` mints a fresh v7 (the hot path, unchanged); a string that parses as a UUID is used verbatim; anything else hashes to a deterministic UUIDv5 under a fixed namespace frozen in producer/controller. `ProduceFunc` hands its closure the resolved key's text, so persisting and re-supplying that string dedups — the parse-verbatim branch is what makes that round trip land on the same claim row. Schema untouched.

**Consequences.** Callers pass their upstream id as-is; the v7-not-v4 warning narrows to hot-path producers minting their own keys (a hashed string is random-shaped — fine below millions of unexpired claims). **Rejected:** storing the original string beside the UUID — with no message_id in the claim table a lookup only answers "claimed at T" inside the TTL window, not worth the column. **Rejected:** hashing every supplied string including UUIDs — the minted key's text would then hash to a different row on re-supply, breaking the crash-restart story the field exists for.
