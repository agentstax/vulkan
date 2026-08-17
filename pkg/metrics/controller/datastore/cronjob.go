package datastore

import "context"

// CronJobSnapshots is every cron_job row with its owner columns and schedule
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
		SELECT
			j.name,
			COALESCE(j.system_id, t.system_id, 0),                       -- j.system_id is NULL unless system-owned
			COALESCE(j.topic_id, g.topic_id, 0),                         -- j.topic_id is NULL unless topic-owned
			COALESCE(j.consumer_group_id, 0),                            -- j.consumer_group_id is NULL unless group-owned
			COALESCE(t.name, ''),
			COALESCE(g.name, ''),
			j.schedule,
			j.suspended,
			j.next_scheduled_time,
			j.last_scheduled_time,
			EXTRACT(EPOCH FROM (now() - j.next_scheduled_time)) AS due_for_secs
		FROM cron_job j
		LEFT JOIN consumer_group g ON g.id = j.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(j.topic_id, g.topic_id)     -- group rows reach their topic through the group
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
			&data.Schedule, &data.Suspended, &data.NextScheduledTime, &data.LastScheduledTime, &data.DueForSecs); err != nil {
			return nil, err
		}
		jobs = append(jobs, data)
	}
	return jobs, rows.Err()
}
