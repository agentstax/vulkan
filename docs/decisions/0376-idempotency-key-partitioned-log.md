---
status: superseded
date: 2026-07-28
phase: "13"
---

# 0376 — Always-on idempotency: make idempotency_key the enforced identity of message_log

**Context.** `idempotencyKey` and `skipIdempotency` sitting together on producer options felt like one knob split in two, and the companion `idempotency_key` table costs a second insert per publish plus a janitor DELETE-sweep.

**Decision.** The settled direction: always-on `idempotency_key`, no `skipIdempotency`, no separate table — `message_log` becomes `PARTITION BY RANGE` on the key (or `uuid_extract_timestamp(idempotency_key)`, a pure function of the key, so the composite PK still enforces uniqueness on the key alone). `id` (`BIGSERIAL`) stays unconstrained purely so `WHERE id > watermark ORDER BY id` keeps working as the consumer's correctness filter. The pruning cost (claim queries filter by `id`, which no longer prunes) is clawed back by a per-group cursor floor — the key of the group's current watermark — added as a pruning-only predicate, clamped by a hard ceiling on the whole `AppendMessage` call (`context.WithTimeout` across every retry) so `now() - ceiling - clockSkewBuffer` is a structural guarantee, with a new dedicated tight TTL rather than `idempotency_key_ttl_ns`.

**Consequences.** The ceiling also closes a latent gap in the separate-table design: a `producerFunc` outliving the dedup TTL has its claim row swept mid-retry, silently defeating dedup. **Rejected:** an `(idempotency_key, id)` composite PK on the id-partitioned table — `id` is an independent counter and enforces nothing. **Rejected:** a live `pg_stat_activity`-derived floor — it cannot see a transaction before `Begin()` reaches the server or during retry backoff. Superseded: the later idempotency-key redesign kept a dedicated per-topic `idempotency_key` table and deliberately did not partition it or correlate it with message ids.
