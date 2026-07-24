package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StepType string

const (
	StepUp   StepType = "UP"
	StepDown StepType = "DOWN"
)

type Step struct {
	Version  int64
	Validate func(context.Context, datastore.Querier, int64) error
	Apply    func(context.Context, datastore.Querier, int64) error
	NoTxn    bool
}

// NewStep checks a step's shape.
// version is where the DB lands after step is complete.
func NewStep(
	version int64,
	validate func(context.Context, datastore.Querier, int64) error,
	apply func(context.Context, datastore.Querier, int64) error,
	noTxn bool,
) (*Step, error) {
	if version < 1 {
		return nil, fmt.Errorf("step version must be >= 1, got %d", version)
	}
	if apply == nil {
		return nil, fmt.Errorf("step to version %d has no apply func", version)
	}

	return &Step{
		Version:  version,
		Validate: validate,
		Apply:    apply,
		NoTxn:    noTxn,
	}, nil
}

// RunStep validates + applies one step against an entity, then records its
// version as a success. The whole unit retried on a transient blip, so steps
// must be idempotent.
func (d *MigrateDatastore) RunStep(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId int64, step *Step) error {
	if step.NoTxn {
		return d.Retry.Wrap(ctx, func() error {
			return d.runStepWithoutTx(ctx, conn, entityType, entityId, step)
		})
	}
	return d.Retry.Wrap(ctx, func() error {
		return d.runStepWithTx(ctx, conn, entityType, entityId, step)
	})
}

// txn step does all three atomically
func (d *MigrateDatastore) runStepWithTx(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId int64, step *Step) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if step.Validate != nil {
		if err := step.Validate(ctx, tx, entityId); err != nil {
			return err
		}
	}
	if err := step.Apply(ctx, tx, entityId); err != nil {
		return err
	}
	if err := d.RecordSuccess(ctx, tx, entityType, entityId, step.Version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// NoTxn step runs on the bare connection and records separately once its apply returns
func (d *MigrateDatastore) runStepWithoutTx(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId int64, step *Step) error {
	if step.Validate != nil {
		if err := step.Validate(ctx, conn, entityId); err != nil {
			return err
		}
	}
	if err := step.Apply(ctx, conn, entityId); err != nil {
		return err
	}
	return d.RecordSuccess(ctx, conn, entityType, entityId, step.Version)
}
