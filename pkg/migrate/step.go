package migrate

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (c *Controller) stepUp(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, m Migration) error {
	step, err := m.ToStep(mDatastore.StepUp, m.Version)
	if err != nil {
		return err
	}
	return c.datastore.RunStep(ctx, conn, owner, step)
}

func (c *Controller) stepDown(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, m Migration) error {
	step, err := m.ToStep(mDatastore.StepDown, m.Version-1) // m.Version-1 == the version to roll back TO
	if err != nil {
		return err
	}
	return c.datastore.RunStep(ctx, conn, owner, step)
}
