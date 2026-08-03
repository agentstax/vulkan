package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// existingPartitions lists surviving message_log_<topic_id>_<n> partition numbers.
func (d *JanitorDatastore) existingPartitions(ctx context.Context, topicID int64) ([]int64, error) {
	sql := fmt.Sprintf(`
		SELECT REPLACE(c.relname, '%s_', '')::bigint AS n
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = '%s'::regclass;
	`, topic.MessageLogTable(topicID), topic.MessageLogTable(topicID))

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partitions []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}

		partitions = append(partitions, n)
	}

	return partitions, rows.Err()
}

// cursorFloor is the waterline floor: the most-lagging group's committed
// offset within this topic (nil if none exist yet). Scoped through the group
// registry so a lagging group on another topic can't block this topic's
// drops/sweeps.
func (d *JanitorDatastore) cursorFloor(ctx context.Context, topicID int64) (*int64, error) {
	sql := `
		SELECT MIN(c.committed)
		FROM cursor c
		JOIN consumer_group g ON g.id = c.consumer_group_id
		WHERE g.topic_id = $1;
	`

	var floor *int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, topicID).Scan(&floor)
	return floor, err
}

// partitionExpired reports whether a partition's newest row is past ttl.
func (d *JanitorDatastore) partitionExpired(ctx context.Context, topicID int64, n int64, ttl time.Duration) (bool, error) {
	sql := fmt.Sprintf(`
		SELECT created_at FROM %s
		ORDER BY id DESC -- rides the PK index; id order approx time order, no created_at index needed
		LIMIT 1;
	`, topic.MessageLogPartitionTable(topicID, n))

	var newest time.Time
	err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&newest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // empty -- nothing to judge, so not expired
	}
	if err != nil {
		return false, err
	}

	return time.Since(newest) >= ttl, nil
}
