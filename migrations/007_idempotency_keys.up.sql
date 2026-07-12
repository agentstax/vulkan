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
-- Swept by the Janitor on IdempotencyKeyTTL, independent of message_log
-- retention -- created_at (not idempotency_key) is the sweep's cutoff column
CREATE TABLE IF NOT EXISTS idempotency_keys (
  topic_id        BIGINT    NOT NULL, -- PK
  idempotency_key UUID      NOT NULL, -- PK
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (topic_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idempotency_keys_topic_created_at
  ON idempotency_keys (topic_id, created_at);
