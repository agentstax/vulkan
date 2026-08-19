package controller

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/migrate/controller/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (c *Controller) stepUp(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, migration migrate.Migration) error {
	step, err := toStep(&migration, datastore.StepUp, migration.Version)
	if err != nil {
		return err
	}
	return c.datastore.RunStep(ctx, conn, owner, step)
}

func (c *Controller) stepDown(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, migration migrate.Migration) error {
	step, err := toStep(&migration, datastore.StepDown, migration.Version-1) // migration.Version-1 == the version to roll back TO
	if err != nil {
		return err
	}
	return c.datastore.RunStep(ctx, conn, owner, step)
}

// ***************
// *** HELPERS ***
// ***************

func toStep(migration *migrate.Migration, stepType datastore.StepType, targetVersion int64) (*datastore.Step, error) {
	switch stepType {
	case datastore.StepUp:
		if migration.Up == nil {
			return nil, fmt.Errorf("version %d has no Up defined", migration.Version)
		}
		return datastore.NewStep(targetVersion, migration.ValidateUp, migration.Up, migration.NoTxn)
	case datastore.StepDown:
		if migration.Down == nil {
			return nil, fmt.Errorf("version %d has no Down defined -- migration is irreversible", migration.Version)
		}
		return datastore.NewStep(targetVersion, migration.ValidateDown, migration.Down, migration.NoTxn)
	default:
		return nil, fmt.Errorf("invalid stepType %s defined", stepType)
	}
}
