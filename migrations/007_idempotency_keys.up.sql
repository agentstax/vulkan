-- Claim gate for AppendMessage retries: an INSERT ... ON CONFLICT DO NOTHING
-- against this table, checked BEFORE the message_log insert, is what lets a
-- retried publish (after an ambiguous commit -- ack lost, but the original
-- attempt actually landed) detect "this already happened" and no-op instead
-- of double-publishing. Can't be a unique constraint on message_log itself:
-- Postgres requires a partitioned table's unique constraints to include the
-- partition key (id), which would make a constraint on idempotency_key alone
-- impossible -- same reason latest_keys exists as its own table rather than
-- a constraint on message_log for compaction_key.
--
-- No retention/cleanup yet -- this grows one row per publish, unbounded.
-- Known follow-up, not solved here (same staged sequencing latest_keys used:
-- land the mechanism first, wire retention awareness into it later).
CREATE TABLE IF NOT EXISTS idempotency_keys (
  topic_id        BIGINT    NOT NULL, -- PK
  idempotency_key UUID      NOT NULL, -- PK
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (topic_id, idempotency_key)
);
