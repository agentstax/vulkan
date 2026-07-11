-- O(1) index for compaction's "is this the latest for its key" lookup --
-- upserted synchronously in the same transaction as every keyed publish,
-- never a background job. Shared across topics (not per-topic like
-- message_log) since it scales with DISTINCT compaction_key count, not
-- total message volume.
CREATE TABLE IF NOT EXISTS latest_keys (
  topic_id       BIGINT NOT NULL, -- PK
  compaction_key TEXT   NOT NULL, -- PK
  latest_id      BIGINT NOT NULL, -- highest message_log id seen for this key so far
  PRIMARY KEY (topic_id, compaction_key)
);
