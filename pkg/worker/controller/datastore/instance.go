package datastore

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ClaimInstance inserts a worker_instance row iff live instances are under
// target_instances. nil = declined (already at target, target 0, or the
// worker row is gone).
func (d *WorkerDatastore) ClaimInstance(ctx context.Context, workerId int64, ttl time.Duration) (*WorkerInstanceRow, error) {
	var claimed *WorkerInstanceRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		claimed, err = d.claimInstance(ctx, workerId, ttl)
		return err
	})
	return claimed, err
}

func (d *WorkerDatastore) claimInstance(ctx context.Context, workerId int64, ttl time.Duration) (*WorkerInstanceRow, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// the worker row lock serializes claimants: without it two concurrent
	// counts both see room under target and both insert
	var target int
	targetSql := fmt.Sprintf(`
		-- vulkan: worker.claimInstance
		SELECT target_instances FROM %[1]s.worker_config WHERE id = $1 FOR UPDATE;
	`, d.Datastore.Schema)
	err = tx.QueryRow(ctx, targetSql, workerId).Scan(&target)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	insertSql := fmt.Sprintf(`
		-- vulkan: worker.claimInstance
		INSERT INTO %[1]s.worker_instance (worker_id, expires_at)
		SELECT $1, now() + make_interval(secs => $2)
		WHERE $3 = -1 -- '-1' means unbound (can always claim)
			OR (SELECT count(*) FROM %[1]s.worker_instance WHERE worker_id = $1 AND expires_at > now()) < $3
		RETURNING id, worker_id, token, attempts;
	`, d.Datastore.Schema)
	var claimed WorkerInstanceRow
	err = tx.QueryRow(ctx, insertSql, workerId, ttl.Seconds(), target).
		Scan(&claimed.Id, &claimed.WorkerId, &claimed.Token, &claimed.Attempts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &claimed, nil
}

// RenewInstance extends an instance the caller already holds.
func (d *WorkerDatastore) RenewInstance(ctx context.Context, instanceId int64, token uuid.UUID, ttl time.Duration) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.renewInstance(ctx, instanceId, token, ttl)
	})
}

func (d *WorkerDatastore) renewInstance(ctx context.Context, instanceId int64, token uuid.UUID, ttl time.Duration) error {
	// an expired row may already be replaced -- renewing it past expiry
	// would put live instances over target_instances
	sql := fmt.Sprintf(`
		-- vulkan: worker.renewInstance
		UPDATE %[1]s.worker_instance
		SET expires_at = now() + make_interval(secs => $3)
		WHERE id = $1
			AND token = $2
			AND expires_at > now();
	`, d.Datastore.Schema)
	tag, err := d.Datastore.Pool.Exec(ctx, sql, instanceId, toTokenData(token), ttl.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return worker.ErrInstanceLost
	}
	return nil
}

// RecordInstanceSuccess resets the instance's consecutive-failure count.
func (d *WorkerDatastore) RecordInstanceSuccess(ctx context.Context, instanceId int64, token uuid.UUID) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.recordInstanceSuccess(ctx, instanceId, token)
	})
}

func (d *WorkerDatastore) recordInstanceSuccess(ctx context.Context, instanceId int64, token uuid.UUID) error {
	sql := fmt.Sprintf(`
		-- vulkan: worker.recordInstanceSuccess
		UPDATE %[1]s.worker_instance
		SET attempts = 0
		WHERE id = $1
			AND token = $2;
	`, d.Datastore.Schema)
	tag, err := d.Datastore.Pool.Exec(ctx, sql, instanceId, toTokenData(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return worker.ErrInstanceLost
	}
	return nil
}

// RecordInstanceFailure adds one to the instance's consecutive-failure count,
// returning the new count.
func (d *WorkerDatastore) RecordInstanceFailure(ctx context.Context, instanceId int64, token uuid.UUID) (int, error) {
	var attempts int
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		attempts, err = d.recordInstanceFailure(ctx, instanceId, token)
		return err
	})
	return attempts, err
}

func (d *WorkerDatastore) recordInstanceFailure(ctx context.Context, instanceId int64, token uuid.UUID) (int, error) {
	sql := fmt.Sprintf(`
		-- vulkan: worker.recordInstanceFailure
		UPDATE %[1]s.worker_instance
		SET attempts = attempts + 1
		WHERE id = $1
			AND token = $2
		RETURNING attempts;
	`, d.Datastore.Schema)
	var attempts int
	err := d.Datastore.Pool.QueryRow(ctx, sql, instanceId, toTokenData(token)).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, worker.ErrInstanceLost
	}
	if err != nil {
		return 0, err
	}
	return attempts, nil
}

// ReleaseInstance removes the instance row immediately, so on a graceful
// shutdown a replacement claims right away instead of waiting out expires_at.
func (d *WorkerDatastore) ReleaseInstance(ctx context.Context, instanceId int64, token uuid.UUID) error {
	return d.DatastoreRetry.Wrap(ctx, func() error {
		return d.releaseInstance(ctx, instanceId, token)
	})
}

func (d *WorkerDatastore) releaseInstance(ctx context.Context, instanceId int64, token uuid.UUID) error {
	sql := fmt.Sprintf(`
		-- vulkan: worker.releaseInstance
		DELETE FROM %[1]s.worker_instance
		WHERE id = $1
			AND token = $2;
	`, d.Datastore.Schema)
	tag, err := d.Datastore.Pool.Exec(ctx, sql, instanceId, toTokenData(token))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return worker.ErrInstanceLost
	}
	return nil
}

// SweepExpiredInstances removes rows past expires_at, returning the count
// removed.
func (d *WorkerDatastore) SweepExpiredInstances(ctx context.Context) (int64, error) {
	var removed int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		removed, err = d.sweepExpiredInstances(ctx)
		return err
	})
	return removed, err
}

func (d *WorkerDatastore) sweepExpiredInstances(ctx context.Context) (int64, error) {
	sql := fmt.Sprintf(`
		-- vulkan: worker.sweepExpiredInstances
		DELETE FROM %[1]s.worker_instance WHERE expires_at <= now();
	`, d.Datastore.Schema)
	tag, err := d.Datastore.Pool.Exec(ctx, sql)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ***************
// *** HELPERS ***
// ***************

func toTokenData(token uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: token, Valid: true}
}
