package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/migrate/controller/datastore"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Controller struct {
	Logger logging.Logger

	datastore *datastore.MigrateDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewController(ds *iDatastore.PostgresDatastore, cfg *ControllerConfig) (*Controller, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if cfg == nil {
		cfg = &ControllerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	migrateDatastore, err := datastore.NewMigrateDatastore(ds, &datastore.MigrateDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &Controller{
		Logger:    cfg.Logger,
		datastore: migrateDatastore,
	}, nil
}

// RunOnce migrates a single owner's schema to targetVersion using registry.
func (c *Controller) RunOnce(ctx context.Context, targetVersion int64, owner *common.Owner, registry []migrate.Migration) error {
	if err := migrate.Validate(registry); err != nil {
		return err
	}

	// Version 1 is the baseline (Register); the registry supplies 2..max.
	maxVersion := int64(len(registry)) + 1
	if targetVersion < 1 || targetVersion > maxVersion {
		return fmt.Errorf("target version %d out of range [1, %d]", targetVersion, maxVersion)
	}

	conn, err := c.datastore.AcquireLock(ctx)
	if err != nil {
		return err
	}
	defer c.datastore.ReleaseLock(ctx, conn)

	return c.migrateOwner(ctx, conn, owner, targetVersion, maxVersion, registry)
}

// RunAll migrates every owner of kind to targetVersion using registry.
// CONTINUES past any owner that fails, joining every error. Topic only --
// system is a singleton, migrated through RunOnce.
func (c *Controller) RunAll(ctx context.Context, targetVersion int64, kind common.OwnerKind, registry []migrate.Migration) error {
	if err := migrate.Validate(registry); err != nil {
		return err
	}

	// Version 1 is the baseline (Register); the registry supplies 2..max.
	maxVersion := int64(len(registry)) + 1
	if targetVersion < 1 || targetVersion > maxVersion {
		return fmt.Errorf("target version %d out of range [1, %d]", targetVersion, maxVersion)
	}

	conn, err := c.datastore.AcquireLock(ctx)
	if err != nil {
		return err
	}
	defer c.datastore.ReleaseLock(ctx, conn)

	owners, err := c.owners(ctx, conn, kind)
	if err != nil {
		return err
	}

	var errs []error
	for _, owner := range owners {
		if err := c.migrateOwner(ctx, conn, owner, targetVersion, maxVersion, registry); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", owner.Kind(), owner.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) owners(ctx context.Context, conn *pgxpool.Conn, kind common.OwnerKind) ([]*common.Owner, error) {
	switch kind {
	case common.OwnerSystem:
		return nil, errors.New("system is a singleton -- use RunOnce, not RunAll")
	case common.OwnerTopic:
		return c.datastore.ListTopics(ctx, conn)
	default:
		return nil, fmt.Errorf("owner kind must be %q, got %q", common.OwnerTopic, kind)
	}
}

// migrateOwner walks one owner between its current version and targetVersion.
func (c *Controller) migrateOwner(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, targetVersion, maxVersion int64, registry []migrate.Migration) error {
	current, err := datastore.Version(ctx, conn, owner)
	if err != nil {
		return err
	}
	if current > maxVersion {
		return migrate.ErrSchemaNewerThanBuild.With("kind", owner.Kind(), "version", current, "build_version", maxVersion)
	}

	switch {
	case targetVersion > current:
		for v := current + 1; v <= targetVersion; v++ {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := c.stepUp(ctx, conn, owner, registry[v-2]); err != nil {
				c.datastore.TryRecordFailure(ctx, conn, owner, v, err)
				return fmt.Errorf("up to version %d: %w", v, err)
			}
			c.Logger.InfoContext(ctx, "schema migrated up", "owner_kind", owner.Kind(), "topic_id", owner.TopicId, "version", v)
		}
	case targetVersion < current:
		for v := current - 1; v >= targetVersion; v-- {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := c.stepDown(ctx, conn, owner, registry[v-1]); err != nil {
				c.datastore.TryRecordFailure(ctx, conn, owner, v, err)
				return fmt.Errorf("down to version %d: %w", v, err)
			}
			c.Logger.InfoContext(ctx, "schema migrated down", "owner_kind", owner.Kind(), "topic_id", owner.TopicId, "version", v)
		}
	}
	return nil
}
