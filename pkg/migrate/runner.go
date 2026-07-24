package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entity types re-exported so callers depend only on pkg/migrate, not its
// datastore subpackage.
const (
	EntitySystem = mDatastore.EntitySystem
	EntityTopic  = mDatastore.EntityTopic
)

// ErrNotRegistered re-exports the datastore sentinel so callers can errors.Is
// against it without importing the datastore subpackage.
var ErrNotRegistered = mDatastore.ErrNotRegistered

type Runner struct {
	Datastore *mDatastore.MigrateDatastore
	Logger    logger.Logger
}

func NewRunner(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*Runner, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	migrateDatastore, err := mDatastore.NewMigrateDatastore(ds, retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &Runner{
		Datastore: migrateDatastore,
		Logger:    log,
	}, nil
}

// RunOnce migrates single entity of entityType and id entityId to targetVersion using registry.
func (r *Runner) RunOnce(ctx context.Context, targetVersion int64, entityType string, entityId int64, registry []Migration) error {
	if err := Validate(registry); err != nil {
		return err
	}
	// Version 1 is the baseline (Register); the registry supplies 2..max.
	maxVersion := int64(len(registry)) + 1
	if targetVersion < 1 || targetVersion > maxVersion {
		return fmt.Errorf("target version %d out of range [1, %d]", targetVersion, maxVersion)
	}

	conn, err := r.Datastore.AcquireLock(ctx)
	if err != nil {
		return err
	}
	defer r.Datastore.ReleaseLock(conn)

	return r.migrateEntity(ctx, conn, entityType, entityId, targetVersion, maxVersion, registry)
}

// RunAll migrates every entity of entityType to targetVersion using registry.
// CONTINUES past any entity that fails, joining every error. Topic only --
// system is a singleton, migrated through RunOnce.
func (r *Runner) RunAll(ctx context.Context, targetVersion int64, entityType string, registry []Migration) error {
	if err := Validate(registry); err != nil {
		return err
	}
	// Version 1 is the baseline (Register); the registry supplies 2..max.
	maxVersion := int64(len(registry)) + 1
	if targetVersion < 1 || targetVersion > maxVersion {
		return fmt.Errorf("target version %d out of range [1, %d]", targetVersion, maxVersion)
	}

	conn, err := r.Datastore.AcquireLock(ctx)
	if err != nil {
		return err
	}
	defer r.Datastore.ReleaseLock(conn)

	entities, err := r.entities(ctx, conn, entityType)
	if err != nil {
		return err
	}

	var errs []error
	for _, e := range entities {
		if err := r.migrateEntity(ctx, conn, entityType, e.Id, targetVersion, maxVersion, registry); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", entityType, e.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) entities(ctx context.Context, conn *pgxpool.Conn, entityType string) ([]mDatastore.Entity, error) {
	switch entityType {
	case mDatastore.EntitySystem:
		return nil, fmt.Errorf("system is a singleton entity -- use RunOnce, not RunAll")
	case mDatastore.EntityTopic:
		return r.Datastore.ListTopics(ctx, conn)
	default:
		return nil, fmt.Errorf("unknown entity type %q", entityType)
	}
}

// migrateEntity walks one entity between its current version and targetVersion.
func (r *Runner) migrateEntity(ctx context.Context, conn *pgxpool.Conn, entityType string, entityId, targetVersion, maxVersion int64, registry []Migration) error {
	current, err := mDatastore.Version(ctx, conn, entityType, entityId)
	if err != nil {
		return err
	}
	if current > maxVersion {
		return fmt.Errorf("%s schema is version %d but this build only defines up to %d -- upgrade the binary", entityType, current, maxVersion)
	}

	switch {
	case targetVersion > current:
		for v := current + 1; v <= targetVersion; v++ {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := r.stepUp(ctx, conn, entityType, entityId, registry[v-2]); err != nil {
				r.Datastore.TryRecordFailure(ctx, conn, entityType, entityId, v, err)
				return fmt.Errorf("up to version %d: %w", v, err)
			}
			r.Logger.InfoContext(ctx, "schema migrated up", "scope", entityType, "entity_id", entityId, "version", v)
		}
	case targetVersion < current:
		for v := current - 1; v >= targetVersion; v-- {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := r.stepDown(ctx, conn, entityType, entityId, registry[v-1]); err != nil {
				r.Datastore.TryRecordFailure(ctx, conn, entityType, entityId, v, err)
				return fmt.Errorf("down to version %d: %w", v, err)
			}
			r.Logger.InfoContext(ctx, "schema migrated down", "scope", entityType, "entity_id", entityId, "version", v)
		}
	}
	return nil
}
