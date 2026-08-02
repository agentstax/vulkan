package alert

import (
	"context"
	"errors"
	"os"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// locksPerPartition is how many lockable relations one partition owns -- table,
// pkey index, compaction_key index, TOAST table, TOAST index. Dropping the
// parent locks all of them at once, so the lock-table stock divided by this is
// the partition ceiling.
const locksPerPartition = 5

// AlertDatastore serves each check its own structural query. Read-only and cold:
// everything is derived from rows and settings that already exist, so nothing
// needs to be running.
type AlertDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func NewAlertDatastore(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*AlertDatastore, error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &AlertDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}

// topicRow is the slice of the topic row the checks fan out over.
type topicRow struct {
	id   int64
	name string
}

// topics lists every registered topic.
func (d *AlertDatastore) topics(ctx context.Context) ([]topicRow, error) {
	var topics []topicRow
	err := d.Retry.Wrap(ctx, func() error {
		sql := `SELECT id, name FROM topic ORDER BY id;`
		rows, err := d.Datastore.Pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()

		var scanned []topicRow
		for rows.Next() {
			var t topicRow
			if err := rows.Scan(&t.id, &t.name); err != nil {
				return err
			}
			scanned = append(scanned, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		topics = scanned
		return nil
	})
	return topics, err
}

// partitionCount is the number of partitions on the topic's message log.
func (d *AlertDatastore) partitionCount(ctx context.Context, topicId int64) (int64, error) {
	var count int64
	err := d.Retry.Wrap(ctx, func() error {
		sql := `SELECT count(*) FROM pg_inherits WHERE inhparent = to_regclass($1);`
		return d.Datastore.Pool.QueryRow(ctx, sql, iTopic.MessageLogTable(topicId)).Scan(&count)
	})
	return count, err
}

// partitionLockCeiling is the partition count at which a DROP/Destroy risks "out
// of shared memory". stock = max_locks_per_transaction * (max_connections +
// max_prepared_transactions), the fixed lock-table size set once at server start.
func (d *AlertDatastore) partitionLockCeiling(ctx context.Context) (int64, error) {
	var stock int64
	err := d.Retry.Wrap(ctx, func() error {
		sql := `
			SELECT current_setting('max_locks_per_transaction')::bigint
				* (current_setting('max_connections')::bigint
					+ current_setting('max_prepared_transactions')::bigint);
		`
		return d.Datastore.Pool.QueryRow(ctx, sql).Scan(&stock)
	})
	if err != nil {
		return 0, err
	}
	return stock / locksPerPartition, nil
}

// compacted reports whether the topic has any compaction_head rows -- only then
// does latest-key replay read-cost apply.
func (d *AlertDatastore) compacted(ctx context.Context, topicId int64) (bool, error) {
	var compacted bool
	err := d.Retry.Wrap(ctx, func() error {
		sql := `SELECT EXISTS (SELECT 1 FROM compaction_head WHERE topic_id = $1);`
		return d.Datastore.Pool.QueryRow(ctx, sql, topicId).Scan(&compacted)
	})
	return compacted, err
}
