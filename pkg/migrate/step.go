package migrate

import (
	"context"

	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (r *Runner) stepUp(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId int64, m Migration) error {
	step, err := m.ToStep(mDatastore.StepUp, m.Version)
	if err != nil {
		return err
	}
	return r.Datastore.RunStep(ctx, conn, entityType, entityId, step)
}

func (r *Runner) stepDown(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId int64, m Migration) error {
	step, err := m.ToStep(mDatastore.StepDown, m.Version-1) // m.Version-1 == the version to roll back TO
	if err != nil {
		return err
	}
	return r.Datastore.RunStep(ctx, conn, entityType, entityId, step)
}
