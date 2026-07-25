package maintain

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The two claimable duties in the maintenance table.
const (
	DutyJanitor   = "janitor"
	DutyWaterline = "waterline"
)

// ErrDutyLost fences an overrunning owner: the claim expired mid-work and
// another maintainer's claim rotated the token. Stop working -- the duty
// isn't yours anymore.
var ErrDutyLost = errors.New("duty lost: claimed by another maintainer")

type MaintenanceDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
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

	dsRetry, err := retry.NewDatastoreRetry(cfg.Retry, cfg.Logger)
	if err != nil {
		return nil, err
	}

	return &MaintenanceDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    cfg.Logger,
	}, nil
}

// ClaimDuty races the duty's gate -- the winner owns it until can_run_after,
// and renew/release fence on the returned token. nil token = claim lost.
func (d *MaintenanceDatastore) ClaimDuty(ctx context.Context, duty string, topicID int64, consumerGroup string, rate time.Duration) (*pgtype.UUID, error) {
	var token *pgtype.UUID
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		token, err = d.claimDuty(ctx, duty, topicID, consumerGroup, rate)
		return err
	})
	return token, err
}

func (d *MaintenanceDatastore) claimDuty(ctx context.Context, duty string, topicID int64, consumerGroup string, rate time.Duration) (*pgtype.UUID, error) {
	// auto-commit: winner does duty work, losers skip.
	// now() is DB time on both sides -- N replicas' clocks never agree, the DB's does.
	sql := `
		UPDATE maintenance
		SET
			can_run_after = now() + make_interval(secs => $4),
			token = gen_random_uuid()
		WHERE duty = $1
			AND topic_id = $2
			AND consumer_group = $3
			AND can_run_after <= now()
		RETURNING token;
	`

	var token pgtype.UUID
	err := d.Datastore.Pool.QueryRow(ctx, sql, duty, topicID, consumerGroup, rate.Seconds()).Scan(&token)
	if err != nil {
		// no row: another maintainer won, or the duty was never seeded
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &token, nil
}

// RenewDuty extends a claim the caller already won -- called between sweep
// batches so a long backlog drain keeps ownership past the original interval.
func (d *MaintenanceDatastore) RenewDuty(ctx context.Context, duty string, topicID int64, consumerGroup string, token pgtype.UUID, rate time.Duration) error {
	return d.Retry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET can_run_after = now() + make_interval(secs => $5)
			WHERE duty = $1
				AND topic_id = $2
				AND consumer_group = $3
				AND token = $4;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty, topicID, consumerGroup, token, rate.Seconds())
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
func (d *MaintenanceDatastore) ReleaseDuty(ctx context.Context, duty string, topicID int64, consumerGroup string, token pgtype.UUID) error {
	return d.Retry.Wrap(ctx, func() error {
		sql := `
			UPDATE maintenance
			SET can_run_after = now()
			WHERE duty = $1
				AND topic_id = $2
				AND consumer_group = $3
				AND token = $4;
		`
		tag, err := d.Datastore.Pool.Exec(ctx, sql, duty, topicID, consumerGroup, token)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrDutyLost
		}
		return nil
	})
}
