package datastore

import "context"

// ScheduleSnapshots is every schedule (config row joined to its cursor) with
// its target topic and schedule state.
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
			j.system_id,
			j.topic_id,
			t.name AS topic_name,
			j.expression,
			j.suspended,
			c.next_scheduled_at,
			c.last_scheduled_at,
			EXTRACT(EPOCH FROM (now() - c.next_scheduled_at)) AS due_for_secs
		FROM schedule_config j
		JOIN schedule_cursor c ON c.schedule_id = j.id
		JOIN topic_config t ON t.id = j.topic_id
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
		if err := rows.Scan(&data.Name, &data.SystemId, &data.TopicId, &data.TopicName,
			&data.Expression, &data.Suspended, &data.NextScheduledAt, &data.LastScheduledAt, &data.DueForSecs); err != nil {
			return nil, err
		}
		schedules = append(schedules, data)
	}
	return schedules, rows.Err()
}
