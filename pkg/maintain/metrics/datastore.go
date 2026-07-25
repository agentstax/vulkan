package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

type maintenanceMetricsDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMaintenanceDatastore(ds *datastore.PostgresDatastore, cfg *MaintenanceMetricsDatastoreConfig) (*maintenanceMetricsDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MaintenanceMetricsDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &maintenanceMetricsDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    cfg.Logger,
	}, nil
}

func (d *maintenanceMetricsDatastore) dutyStatusSnapshot(ctx context.Context) ([]DutyStatus, error) {
	// each duty runs at its own topic's rate, so the rate switches on duty
	// kind. The WHERE mirrors the CASE: a kind this build doesn't know is
	// skipped whole, not listed with a NULL rate that breaks the scan
	sql := `
		SELECT
			m.duty, t.name, m.consumer_group,
			CASE m.duty
				WHEN 'janitor' THEN t.janitor_poll_rate_ns
				WHEN 'waterline' THEN t.waterline_poll_rate_ns
			END,
			EXTRACT(EPOCH FROM (now() - m.can_run_after))
		FROM maintenance m
		JOIN topic t ON t.id = m.topic_id
		WHERE m.duty IN ('janitor', 'waterline')
		ORDER BY t.name, m.duty, m.consumer_group;
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duties []DutyStatus
	for rows.Next() {
		var s DutyStatus
		var rateNs int64
		var gateAgeSecs float64
		if err := rows.Scan(&s.Duty, &s.TopicName, &s.ConsumerGroup, &rateNs, &gateAgeSecs); err != nil {
			return nil, err
		}
		s.Rate = time.Duration(rateNs)
		s.GateAge = time.Duration(gateAgeSecs * float64(time.Second))
		s.Overdue = s.GateAge > overdueFactor*s.Rate
		duties = append(duties, s)
	}
	return duties, rows.Err()
}
