package datastore

import (
	"context"
	"time"
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
	// each duty runs at its own topic's rate, so the rate switches on duty
	// kind. The WHERE mirrors the CASE: a kind this build doesn't know is
	// skipped whole, not listed with a NULL rate that breaks the scan
	sql := `
		SELECT
			m.duty, t.name, COALESCE(g.name, ''),
			CASE m.duty
				WHEN 'janitor' THEN t.janitor_poll_rate_ns
				WHEN 'waterline' THEN t.waterline_poll_rate_ns
			END,
			EXTRACT(EPOCH FROM (now() - m.can_run_after)),
			m.attempts
		FROM maintenance m
		JOIN topic t ON t.id = m.topic_id
		LEFT JOIN consumer_group g ON g.id = m.consumer_group_id
		WHERE m.duty IN ('janitor', 'waterline')
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
		var rateNs int64
		var gateAgeSecs float64
		if err := rows.Scan(&s.Duty, &s.TopicName, &s.ConsumerGroup, &rateNs, &gateAgeSecs, &s.Attempts); err != nil {
			return nil, err
		}
		s.Rate = time.Duration(rateNs)
		s.GateAge = time.Duration(gateAgeSecs * float64(time.Second))
		s.Overdue = s.GateAge > overdueFactor*s.Rate
		duties = append(duties, s)
	}
	return duties, rows.Err()
}
