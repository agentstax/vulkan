package datastore

import (
	"context"
	"fmt"
)

// ScheduleSnapshots is every schedule (config row joined to its cursor) with
// its target topic and schedule state.
func (d *MetricsDatastore) ScheduleSnapshots(ctx context.Context) ([]ScheduleSnapshotRow, error) {
	var schedules []ScheduleSnapshotRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		schedules, err = d.scheduleSnapshots(ctx)
		return err
	})
	return schedules, err
}

func (d *MetricsDatastore) scheduleSnapshots(ctx context.Context) ([]ScheduleSnapshotRow, error) {
	sql := fmt.Sprintf(`
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
		FROM %[1]s.schedule_config j
		JOIN %[1]s.schedule_cursor c ON c.schedule_id = j.id
		JOIN %[1]s.topic_config t ON t.id = j.topic_id
		ORDER BY j.name;
	`, d.Datastore.Schema)
	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []ScheduleSnapshotRow
	for rows.Next() {
		var data ScheduleSnapshotRow
		if err := rows.Scan(&data.Name, &data.SystemId, &data.TopicId, &data.TopicName,
			&data.Expression, &data.Suspended, &data.NextScheduledAt, &data.LastScheduledAt, &data.DueForSecs); err != nil {
			return nil, err
		}
		schedules = append(schedules, data)
	}
	return schedules, rows.Err()
}
