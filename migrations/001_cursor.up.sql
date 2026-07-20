-- consumer group cursors for tracking offset in message_log
CREATE TABLE IF NOT EXISTS cursor (
  consumer_group TEXT NOT NULL,
  topic_id BIGINT NOT NULL, -- a group tracks an independent cursor per topic
  claimed BIGINT NOT NULL DEFAULT 0, -- the read frontier 'inflight' work
  committed BIGINT NOT NULL DEFAULT 0, -- every message id > committed is in an end state done / dead
  -- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
  -- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
  settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
  pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
  pending_xmax XID8, -- txid fence read in the same snapshot as pending_head
  PRIMARY KEY (consumer_group, topic_id)
);