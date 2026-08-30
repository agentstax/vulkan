package datastore

import "context"

// ScheduleSnapshots is every schedule (config row joined to its cursor) with its owner columns and schedule
// state.
func (d *MetricsDatastore) ScheduleSnapshots(ctx context.Context) ([]ScheduleSnapshotData, error) {
	var schedules []ScheduleSnapshotData
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		schedules, err = d.scheduleSnapshots(ctx)
		return err
	})
	return schedules, err
}

func (d *MetricsDatastore) scheduleSnapshots(ctx context.Context) ([]ScheduleSnapshotData, error) {
	sql := `
		-- vulkan: metrics.scheduleSnapshots
		SELECT
			j.name,
			COALESCE(j.system_id, t.system_id, 0),                       -- j.system_id is NULL unless system-owned
			COALESCE(j.topic_id, g.topic_id, 0),                         -- j.topic_id is NULL unless topic-owned
			COALESCE(j.consumer_group_id, 0),                            -- j.consumer_group_id is NULL unless group-owned
			COALESCE(t.name, ''),
			COALESCE(g.name, ''),
			j.expression,
			j.suspended,
			c.next_scheduled_at,
			c.last_scheduled_at,
			EXTRACT(EPOCH FROM (now() - c.next_scheduled_at)) AS due_for_secs
		FROM schedule_config j
		JOIN schedule_cursor c ON c.schedule_id = j.id
		LEFT JOIN consumer_group_config g ON g.id = j.consumer_group_id
		LEFT JOIN topic_config t ON t.id = COALESCE(j.topic_id, g.topic_id)     -- group rows reach their topic through the group
		ORDER BY j.name;
	`
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []ScheduleSnapshotData
	for rows.Next() {
		var data ScheduleSnapshotData
		if err := rows.Scan(&data.Name, &data.SystemId, &data.TopicId, &data.ConsumerGroupId, &data.TopicName, &data.GroupName,
			&data.Expression, &data.Suspended, &data.NextScheduledAt, &data.LastScheduledAt, &data.DueForSecs); err != nil {
			return nil, err
		}
		schedules = append(schedules, data)
	}
	return schedules, rows.Err()
}
