package datastore

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

// migration_log stores exactly one owner column per row, so each read matches
// its own column and pins the other two to NULL.

// SystemSchemaState is the system's version facts, read on the pool: current
// from the latest-by-id success row, minimum compatible from the strictest
// step at or below it.
func (d *MigrateDatastore) SystemSchemaState(ctx context.Context, systemId int64) (*SchemaStateRow, error) {
	var state *SchemaStateRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		state, err = d.systemSchemaState(ctx, systemId)
		return err
	})
	return state, err
}

func (d *MigrateDatastore) systemSchemaState(ctx context.Context, systemId int64) (*SchemaStateRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: migrate.systemSchemaState
		WITH successes AS (
			SELECT id, migration_version, min_compatible_version
			FROM %[1]s.migration_log
			WHERE system_id = $1
				AND topic_id IS NULL
				AND consumer_group_id IS NULL
				AND status = 'success'
		),
		current AS (
			-- latest-by-id, not MAX -- a downgrade records a lower version
			SELECT migration_version FROM successes ORDER BY id DESC LIMIT 1
		),
		compatibility AS (
			-- strictest declaration among steps at or below current -- a step
			-- rolled back below current no longer binds. The current row itself
			-- qualifies, so MAX never aggregates an empty set
			SELECT MAX(successes.min_compatible_version) AS min_compatible_version
			FROM successes, current
			WHERE successes.migration_version <= current.migration_version
		)
		SELECT current.migration_version, compatibility.min_compatible_version
		FROM current, compatibility;
	`, d.Datastore.Schema)

	var state SchemaStateRow
	if err := d.Datastore.Pool.QueryRow(ctx, sql, systemId).Scan(&state.Version, &state.MinCompatibleVersion); err != nil {
		return nil, registrationError(err)
	}
	return &state, nil
}

// TopicSchemaState is a topic's version facts, read on the pool: current from
// the latest-by-id success row, minimum compatible from the strictest step at
// or below it.
func (d *MigrateDatastore) TopicSchemaState(ctx context.Context, topicId int64) (*SchemaStateRow, error) {
	var state *SchemaStateRow
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		state, err = d.topicSchemaState(ctx, topicId)
		return err
	})
	return state, err
}

func (d *MigrateDatastore) topicSchemaState(ctx context.Context, topicId int64) (*SchemaStateRow, error) {
	sql := fmt.Sprintf(`
		-- vulkan: migrate.topicSchemaState
		WITH successes AS (
			SELECT id, migration_version, min_compatible_version
			FROM %[1]s.migration_log
			WHERE system_id IS NULL
				AND topic_id = $1
				AND consumer_group_id IS NULL
				AND status = 'success'
		),
		current AS (
			-- latest-by-id, not MAX -- a downgrade records a lower version
			SELECT migration_version FROM successes ORDER BY id DESC LIMIT 1
		),
		compatibility AS (
			-- strictest declaration among steps at or below current -- a step
			-- rolled back below current no longer binds. The current row itself
			-- qualifies, so MAX never aggregates an empty set
			SELECT MAX(successes.min_compatible_version) AS min_compatible_version
			FROM successes, current
			WHERE successes.migration_version <= current.migration_version
		)
		SELECT current.migration_version, compatibility.min_compatible_version
		FROM current, compatibility;
	`, d.Datastore.Schema)

	var state SchemaStateRow
	if err := d.Datastore.Pool.QueryRow(ctx, sql, topicId).Scan(&state.Version, &state.MinCompatibleVersion); err != nil {
		return nil, registrationError(err)
	}
	return &state, nil
}

// Version is an owner's latest-by-id success row -- latest-by-id, NOT MAX, so
// a downgrade (which records a LOWER version) reads back correctly. The
// migration run is owner-generic (one steps loop, one record insert for every
// scope), so its read on the lock-holding connection is too.
//
// There is no implied baseline but every owner is recorded at creation.
func Version(ctx context.Context, q datastore.Querier, owner *common.Owner, schema string) (int64, error) {
	// IS NOT DISTINCT FROM: NULL-safe equality against the owner's columns
	sql := fmt.Sprintf(`
		-- vulkan: migrate.Version
		SELECT migration_version FROM %[1]s.migration_log
		WHERE system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
			AND status = 'success'
		ORDER BY id DESC
		LIMIT 1;
	`, schema)

	var version int64
	if err := q.QueryRow(ctx, sql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).Scan(&version); err != nil {
		return 0, registrationError(err)
	}
	return version, nil
}
