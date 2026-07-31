package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	mDatastore "github.com/agentstax/vulkan/pkg/migrate/datastore"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5/pgxpool"
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

// RunOnce migrates a single owner's schema to targetVersion using registry.
func (r *Runner) RunOnce(ctx context.Context, targetVersion int64, owner *common.Owner, registry []Migration) error {
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

	return r.migrateOwner(ctx, conn, owner, targetVersion, maxVersion, registry)
}

// RunAll migrates every owner of kind to targetVersion using registry.
// CONTINUES past any owner that fails, joining every error. Topic only --
// system is a singleton, migrated through RunOnce.
func (r *Runner) RunAll(ctx context.Context, targetVersion int64, kind common.OwnerKind, registry []Migration) error {
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

	owners, err := r.owners(ctx, conn, kind)
	if err != nil {
		return err
	}

	var errs []error
	for _, owner := range owners {
		if err := r.migrateOwner(ctx, conn, owner, targetVersion, maxVersion, registry); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", owner.Kind(), owner.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) owners(ctx context.Context, conn *pgxpool.Conn, kind common.OwnerKind) ([]*common.Owner, error) {
	switch kind {
	case common.OwnerSystem:
		return nil, fmt.Errorf("system is a singleton -- use RunOnce, not RunAll")
	case common.OwnerTopic:
		return r.Datastore.ListTopics(ctx, conn)
	default:
		return nil, fmt.Errorf("unknown owner kind %q", kind)
	}
}

// migrateOwner walks one owner between its current version and targetVersion.
func (r *Runner) migrateOwner(ctx context.Context, conn *pgxpool.Conn, owner *common.Owner, targetVersion, maxVersion int64, registry []Migration) error {
	current, err := mDatastore.Version(ctx, conn, owner)
	if err != nil {
		return err
	}
	if current > maxVersion {
		return fmt.Errorf("%s schema is version %d but this build only defines up to %d -- upgrade the binary", owner.Kind(), current, maxVersion)
	}

	switch {
	case targetVersion > current:
		for v := current + 1; v <= targetVersion; v++ {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := r.stepUp(ctx, conn, owner, registry[v-2]); err != nil {
				r.Datastore.TryRecordFailure(ctx, conn, owner, v, err)
				return fmt.Errorf("up to version %d: %w", v, err)
			}
			r.Logger.InfoContext(ctx, "schema migrated up", "owner", owner.Kind(), "topic_id", owner.TopicId, "version", v)
		}
	case targetVersion < current:
		for v := current - 1; v >= targetVersion; v-- {
			// correct migration is offset in slice index. registry[0] = version 2
			if err := r.stepDown(ctx, conn, owner, registry[v-1]); err != nil {
				r.Datastore.TryRecordFailure(ctx, conn, owner, v, err)
				return fmt.Errorf("down to version %d: %w", v, err)
			}
			r.Logger.InfoContext(ctx, "schema migrated down", "owner", owner.Kind(), "topic_id", owner.TopicId, "version", v)
		}
	}
	return nil
}
