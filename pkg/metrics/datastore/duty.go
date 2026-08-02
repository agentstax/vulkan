package datastore

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// overdueFactor: how many rates past its gate a duty counts as overdue.
const overdueFactor = 10

// DutySnapshot is one maintenance row's health.
type DutySnapshot struct {
	Duty          string
	TopicName     string
	ConsumerGroup string
	Rate          time.Duration
	GateAge       time.Duration // now() - can_run_after: negative while claimed into the future, positive once eligible and unclaimed
	Overdue       bool          // GateAge > overdueFactor * Rate -- nobody is maintaining this duty (or its owner is stuck)
	Attempts      int
}

// DutySnapshots is every duty's current health, queried live from Postgres --
// works cold, nothing needs to be running.
func (d *MetricsDatastore) DutySnapshots(ctx context.Context) ([]DutySnapshot, error) {
	var duties []DutySnapshot
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		duties, err = d.dutySnapshots(ctx)
		return err
	})
	return duties, err
}

func (d *MetricsDatastore) dutySnapshots(ctx context.Context) ([]DutySnapshot, error) {
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

	var duties []DutySnapshot
	for rows.Next() {
		var s DutySnapshot
		var rateNs pgtype.Int8
		var gateAgeSecs float64
		if err := rows.Scan(&s.Duty, &s.TopicName, &s.ConsumerGroup, &rateNs, &gateAgeSecs, &s.Attempts); err != nil {
			return nil, err
		}
		if !rateNs.Valid {
			// a row with no poll_rate can't have an Overdue verdict -- skip it
			// rather than fail the whole snapshot
			continue
		}
		s.Rate = time.Duration(rateNs.Int64)
		s.GateAge = time.Duration(gateAgeSecs * float64(time.Second))
		s.Overdue = s.GateAge > overdueFactor*s.Rate
		duties = append(duties, s)
	}
	return duties, rows.Err()
}
