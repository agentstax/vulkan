package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/topic"
)

// SweepExpiredWaitingDeclarations deletes waiting binding_config_log rows whose
// attempt ran more than ttl ago -- one batched DELETE per topic's table, at
// most batchSize rows each -- and returns how many were deleted in total.
func (d *JanitorDatastore) SweepExpiredWaitingDeclarations(ctx context.Context, ttl time.Duration, batchSize int) (int64, error) {
	var swept int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		swept, err = d.sweepExpiredWaitingDeclarations(ctx, ttl, batchSize)
		return err
	})
	return swept, err
}

func (d *JanitorDatastore) sweepExpiredWaitingDeclarations(ctx context.Context, ttl time.Duration, batchSize int) (int64, error) {
	topicIds, err := d.listGroupTopicIds(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-ttl)

	var swept int64
	for _, topicId := range topicIds {
		topicSwept, err := d.sweepTopicWaitingDeclarations(ctx, topicId, cutoff, batchSize)
		if err != nil {
			return 0, err
		}
		swept += topicSwept
	}
	return swept, nil
}

func (d *JanitorDatastore) sweepTopicWaitingDeclarations(ctx context.Context, topicId int64, cutoff time.Time, batchSize int) (int64, error) {
	// a declarer's newest waiting id is protected even past the cutoff, so
	// a dead waiter stays visible in listings. Installed rows are never
	// touched.
	sql := fmt.Sprintf(`
		-- vulkan: consumergroupjanitor.sweepTopicWaitingDeclarations
		WITH newest_waiting AS (
			SELECT consumer_group_id, declared_by, max(id) AS newest_id
			FROM %[1]s.%[2]s
			WHERE status = 'waiting'
			GROUP BY consumer_group_id, declared_by
		)
		DELETE FROM %[1]s.%[2]s
		WHERE id IN (
			SELECT binding_config_log.id
			FROM %[1]s.%[2]s binding_config_log
			JOIN newest_waiting ON newest_waiting.consumer_group_id = binding_config_log.consumer_group_id
				AND newest_waiting.declared_by = binding_config_log.declared_by
			WHERE binding_config_log.status = 'waiting'
			AND binding_config_log.attempted_at < $1
			AND binding_config_log.id < newest_waiting.newest_id
			LIMIT $2
		);
	`, d.Datastore.Schema, topic.BindingConfigLogTable(topicId))
	tag, err := d.Datastore.Pool.Exec(ctx, sql, cutoff, batchSize)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// listGroupTopicIds is every topic id with registered groups. A binding_config_log
// row cascades with its group, so these topics cover every declaration.
func (d *JanitorDatastore) listGroupTopicIds(ctx context.Context) ([]int64, error) {
	sql := fmt.Sprintf(`
		-- vulkan: consumergroupjanitor.listGroupTopicIds
		SELECT DISTINCT topic_id
		FROM %[1]s.consumer_group_config
		ORDER BY topic_id;
	`, d.Datastore.Schema)
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topicIds []int64
	for rows.Next() {
		var topicId int64
		if err := rows.Scan(&topicId); err != nil {
			return nil, err
		}
		topicIds = append(topicIds, topicId)
	}
	return topicIds, rows.Err()
}
