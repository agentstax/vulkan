package datastore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// overdueThreshold: how long a job may sit due-but-unfired before it counts
// as overdue.
const overdueThreshold = 10 * time.Minute

// CronJobSnapshots is every cron_job row's current firing health.
func (d *MetricsDatastore) CronJobSnapshots(ctx context.Context) ([]CronJobSnapshot, error) {
	var jobs []CronJobSnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		jobs, err = d.cronJobSnapshots(ctx)
		return err
	})
	return jobs, err
}

func (d *MetricsDatastore) cronJobSnapshots(ctx context.Context) ([]CronJobSnapshot, error) {
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

	var jobs []CronJobSnapshot
	for rows.Next() {
		var s CronJobSnapshot
		var systemId, topicId, consumerGroupId int64
		var topicName, groupName string
		var lastScheduled pgtype.Timestamptz
		var dueForSecs float64

		if err := rows.Scan(&s.Name, &systemId, &topicId, &consumerGroupId, &topicName, &groupName,
			&s.Schedule, &s.Suspended, &s.NextScheduledTime, &lastScheduled, &dueForSecs); err != nil {
			return nil, err
		}

		s.Owner, err = toOwner(systemId, topicId, consumerGroupId, topicName, groupName)
		if err != nil {
			return nil, err
		}

		if lastScheduled.Valid {
			s.LastScheduledTime = lastScheduled.Time
		}

		s.DueFor = time.Duration(dueForSecs * float64(time.Second))

		// a suspended row's next_scheduled_time goes stale on purpose --
		// unsuspending recomputes it, so staleness is never overdue
		s.Overdue = !s.Suspended && s.DueFor > overdueThreshold

		jobs = append(jobs, s)
	}
	return jobs, rows.Err()
}
