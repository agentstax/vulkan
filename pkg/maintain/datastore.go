package maintain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The claimable duty kinds in the maintenance table.
const (
	DutyJanitor   = "janitor"
	DutyWaterline = "waterline"
	DutyScheduler = "scheduler"
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

// DutyMetadata is the per-duty tuning the maintenance row itself carries --
// one shape for every duty kind, so no tuning lives on the owner's own
// table. A duty reads only the fields it runs on; its seed writes only those.
type DutyMetadata struct {
	PollRate       time.Duration `json:"poll_rate"`
	SweepBatchSize int           `json:"sweep_batch_size"` // janitor only: rows deleted per sweep transaction
}

func NewDutyMetadata(pollRate time.Duration, sweepBatchSize int) (*DutyMetadata, error) {
	if pollRate <= 0 {
		return nil, fmt.Errorf("pollRate must be > 0, got %v", pollRate)
	}
	if sweepBatchSize < 0 {
		return nil, fmt.Errorf("sweepBatchSize must be >= 0, got %d", sweepBatchSize)
	}
	return &DutyMetadata{PollRate: pollRate, SweepBatchSize: sweepBatchSize}, nil
}

// GetDutyMetadata reads the (duty, owner) row's metadata. Errors if the row
// was never seeded -- the owner's register creates it.
func (d *MaintenanceDatastore) GetDutyMetadata(ctx context.Context, duty string, owner *common.Owner) (*DutyMetadata, error) {
	var meta DutyMetadata
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		sql := `
			SELECT metadata
			FROM maintenance
			WHERE duty = $1
				AND system_id IS NOT DISTINCT FROM $2
				AND topic_id IS NOT DISTINCT FROM $3
				AND consumer_group_id IS NOT DISTINCT FROM $4;
		`
		var raw []byte
		err := d.Datastore.Pool.QueryRow(ctx, sql, duty, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).Scan(&raw)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("duty %q has no maintenance row -- the owner's register seeds it", duty)
			}
			return err
		}
		return json.Unmarshal(raw, &meta)
	})
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

// FleetDuty is one row of the fleet's discovery view. The whole struct is
// the fleet's reconcile key.
type FleetDuty struct {
	Duty            string
	SystemID        int64
	TopicID         int64  // 0 for system-owned duties
	ConsumerGroupID int64  // 0 unless group-owned
	TopicName       string // '' for system-owned; owner diagnostics + logs
	ConsumerGroup   string // '' unless group-owned
	Metadata        DutyMetadata
}

// owner rebuilds the row's owner, the identity a duty's Register runs under.
func (f FleetDuty) owner() (*common.Owner, error) {
	switch {
	case f.ConsumerGroupID > 0:
		return common.NewConsumerGroupOwner(f.SystemID, f.TopicID, f.ConsumerGroupID, f.ConsumerGroup)
	case f.TopicID > 0:
		return common.NewTopicOwner(f.SystemID, f.TopicID, f.TopicName)
	default:
		return common.NewSystemOwner(f.SystemID)
	}
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
	// every row carries its own poll_rate in metadata, so no per-kind rate
	// source. The owner decides the joins: topic-owned rows carry topic_id,
	// group-owned rows (waterline) reach the topic through the group,
	// system-owned rows (scheduler) have no topic at all -- topic columns
	// COALESCE to the zero values. system_id falls back through the topic so
	// owner() can rebuild any kind's owner from the one row.
	sql := `
		SELECT m.duty, COALESCE(m.system_id, t.system_id, 0), COALESCE(t.id, 0), COALESCE(m.consumer_group_id, 0), COALESCE(t.name, ''), COALESCE(g.name, ''), m.metadata
		FROM maintenance m
		LEFT JOIN consumer_group g ON g.id = m.consumer_group_id
		LEFT JOIN topic t ON t.id = COALESCE(m.topic_id, g.topic_id);
	`

	rows, err := d.Datastore.Pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var duties []FleetDuty
	for rows.Next() {
		var f FleetDuty
		var raw []byte
		if err := rows.Scan(&f.Duty, &f.SystemID, &f.TopicID, &f.ConsumerGroupID, &f.TopicName, &f.ConsumerGroup, &raw); err != nil {
			return nil, err
		}
		// a bad row skips, it doesn't error the whole list and stall every
		// duty fleet-wide
		if err := json.Unmarshal(raw, &f.Metadata); err != nil {
			d.Logger.WarnContext(ctx, "duty row metadata unreadable -- skipping", "duty", f.Duty, "error", err)
			continue
		}
		if f.Metadata.PollRate <= 0 {
			d.Logger.WarnContext(ctx, "duty row has no poll rate -- skipping", "duty", f.Duty)
			continue
		}
		duties = append(duties, f)
	}
	return duties, rows.Err()
}

type DutyClaim struct {
	Id          int64
	Duty        string
	Owner       *common.Owner
	Token       pgtype.UUID
	CanRunAfter time.Time
	Attempts    int
}

// ClaimDuty races the duty's gate -- the winner owns it until can_run_after,
// and renew/release fence on the returned Duty's token. nil = claim lost.
func (d *MaintenanceDatastore) ClaimDuty(ctx context.Context, duty string, owner *common.Owner, rate time.Duration) (*DutyClaim, error) {
	var claimed *DutyClaim
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimDuty(ctx, duty, owner, rate)
		return err
	})
	return claimed, err
}

func (d *MaintenanceDatastore) claimDuty(ctx context.Context, duty string, owner *common.Owner, rate time.Duration) (*DutyClaim, error) {
	// auto-commit: winner does duty work, losers skip.
	// now() is DB time on both sides -- N replicas' clocks never agree, the DB's does.
	sql := `
		UPDATE maintenance
		SET
			token = gen_random_uuid(),
			can_run_after = now() + make_interval(secs => $5),
			attempts = attempts + 1
		WHERE duty = $1
			AND system_id IS NOT DISTINCT FROM $2
			AND topic_id IS NOT DISTINCT FROM $3
			AND consumer_group_id IS NOT DISTINCT FROM $4
			AND can_run_after <= now()
		RETURNING id, duty, COALESCE(system_id, 0), COALESCE(topic_id, 0), COALESCE(consumer_group_id, 0), token, can_run_after, attempts;
	`

	claimed := DutyClaim{Owner: &common.Owner{}}
	err := d.Datastore.Pool.QueryRow(ctx, sql, duty, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn(), rate.Seconds()).
		Scan(&claimed.Id, &claimed.Duty, &claimed.Owner.SystemId, &claimed.Owner.TopicId, &claimed.Owner.ConsumerGroupId, &claimed.Token, &claimed.CanRunAfter, &claimed.Attempts)
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
