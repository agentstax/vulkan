-- Reference schema for the hybrid "waterline" managed-cursor message platform.
-- This is the END-STATE two-table split from LEARNING_PLAN.md (events log +
-- deliveries lifecycle), plus the pieces the at-scale benchmark + second audit
-- proved out: a sparse exception window, crash-safe leases, frozen-block lanes,
-- and the routing / partition / compaction columns for Phases 7-9.
--
-- It is intentionally self-contained (one file, applied via Migrate()) so the
-- reference can be stood up against a throwaway DB without golang-migrate.

DROP TABLE IF EXISTS events, cursors, leases, deliveries, bindings, processed CASCADE;
DROP TYPE IF EXISTS delivery_state CASCADE;

-- ---------------------------------------------------------------------------
-- events: the immutable, append-only log. Never deleted on consume. "offset"
-- is the position. Routing (topic/routing_key/headers) and ordering
-- (partition_key) attributes live here; payload IS NULL marks a compaction
-- tombstone (Kafka compacted-topic semantics, Phase 9).
-- ---------------------------------------------------------------------------
CREATE TABLE events (
    "offset"      BIGSERIAL    PRIMARY KEY,
    topic         TEXT,
    routing_key   TEXT,
    headers       JSONB        NOT NULL DEFAULT '{}'::jsonb,
    partition_key TEXT,
    payload       JSONB,                                 -- NULL = tombstone (compaction)
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
-- find latest-per-key fast (compaction) and scan a partition in order.
CREATE INDEX events_partition ON events (partition_key, "offset")
    WHERE partition_key IS NOT NULL;

-- ---------------------------------------------------------------------------
-- cursors: one row per (group, lane). committed = waterline (every offset <=
-- committed on this lane is resolved); claimed = read frontier. block_hi is the
-- FROZEN upper bound of a lane's contiguous block (R1/R4); NULL = single-lane
-- case (cap at the live head).
-- ---------------------------------------------------------------------------
CREATE TABLE cursors (
    consumer_group TEXT   NOT NULL,
    lane           INT    NOT NULL DEFAULT 0,
    committed      BIGINT NOT NULL DEFAULT 0,
    claimed        BIGINT NOT NULL DEFAULT 0,
    block_hi       BIGINT,
    PRIMARY KEY (consumer_group, lane)
);

-- ---------------------------------------------------------------------------
-- leases: an in-flight reserved range (lo, hi] one worker holds (d5 crash-safe
-- reclaim). A row exists ONLY while a batch is claimed-but-not-committed.
-- ---------------------------------------------------------------------------
CREATE TABLE leases (
    consumer_group TEXT        NOT NULL,
    lane           INT         NOT NULL,
    lo             BIGINT      NOT NULL,
    hi             BIGINT      NOT NULL,
    lease_until    TIMESTAMPTZ NOT NULL,
    lease_token    UUID        NOT NULL,
    reclaims       INT         NOT NULL DEFAULT 0,   -- times this range has been reclaimed (poison-batch cap)
    PRIMARY KEY (consumer_group, lane, lo)
);

-- ---------------------------------------------------------------------------
-- deliveries: the SPARSE exception window. A row exists ONLY for an offset that
-- fell off the happy path (retry/dead/out-of-order) OR was materialized for a
-- FIFO-partitioned stream (Phase 8). Success = DELETE (pop-delete) -> no 'acked'
-- state. 'dead' rows persist below the line as the per-group DLQ.
-- ---------------------------------------------------------------------------
CREATE TYPE delivery_state AS ENUM ('ready', 'inflight', 'dead');

CREATE TABLE deliveries (
    consumer_group TEXT           NOT NULL,
    lane           INT            NOT NULL DEFAULT 0,    -- R2: which lane owns this exception
    "offset"       BIGINT         NOT NULL,
    partition_key  TEXT,                                 -- Phase 8: at-most-one-in-flight-per-key
    state          delivery_state NOT NULL DEFAULT 'ready',
    attempts       INT            NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ    NOT NULL DEFAULT now(),
    lease_until    TIMESTAMPTZ,
    lease_token    UUID,
    last_error     TEXT,
    PRIMARY KEY (consumer_group, "offset")
);
-- exception drain (pop-delete): claimable rows in offset order. Covers both
-- 'ready' and 'inflight' so the claim can also reclaim expired-inflight rows
-- (a crashed exception worker) in one scan.
CREATE INDEX deliveries_claimable ON deliveries (consumer_group, "offset")
    WHERE state IN ('ready', 'inflight');
-- FIFO keyed claim: per-(group,key) unresolved rows in offset order.
CREATE INDEX deliveries_key ON deliveries (consumer_group, partition_key, "offset")
    WHERE partition_key IS NOT NULL AND state IN ('ready', 'inflight');

-- ---------------------------------------------------------------------------
-- bindings: routing rules (Phase 7). A group with no binding matches all events
-- (backward-compatible). kind='topic' uses a POSIX regex over routing_key (the
-- NATS pattern is translated to regex at bind time); kind='header' uses JSONB
-- containment over headers.
-- ---------------------------------------------------------------------------
CREATE TABLE bindings (
    id             BIGSERIAL PRIMARY KEY,
    consumer_group TEXT  NOT NULL,
    kind           TEXT  NOT NULL CHECK (kind IN ('topic', 'header')),
    pattern        TEXT,                                 -- topic: POSIX regex of the NATS pattern
    display        TEXT,                                 -- topic: original NATS pattern (for humans)
    header_match   JSONB NOT NULL DEFAULT '{}'::jsonb    -- header: containment object
);
CREATE INDEX bindings_group ON bindings (consumer_group);

-- ---------------------------------------------------------------------------
-- processed: a test-only ledger. Every time any path "processes" an offset we
-- bump times, so tests can detect gaps (times=0) and double-process (times>1
-- without an induced crash). Not used by the runtime paths.
-- ---------------------------------------------------------------------------
CREATE TABLE processed (
    consumer_group TEXT   NOT NULL,
    "offset"       BIGINT NOT NULL,
    times          INT    NOT NULL DEFAULT 0,
    PRIMARY KEY (consumer_group, "offset")
);
