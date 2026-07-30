package maintain

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The claimable duty kinds in the maintenance table.
const (
	DutyJanitor   = "janitor"
	DutyWaterline = "waterline"
	DutyAlert     = "alert"
)

// ErrDutyLost fences an overrunning owner: the claim expired mid-work and
// another maintainer's claim rotated the token. Stop working -- the duty
// isn't yours anymore.
var ErrDutyLost = errors.New("duty lost: claimed by another maintainer")

type MaintenanceDatastore struct {
	Datastore      *datastore.PostgresDatastore
	DatastoreRetry *retry.DatastoreRetry
	DutyRetry      *retry.Retry // used to calculate delay for backoff retry logic of duties
	Logger         logger.Logger
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMaintenanceDatastore(ds *datastore.PostgresDatastore, cfg *MaintenanceDatastoreConfig) (*MaintenanceDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &MaintenanceDatastoreConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	datastoreRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	dutyRetry, err := retry.NewRetry(cfg.DutyRetry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MaintenanceDatastore{
		Datastore:      ds,
		DatastoreRetry: datastoreRetry,
		DutyRetry:      dutyRetry,
		Logger:         cfg.Logger,
	}, nil
}

// GetGroupId resolves a consumer group's id by its owning topic and name.
// Returns (0, nil) if the group is not registered on that topic.
func (d *MaintenanceDatastore) GetGroupId(ctx context.Context, topicID int64, name string) (int64, error) {
	var id int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		err := d.Datastore.Pool.QueryRow(ctx, `SELECT id FROM consumer_group WHERE topic_id = $1 AND name = $2;`, topicID, name).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			id = 0
			return nil
		}
		return err
	})
	return id, err
}

// FleetDuty is one row of the fleet's discovery view. The whole struct is
// the fleet's reconcile key.
type FleetDuty struct {
	Duty          string
	TopicID       int64
	TopicName     string // duties register by topic name not id
	SchemaVersion topic.SchemaVersion
	ConsumerGroup string // duties register by group name not id ('' = topic-scoped)
	Rate          time.Duration
}

// ListDuties lists every duty seeded in the maintenance table. Read-only:
// claiming stays with each spawned duty's own runner.
func (d *MaintenanceDatastore) ListDuties(ctx context.Context) ([]FleetDuty, error) {
	var duties []FleetDuty
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		duties, err = d.listDuties(ctx)
		return err
	})
	return duties, err
}

func (d *MaintenanceDatastore) listDuties(ctx context.Context) ([]FleetDuty, error) {
	// each duty runs at its own topic's rate, so the rate switches on duty kind.
	// The owner decides the topic join: topic-owned rows carry topic_id, group-
	// owned rows (waterline) reach it through the group. Group joined for its
	// NAME -- rollers register by name, resolving the id themselves ('' for
	// topic-scoped duties)
	sql := `
		SELECT
			m.duty, COALESCE(g.name, ''), t.id, t.name, t.schema_version,
			CASE m.duty
				WHEN 'janitor' THEN t.janitor_poll_rate_ns
				WHEN 'waterline' THEN t.waterline_poll_rate_ns
				WHEN 'alert' THEN s.alert_poll_rate_ns
			END
		FROM maintenance m
		LEFT JOIN consumer_group g ON g.id = m.consumer_group_id
		JOIN topic t ON t.id = COALESCE(m.topic_id, g.topic_id)
		LEFT JOIN system s ON true -- singleton (id 0); LEFT so janitor/waterline
		                           -- discovery never depends on the system row
		WHERE m.duty IN ('janitor', 'waterline', 'alert');
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duties []FleetDuty
	for rows.Next() {
		var f FleetDuty
		var rateNs int64
		if err := rows.Scan(&f.Duty, &f.ConsumerGroup, &f.TopicID, &f.TopicName, &f.SchemaVersion, &rateNs); err != nil {
			return nil, err
		}
		f.Rate = time.Duration(rateNs)
		duties = append(duties, f)
	}
	return duties, rows.Err()
}

type DutyClaim struct {
	Id          int64
	Duty        string
	Owner       common.Owner
	Token       pgtype.UUID
	CanRunAfter time.Time
	Attempts    int
}

// ClaimDuty races the duty's gate -- the winner owns it until can_run_after,
// and renew/release fence on the returned Duty's token. nil = claim lost.
func (d *MaintenanceDatastore) ClaimDuty(ctx context.Context, duty string, owner common.Owner, rate time.Duration) (*DutyClaim, error) {
	var claimed *DutyClaim
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimDuty(ctx, duty, owner, rate)
		return err
	})
	return claimed, err
}

func (d *MaintenanceDatastore) claimDuty(ctx context.Context, duty string, owner common.Owner, rate time.Duration) (*DutyClaim, error) {
	// auto-commit: winner does duty work, losers skip.
	// now() is DB time on both sides -- N replicas' clocks never agree, the DB's does.
	sql := `
		UPDATE maintenance
		SET
			token = gen_random_uuid(),
			can_run_after = now() + make_interval(secs => $4),
			attempts = attempts + 1
		WHERE duty = $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
			AND can_run_after <= now()
		RETURNING id, duty, COALESCE(topic_id, 0), COALESCE(consumer_group_id, 0), token, can_run_after, attempts;
	`

	var claimed DutyClaim
	err := d.Datastore.Pool.QueryRow(ctx, sql, duty, owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), rate.Seconds()).
		Scan(&claimed.Id, &claimed.Duty, &claimed.Owner.TopicId, &claimed.Owner.ConsumerGroupId, &claimed.Token, &claimed.CanRunAfter, &claimed.Attempts)
	if err != nil {
		// no row: another maintainer won, or the duty was never seeded
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &claimed, nil
}

// BackoffDuty pushes a failed duty's retry gate out by its Nth-attempt delay
// from DutyRetry. Returns the delay it wrote, for the caller's own logging.
func (d *MaintenanceDatastore) BackoffDuty(ctx context.Context, duty *DutyClaim) (time.Duration, error) {
	delay := d.DutyRetry.CalculateDelay(duty.Attempts - 1)

	err := d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET can_run_after = now() + make_interval(secs => $3)
			WHERE id = $1
				AND token = $2;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty.Id, duty.Token, delay.Seconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDutyLost
		}
		return nil
	})
	return delay, err
}

// Reset's duty after successful attempt
func (d *MaintenanceDatastore) ResetDuty(ctx context.Context, duty *DutyClaim) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET attempts = 0
			WHERE id = $1
				AND token = $2;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty.Id, duty.Token)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDutyLost
		}
		return nil
	})
}

// RenewDuty extends a claim the caller already won.
func (d *MaintenanceDatastore) RenewDuty(ctx context.Context, duty *DutyClaim, rate time.Duration) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET can_run_after = now() + make_interval(secs => $3)
			WHERE id = $1
				AND token = $2;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty.Id, duty.Token, rate.Seconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDutyLost
		}
		return nil
	})
}

// ReleaseDuty reopens the duty immediately, so on a graceful shutdown
// mid-claim the next tick's winner resumes instead of waiting out the
// claimed interval.
// Never call after a FAILED duty run: the unreleased claim keeps
// can_run_after in the future, so the retry waits out the interval instead
// of every replica immediately re-claiming and re-failing in a loop.
func (d *MaintenanceDatastore) ReleaseDuty(ctx context.Context, duty *DutyClaim) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET can_run_after = now()
			WHERE id = $1
				AND token = $2;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty.Id, duty.Token)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDutyLost
		}
		return nil
	})
}
