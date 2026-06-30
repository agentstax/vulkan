package waterline

import "context"

// Log compaction (Phase 9, Kafka compacted topics). Keep only the LATEST event
// per partition_key; delete superseded older events. A latest event whose
// payload IS NULL is a TOMBSTONE: the key is removed entirely.
//
// Why this is safe in the hybrid: the cursor advances by COORDINATE
// (claimed = LEAST(claimed+batch, head)) and reads whatever rows remain in
// (lo, hi]. Deleting events leaves gaps in the offset space, but a gap is just a
// range that returns fewer rows — the cursor still advances lo->hi correctly and
// the waterline math is unaffected. So compaction never corrupts progress; it
// only changes WHICH values a not-yet-arrived consumer will see, which is the
// entire point of a compacted topic.
//
// The `floor` parameter bounds how far compaction may advance:
//   - WatermarkFloor (default via CompactSafe): floor = min(committed) across ALL
//     groups, so no group ever loses a value it had not yet decided on. Safe for
//     mixed compacted/non-compacted consumers.
//   - HeadFloor: floor = head, giving true Kafka compacted-topic semantics — a
//     slow consumer may skip straight to the latest value per key. Use only when
//     every consumer of the topic accepts latest-only delivery.

// CompactResult reports what a compaction pass removed.
type CompactResult struct {
	Superseded int64 // older duplicates removed
	Tombstoned int64 // keys removed because their latest value was a tombstone
}

// CompactFloor computes the watermark-safe floor: the minimum committed offset
// across all groups (0 if there are no groups). Events at or below this floor
// have been decided on by every group.
func (l *PgLog) CompactFloor(ctx context.Context) (int64, error) {
	var floor int64
	err := l.Pool.QueryRow(ctx,
		`SELECT COALESCE(min(committed),0) FROM cursors`).Scan(&floor)
	return floor, err
}

// Head returns the current log head (max offset).
func (l *PgLog) Head(ctx context.Context) (int64, error) {
	var head int64
	err := l.Pool.QueryRow(ctx, `SELECT COALESCE(max("offset"),0) FROM events`).Scan(&head)
	return head, err
}

// Compact runs one compaction pass over a topic (topic=="" means all topics),
// removing every keyed event below `floor` that is not the latest for its key,
// and removing keys whose latest (<= floor) value is a tombstone. Unkeyed events
// (partition_key IS NULL) are never compacted. An event still referenced by ANY
// deliveries row (a live retry OR a dead/DLQ entry) is NEVER removed, even above
// the floor's protection — otherwise compacting a superseded key would orphan a
// DLQ entry's payload (committed rises past dead rows, so the floor alone does
// not protect them). Returns what was removed.
func (l *PgLog) Compact(ctx context.Context, topic string, floor int64) (CompactResult, error) {
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	defer tx.Rollback(ctx)

	var topicArg any
	if topic != "" {
		topicArg = topic
	}

	// 1) drop superseded older events (keep the latest offset per key, <= floor).
	const supersede = `
		WITH latest AS (
			SELECT partition_key, max("offset") AS keep
			FROM events
			WHERE partition_key IS NOT NULL AND "offset" <= $1
			  AND ($2::text IS NULL OR topic = $2)
			GROUP BY partition_key
		)
		DELETE FROM events e USING latest l
		WHERE e.partition_key = l.partition_key
		  AND e."offset" < l.keep AND e."offset" <= $1
		  AND ($2::text IS NULL OR e.topic = $2)
		  AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d."offset" = e."offset");`
	tag, err := tx.Exec(ctx, supersede, floor, topicArg)
	if err != nil {
		return CompactResult{}, err
	}
	res := CompactResult{Superseded: tag.RowsAffected()}

	// 2) remove keys whose surviving latest value (<= floor) is a tombstone.
	const tomb = `
		WITH latest AS (
			SELECT partition_key, max("offset") AS keep
			FROM events
			WHERE partition_key IS NOT NULL AND "offset" <= $1
			  AND ($2::text IS NULL OR topic = $2)
			GROUP BY partition_key
		)
		DELETE FROM events e USING latest l
		WHERE e.partition_key = l.partition_key AND e."offset" = l.keep
		  AND e.payload IS NULL AND e."offset" <= $1
		  AND ($2::text IS NULL OR e.topic = $2)
		  AND NOT EXISTS (SELECT 1 FROM deliveries d WHERE d."offset" = e."offset");`
	tag2, err := tx.Exec(ctx, tomb, floor, topicArg)
	if err != nil {
		return CompactResult{}, err
	}
	res.Tombstoned = tag2.RowsAffected()
	return res, tx.Commit(ctx)
}

// CompactSafe runs Compact at the watermark-safe floor (min committed across all
// groups). This never drops a value a group has not yet consumed.
func (l *PgLog) CompactSafe(ctx context.Context, topic string) (CompactResult, error) {
	floor, err := l.CompactFloor(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	return l.Compact(ctx, topic, floor)
}
