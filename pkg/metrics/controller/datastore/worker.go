package datastore

import "context"

// WorkerSnapshots is every worker row with its owner columns and aggregated
// worker_instance liveness.
func (d *MetricsDatastore) WorkerSnapshots(ctx context.Context) ([]WorkerSnapshotRow, error) {
	var workers []WorkerSnapshotRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		workers, err = d.workerSnapshots(ctx)
		return err
	})
	return workers, err
}

func (d *MetricsDatastore) workerSnapshots(ctx context.Context) ([]WorkerSnapshotRow, error) {
	sql := `
		-- vulkan: metrics.workerSnapshots
		SELECT
			w.name,
			COALESCE(w.system_id, t.system_id, 0),                        -- w.system_id is NULL unless system-owned
			COALESCE(w.topic_id, g.topic_id, 0),                          -- w.topic_id is NULL unless topic-owned
			COALESCE(w.consumer_group_id, 0),                             -- w.consumer_group_id is NULL unless group-owned
			COALESCE(t.name, ''),
			COALESCE(g.name, ''),
			w.target_instances,
			COUNT(i.id) FILTER (WHERE i.expires_at > now()) AS live_instances,
			COALESCE(MAX(i.attempts) FILTER (WHERE i.expires_at > now()), 0) AS max_attempts,
			COALESCE(EXTRACT(EPOCH FROM (now() - MAX(i.expires_at))), 0) AS unclaimed_for_secs  -- dead rows feed this until something deletes them
		FROM worker_config w
		LEFT JOIN consumer_group_config g ON g.id = w.consumer_group_id
		LEFT JOIN topic_config t ON t.id = COALESCE(w.topic_id, g.topic_id)      -- group rows reach their topic through the group
		LEFT JOIN worker_instance i ON i.worker_id = w.id
		GROUP BY w.id, w.name, w.system_id, w.topic_id, w.consumer_group_id, w.target_instances, t.system_id, t.name, g.topic_id, g.name
		ORDER BY t.name, w.name, g.name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []WorkerSnapshotRow
	for rows.Next() {
		var data WorkerSnapshotRow
		if err := rows.Scan(&data.Name, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.TopicName, &data.GroupName,
			&data.TargetInstances, &data.LiveInstances, &data.MaxAttempts, &data.UnclaimedForSecs); err != nil {
			return nil, err
		}
		workers = append(workers, data)
	}
	return workers, rows.Err()
}
