package datastore

import "context"

// DutySnapshots is every maintenance row's gate state.
func (d *MetricsDatastore) DutySnapshots(ctx context.Context) ([]DutySnapshotData, error) {
	var duties []DutySnapshotData
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		duties, err = d.dutySnapshots(ctx)
		return err
	})
	return duties, err
}

func (d *MetricsDatastore) dutySnapshots(ctx context.Context) ([]DutySnapshotData, error) {
	// every row carries its own poll_rate in metadata, so no per-kind rate
	// source. The owner decides the topic join: topic-owned rows carry
	// topic_id, group-owned rows (waterline) reach it through the group,
	// system-owned rows (scheduler) have no topic at all -- name shows ''.
	sql := `
		SELECT
			m.duty, COALESCE(t.name, ''), COALESCE(g.name, ''),
			(m.metadata->>'poll_rate')::BIGINT,
			EXTRACT(EPOCH FROM (now() - m.can_run_after)),
			m.attempts
		FROM maintenance m
		LEFT JOIN consumer_group g ON g.id = m.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(m.topic_id, g.topic_id)
		ORDER BY t.name, m.duty, g.name;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duties []DutySnapshotData
	for rows.Next() {
		var data DutySnapshotData
		if err := rows.Scan(&data.Duty, &data.TopicName, &data.ConsumerGroup, &data.RateNs, &data.GateAgeSecs, &data.Attempts); err != nil {
			return nil, err
		}
		duties = append(duties, data)
	}
	return duties, rows.Err()
}
