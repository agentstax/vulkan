package datastore

import (
	"context"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// IsCompacted reports whether topicId has ever seen a keyed publish -- any
// compaction_head row means latest-per-key winners outlive retention.
func (d *MetricsDatastore) IsCompacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		compacted, err = d.isCompacted(ctx, topicId)
		return err
	})
	return compacted, err
}

func (d *MetricsDatastore) isCompacted(ctx context.Context, topicId int64) (bool, error) {
	sql := fmt.Sprintf(`
		-- vulkan: metrics.isCompacted
		SELECT EXISTS (SELECT 1 FROM %s);
	`, iTopic.CompactionHeadTable(topicId))
	var compacted bool
	err := d.Datastore.Pool.QueryRow(ctx, sql).Scan(&compacted)
	return compacted, err
}

// SchemaVersionCounts is every payload version present in the topic's log,
// with its row count and how many compaction heads point at it.
func (d *MetricsDatastore) SchemaVersionCounts(ctx context.Context, topicId int64) ([]SchemaVersionCountRow, error) {
	var counts []SchemaVersionCountRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		counts, err = d.schemaVersionCounts(ctx, topicId)
		return err
	})
	return counts, err
}

func (d *MetricsDatastore) schemaVersionCounts(ctx context.Context, topicId int64) ([]SchemaVersionCountRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: metrics.schemaVersionCounts
		SELECT
			m.schema_version,
			count(*) AS messages,
			count(h.compaction_key) AS compaction_heads
		FROM %s m
		LEFT JOIN %s h ON h.head_id = m.id
		GROUP BY m.schema_version
		ORDER BY m.schema_version;
	`, iTopic.MessageLogTable(topicId), iTopic.CompactionHeadTable(topicId))
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[SchemaVersionCountRow])
}

// GroupSchemaVersionLag is, per consumer group on the topic, how many rows at
// schemaVersion sit above the group's committed cursor and how many of its
// exception rows at that version are unresolved.
func (d *MetricsDatastore) GroupSchemaVersionLag(ctx context.Context, topicId int64, schemaVersion int64) ([]GroupSchemaVersionLagRow, error) {
	var lags []GroupSchemaVersionLagRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		lags, err = d.groupSchemaVersionLag(ctx, topicId, schemaVersion)
		return err
	})
	return lags, err
}

func (d *MetricsDatastore) groupSchemaVersionLag(ctx context.Context, topicId int64, schemaVersion int64) ([]GroupSchemaVersionLagRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: metrics.groupSchemaVersionLag
		SELECT
			g.name AS consumer_group,
			(
				SELECT count(*) FROM %[2]s m
				WHERE m.schema_version = $1
					AND m.id > c.committed
			) AS unconsumed,
			(
				SELECT count(*) FROM %[3]s e
				JOIN %[2]s m ON m.id = e.message_id
				WHERE e.consumer_group_id = c.consumer_group_id
					AND m.schema_version = $1
					AND e.status IN ('ready', 'inflight', 'deferred')
			) AS unresolved_exceptions
		FROM %[1]s c
		JOIN consumer_group_config g ON g.id = c.consumer_group_id
		ORDER BY g.name;
	`, iTopic.ConsumerGroupCursorTable(topicId), iTopic.MessageLogTable(topicId), iTopic.ExceptionQueueTable(topicId))
	rows, err := d.Datastore.Pool.Query(ctx, sql, schemaVersion)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[GroupSchemaVersionLagRow])
}
