package datastore

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Version is an owner's latest-by-id success row, read on the pool.
func (d *MigrateDatastore) Version(ctx context.Context, owner *common.Owner) (int64, error) {
	var version int64
	err := d.DatastoreRetry.Wrap(ctx, func() error {
		var err error
		version, err = d.version(ctx, owner)
		return err
	})
	return version, err
}

func (d *MigrateDatastore) version(ctx context.Context, owner *common.Owner) (int64, error) {
	return Version(ctx, d.Datastore.Pool, owner)
}

// Version is an owner's latest-by-id success row -- latest-by-id, NOT MAX, so
// a downgrade (which records a LOWER version) reads back correctly. The free
// form exists for reads on the lock-holding connection.
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

	var v int64
	if err := q.QueryRow(ctx, sql, owner.SystemIdColumn(), owner.TopicIdColumn(), owner.ConsumerGroupIdColumn()).Scan(&v); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotRegistered
		}
		// 42P01 = table does not exist
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
			return 0, ErrNotRegistered
		}
		return 0, err
	}
	return v, nil
}
