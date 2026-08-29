package datastore

import "context"

// CronJobSnapshots is every cron job (config row joined to its cursor) with its owner columns and schedule
// state.
func (d *MetricsDatastore) CronJobSnapshots(ctx context.Context) ([]CronJobSnapshotData, error) {
	var jobs []CronJobSnapshotData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		jobs, err = d.cronJobSnapshots(ctx)
		return err
	})
	return jobs, err
}

func (d *MetricsDatastore) cronJobSnapshots(ctx context.Context) ([]CronJobSnapshotData, error) {
	sql := `
		-- vulkan: metrics.cronJobSnapshots
		SELECT
			j.name,
			COALESCE(j.system_id, t.system_id, 0),                       -- j.system_id is NULL unless system-owned
			COALESCE(j.topic_id, g.topic_id, 0),                         -- j.topic_id is NULL unless topic-owned
			COALESCE(j.consumer_group_id, 0),                            -- j.consumer_group_id is NULL unless group-owned
			COALESCE(t.name, ''),
			COALESCE(g.name, ''),
			j.schedule,
			j.suspended,
			c.next_scheduled_at,
			c.last_scheduled_at,
			EXTRACT(EPOCH FROM (now() - c.next_scheduled_at)) AS due_for_secs
		FROM cron_job_config j
		JOIN cron_job_cursor c ON c.cron_job_id = j.id
		LEFT JOIN consumer_group_config g ON g.id = j.consumer_group_id
		LEFT JOIN topic_config t ON t.id = COALESCE(j.topic_id, g.topic_id)     -- group rows reach their topic through the group
		ORDER BY j.name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []CronJobSnapshotData
	for rows.Next() {
		var data CronJobSnapshotData
		if err := rows.Scan(&data.Name, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.TopicName, &data.GroupName,
			&data.Schedule, &data.Suspended, &data.NextScheduledAt, &data.LastScheduledAt, &data.DueForSecs); err != nil {
			return nil, err
		}
		jobs = append(jobs, data)
	}
	return jobs, rows.Err()
}
