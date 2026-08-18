package datastore

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
)

// migration_log stores exactly one owner column per row, so each read matches
// its own column and pins the other two to NULL.

// SystemVersion is the system's latest-by-id success row, read on the pool.
func (d *MigrateDatastore) SystemVersion(ctx context.Context, systemId int64) (int64, error) {
	var version int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		version, err = d.systemVersion(ctx, systemId)
		return err
	})
	return version, err
}

func (d *MigrateDatastore) systemVersion(ctx context.Context, systemId int64) (int64, error) {
	sql := `
		SELECT migration_version FROM migration_log
		WHERE system_id = $1
			AND topic_id IS NULL
			AND consumer_group_id IS NULL
			AND status = 'success'
		ORDER BY id DESC
		LIMIT 1;
	`

	var version int64
	if err := d.Datastore.Pool.QueryRow(ctx, sql, systemId).Scan(&version); err != nil {
		return 0, registrationError(err)
	}
	return version, nil
}

// TopicVersion is a topic's latest-by-id success row, read on the pool.
func (d *MigrateDatastore) TopicVersion(ctx context.Context, topicId int64) (int64, error) {
	var version int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		version, err = d.topicVersion(ctx, topicId)
		return err
	})
	return version, err
}

func (d *MigrateDatastore) topicVersion(ctx context.Context, topicId int64) (int64, error) {
	sql := `
		SELECT migration_version FROM migration_log
		WHERE system_id IS NULL
			AND topic_id = $1
			AND consumer_group_id IS NULL
			AND status = 'success'
		ORDER BY id DESC
		LIMIT 1;
	`

	var version int64
	if err := d.Datastore.Pool.QueryRow(ctx, sql, topicId).Scan(&version); err != nil {
		return 0, registrationError(err)
	}
	return version, nil
}

// Version is an owner's latest-by-id success row -- latest-by-id, NOT MAX, so
// a downgrade (which records a LOWER version) reads back correctly. The
// migration run is owner-generic (one steps loop, one record insert for every
// scope), so its read on the lock-holding connection is too.
//
// There is no implied baseline but every owner is recorded at creation.
func Version(ctx context.Context, q datastore.Querier, owner *common.Owner) (int64, error) {
	// IS NOT DISTINCT FROM: NULL-safe equality against the owner's columns
	sql := `
		SELECT migration_version FROM migration_log
		WHERE system_id IS NOT DISTINCT FROM $1
			AND topic_id IS NOT DISTINCT FROM $2
			AND consumer_group_id IS NOT DISTINCT FROM $3
			AND status = 'success'
		ORDER BY id DESC
		LIMIT 1;
	`

	var version int64
	if err := q.QueryRow(ctx, sql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).Scan(&version); err != nil {
		return 0, registrationError(err)
	}
	return version, nil
}
